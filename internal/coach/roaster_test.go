package coach

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// openRoasterTestDB opens a temp SQLite DB with both coaching_state and
// coaching_messages tables (the minimal schema required by roaster tests).
func openRoasterTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "roaster_test.db")

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open roaster test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS coaching_state (
		    action_id TEXT PRIMARY KEY,
		    state TEXT NOT NULL DEFAULT 'novice',
		    consecutive_optimal INTEGER NOT NULL DEFAULT 0,
		    total_coached INTEGER NOT NULL DEFAULT 0,
		    total_adopted INTEGER NOT NULL DEFAULT 0,
		    first_coached_at TEXT,
		    last_coached_at TEXT,
		    last_adopted_at TEXT,
		    next_coach_after TEXT,
		    graduated_at TEXT
		);
		CREATE TABLE IF NOT EXISTS coaching_messages (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    category TEXT NOT NULL,
		    mode TEXT NOT NULL,
		    template TEXT NOT NULL,
		    variables TEXT NOT NULL,
		    generated_at TEXT NOT NULL,
		    used_count INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_coaching_messages_cat_mode
		    ON coaching_messages(category, mode);
	`)
	if err != nil {
		t.Fatalf("create roaster schema: %v", err)
	}

	return db
}

// testSuggestion returns a Suggestion with deterministic fields for use in tests.
func testSuggestion() *Suggestion {
	return &Suggestion{
		ActionID:   "zsh:gs",
		Tool:       "zsh",
		UserAction: "git status",
		Optimal:    "gs",
		SourceFile: "~/.zshrc",
		SourceLine: 42,
		KeysSaved:  8,
	}
}

// mockLLM is a trivial llm.Provider that returns pre-configured responses.
type mockLLM struct {
	name     string
	response string
	calls    int
}

func (m *mockLLM) Name() string { return m.name }

func (m *mockLLM) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	m.calls++
	return llm.CompletionResponse{Content: m.response}, nil
}

func (m *mockLLM) Stream(_ context.Context, _ llm.CompletionRequest) (<-chan string, error) {
	ch := make(chan string, 1)
	ch <- m.response
	close(ch)
	return ch, nil
}

func (m *mockLLM) HealthCheck(_ context.Context) error { return nil }

// ---------------------------------------------------------------------------
// TestInterpolate
// ---------------------------------------------------------------------------

func TestInterpolate(t *testing.T) {
	s := &Suggestion{
		UserAction: "git status",
		Optimal:    "gs",
		SourceFile: "~/.zshrc",
		SourceLine: 42,
		KeysSaved:  8,
	}

	tmpl := "{optimal} instead of {typed}: save {keys_saved} keys. ({source_file}:{source_line}) chars={chars_typed}"
	got := interpolate(tmpl, s)

	checks := []struct {
		needle string
		desc   string
	}{
		{"gs", "{optimal} replaced"},
		{"git status", "{typed} replaced"},
		{"~/.zshrc", "{source_file} replaced"},
		{"42", "{source_line} replaced"},
		{fmt.Sprintf("%d", len(s.UserAction)), "{chars_typed} replaced"},
		{"8", "{keys_saved} replaced"},
	}

	for _, c := range checks {
		if !strings.Contains(got, c.needle) {
			t.Errorf("interpolate: %s — want %q in %q", c.desc, c.needle, got)
		}
	}

	// Ensure none of the placeholders remain.
	for _, placeholder := range []string{"{optimal}", "{typed}", "{source_file}", "{source_line}", "{chars_typed}", "{keys_saved}"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("interpolate: placeholder %q was not replaced in %q", placeholder, got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestTier1Templates
// ---------------------------------------------------------------------------

func TestTier1Templates(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg)

	s := testSuggestion()

	tests := []struct {
		mode     string
		wantIn   []string
	}{
		{
			mode:   "chill",
			wantIn: []string{"gs", "~/.zshrc"},
		},
		{
			mode:   "moderate",
			wantIn: []string{"gs", "~/.zshrc"},
		},
		{
			mode:   "strict",
			wantIn: []string{"gs", "~/.zshrc"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			got := r.Generate(s, tc.mode)
			if got == "" {
				t.Fatalf("mode=%s: Generate returned empty string", tc.mode)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(got, want) {
					t.Errorf("mode=%s: want %q in output %q", tc.mode, want, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTier2CachedPool
// ---------------------------------------------------------------------------

func TestTier2CachedPool(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg)

	s := testSuggestion()
	// The suggestion has Tool="zsh" so category should be "shell_alias" (ActionID prefix "zsh:gs").
	// Insert a cached message for shell_alias + chill.
	_, err := db.Exec(`
		INSERT INTO coaching_messages (category, mode, template, variables, generated_at, used_count)
		VALUES ('shell_alias', 'chill', 'LLM cached: use {optimal} ({source_file}:{source_line})', '{}', datetime('now'), 0)
	`)
	if err != nil {
		t.Fatalf("insert cached message: %v", err)
	}

	got := r.Generate(s, "chill")
	if got == "" {
		t.Fatal("Generate returned empty string")
	}

	// Should include the LLM cached prefix to confirm it came from tier 2.
	if !strings.Contains(got, "LLM cached:") {
		t.Errorf("expected tier-2 cached message, got: %q", got)
	}

	// Verify used_count was incremented.
	var count int
	err = db.QueryRow(`SELECT used_count FROM coaching_messages WHERE category = 'shell_alias' AND mode = 'chill'`).Scan(&count)
	if err != nil {
		t.Fatalf("query used_count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected used_count=1, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// TestTier2FallbackToTier1
// ---------------------------------------------------------------------------

func TestTier2FallbackToTier1(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg)

	s := testSuggestion()

	// No cached messages; should fall back to Tier 1.
	got := r.Generate(s, "chill")
	if got == "" {
		t.Fatal("Generate returned empty string")
	}

	// Tier 1 chill shell_alias template contains "Tip:"
	if !strings.Contains(got, "Tip:") {
		t.Errorf("expected tier-1 fallback template (contains 'Tip:'), got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// TestTier2Rotation
// ---------------------------------------------------------------------------

func TestTier2Rotation(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg)

	s := testSuggestion()

	// Insert 3 messages with different identifiable content.
	msgs := []string{"alpha: use {optimal}", "beta: use {optimal}", "gamma: use {optimal}"}
	for _, tmpl := range msgs {
		_, err := db.Exec(`
			INSERT INTO coaching_messages (category, mode, template, variables, generated_at, used_count)
			VALUES ('shell_alias', 'chill', ?, '{}', datetime('now'), 0)
		`, tmpl)
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}

	seen := make(map[string]bool)
	for i := 0; i < 3; i++ {
		got := r.Generate(s, "chill")
		if got == "" {
			t.Fatalf("call %d: Generate returned empty string", i)
		}
		// Extract the prefix word (alpha/beta/gamma) to track which message was used.
		var prefix string
		switch {
		case strings.Contains(got, "alpha"):
			prefix = "alpha"
		case strings.Contains(got, "beta"):
			prefix = "beta"
		case strings.Contains(got, "gamma"):
			prefix = "gamma"
		default:
			t.Fatalf("call %d: output %q doesn't match any inserted message", i, got)
		}
		if seen[prefix] {
			t.Errorf("call %d: rotation repeated %q before all messages were used once", i, prefix)
		}
		seen[prefix] = true
	}

	if len(seen) != 3 {
		t.Errorf("expected 3 distinct messages, got %d", len(seen))
	}
}

// ---------------------------------------------------------------------------
// TestRefreshPool
// ---------------------------------------------------------------------------

func TestRefreshPool(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}

	// Build a JSON array of 10 messages that the mock LLM will return.
	messages := make([]string, 10)
	for i := range messages {
		messages[i] = fmt.Sprintf("Generated message %d: use {optimal}", i+1)
	}
	jsonResp, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("marshal mock messages: %v", err)
	}

	mock := &mockLLM{name: "mock", response: string(jsonResp)}
	r := NewRoaster(db, mock, cfg)

	if err := r.RefreshPool(context.Background()); err != nil {
		t.Fatalf("RefreshPool error: %v", err)
	}

	// Verify that messages were inserted for at least one category+mode combo.
	var total int
	err = db.QueryRow(`SELECT COUNT(*) FROM coaching_messages`).Scan(&total)
	if err != nil {
		t.Fatalf("count coaching_messages: %v", err)
	}
	if total == 0 {
		t.Error("RefreshPool: no messages inserted into coaching_messages")
	}

	// The mock LLM should have been called at least once (one per category×mode).
	if mock.calls == 0 {
		t.Error("RefreshPool: mock LLM was never called")
	}
}

// ---------------------------------------------------------------------------
// TestRefreshPoolNilLLM
// ---------------------------------------------------------------------------

func TestRefreshPoolNilLLM(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg)

	// Should be a no-op when LLM is nil.
	if err := r.RefreshPool(context.Background()); err != nil {
		t.Errorf("RefreshPool with nil llm should return nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestGenerateLive
// ---------------------------------------------------------------------------

func TestGenerateLive(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}

	mock := &mockLLM{name: "mock", response: "You should use gs instead of git status."}
	r := NewRoaster(db, mock, cfg)

	s := testSuggestion()
	got, err := r.GenerateLive(context.Background(), s, "strict", "user is in a git repo")
	if err != nil {
		t.Fatalf("GenerateLive error: %v", err)
	}
	if got == "" {
		t.Error("GenerateLive returned empty string")
	}

	// The mock LLM should have been called exactly once.
	if mock.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls)
	}
}

// ---------------------------------------------------------------------------
// TestGenerateLiveOffline
// ---------------------------------------------------------------------------

func TestGenerateLiveOffline(t *testing.T) {
	db := openRoasterTestDB(t)
	cfg := &config.CoachConfig{}
	r := NewRoaster(db, nil, cfg) // nil llm = offline mode

	s := testSuggestion()
	_, err := r.GenerateLive(context.Background(), s, "strict", "")
	if err == nil {
		t.Error("GenerateLive with nil llm should return an error")
	}
}

// ---------------------------------------------------------------------------
// TestCategoryFromSuggestion
// ---------------------------------------------------------------------------

func TestCategoryFromSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		s        *Suggestion
		wantCat  string
	}{
		{
			name:    "zsh alias",
			s:       &Suggestion{Tool: "zsh", ActionID: "zsh:gs"},
			wantCat: "shell_alias",
		},
		{
			name:    "bash alias",
			s:       &Suggestion{Tool: "bash", ActionID: "bash:ll"},
			wantCat: "shell_alias",
		},
		{
			name:    "zsh function",
			s:       &Suggestion{Tool: "zsh", ActionID: "zsh:myfunc"},
			wantCat: "shell_alias",
		},
		{
			name:    "hyprland keybind",
			s:       &Suggestion{Tool: "hyprland", ActionID: "hyprland:movefocus_l"},
			wantCat: "wm_keybind",
		},
		{
			name:    "tmux keybind",
			s:       &Suggestion{Tool: "tmux", ActionID: "tmux:split"},
			wantCat: "wm_keybind",
		},
		{
			name:    "kitty keybind",
			s:       &Suggestion{Tool: "kitty"},
			wantCat: "wm_keybind",
		},
		{
			name:    "qutebrowser keybind",
			s:       &Suggestion{Tool: "qutebrowser"},
			wantCat: "wm_keybind",
		},
		{
			name:    "yazi keybind",
			s:       &Suggestion{Tool: "yazi"},
			wantCat: "wm_keybind",
		},
		{
			name:    "nvim editor motion",
			s:       &Suggestion{Tool: "nvim"},
			wantCat: "editor_motion",
		},
		{
			name:    "unknown tool",
			s:       &Suggestion{Tool: "something_else"},
			wantCat: "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := categoryFromSuggestion(tc.s)
			if got != tc.wantCat {
				t.Errorf("categoryFromSuggestion(%+v) = %q, want %q", tc.s, got, tc.wantCat)
			}
		})
	}
}
