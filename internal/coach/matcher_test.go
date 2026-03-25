package coach

import (
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// helpers

func strPtr(s string) *string { return &s }

func makeAlias(aliasName, expansion, tool, sourceFile string, sourceLine int) kb.KBEntry {
	return kb.KBEntry{
		Tool:       tool,
		Type:       parsers.EntryAlias,
		RawBinding: strPtr(aliasName),
		RawAction:  strPtr(expansion),
		SourceFile: sourceFile,
		SourceLine: sourceLine,
	}
}

// ---------------------------------------------------------------------------
// TestNormalize
// ---------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  git  status  ", "git status"},
		{"sudo docker ps", "docker ps"},
		{"noglob git log", "git log"},
		{"git log | head", "git log"},
		{"git status", "git status"},
		{"nocorrect ls -la", "ls -la"},
		{"command ls", "ls"},
		// "sudo" alone must NOT be stripped
		{"sudo", "sudo"},
		// prefix only stripped when followed by space + more text
		{"noglob", "noglob"},
		// pipe at end — trailing pipe chain removed
		{"git log | head -n 10", "git log"},
		// multiple spaces around operators
		{"git status  &&  git diff", "git status && git diff"},
	}

	for _, tc := range tests {
		got := normalize(tc.input)
		if got != tc.expected {
			t.Errorf("normalize(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchExact
// ---------------------------------------------------------------------------

func TestMatchExact(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
		makeAlias("gp", "git push", "zsh", "/home/user/.zshrc", 11),
		makeAlias("ll", "ls -la", "zsh", "/home/user/.zshrc", 12),
	}
	m := NewMatcher(entries)

	tests := []struct {
		source      string
		action      string
		wantNil     bool
		wantOptimal string
	}{
		{SourceShell, "git status", false, "gs"},
		{SourceShell, "git push origin main", false, "gp origin main"},
		{SourceShell, "ls -la", false, "ll"},
		{SourceShell, "git commit", true, ""},
		// user already typed the alias — no suggestion
		{SourceShell, "gs", true, ""},
	}

	for _, tc := range tests {
		got := m.Match(tc.source, tc.action)
		if tc.wantNil {
			if got != nil {
				t.Errorf("Match(%q, %q) = %+v, want nil", tc.source, tc.action, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("Match(%q, %q) = nil, want Suggestion{Optimal: %q}", tc.source, tc.action, tc.wantOptimal)
			continue
		}
		if got.Optimal != tc.wantOptimal {
			t.Errorf("Match(%q, %q).Optimal = %q, want %q", tc.source, tc.action, got.Optimal, tc.wantOptimal)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchParameterized
// ---------------------------------------------------------------------------

func TestMatchParameterized(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("dclogs", "docker compose logs -f", "zsh", "/home/user/.zshrc", 20),
		makeAlias("dcup", "docker compose up -d", "zsh", "/home/user/.zshrc", 21),
	}
	m := NewMatcher(entries)

	tests := []struct {
		source      string
		action      string
		wantNil     bool
		wantOptimal string
	}{
		{SourceShell, "docker compose logs -f myservice", false, "dclogs myservice"},
		{SourceShell, "docker compose up -d", false, "dcup"},
		{SourceShell, "docker compose down", true, ""},
	}

	for _, tc := range tests {
		got := m.Match(tc.source, tc.action)
		if tc.wantNil {
			if got != nil {
				t.Errorf("Match(%q, %q) = %+v, want nil", tc.source, tc.action, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("Match(%q, %q) = nil, want Suggestion{Optimal: %q}", tc.source, tc.action, tc.wantOptimal)
			continue
		}
		if got.Optimal != tc.wantOptimal {
			t.Errorf("Match(%q, %q).Optimal = %q, want %q", tc.source, tc.action, got.Optimal, tc.wantOptimal)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchKeySaved
// ---------------------------------------------------------------------------

func TestMatchKeySaved(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	want := len("git status") - len("gs") // 10 - 2 = 8
	if got.KeysSaved != want {
		t.Errorf("KeysSaved = %d, want %d", got.KeysSaved, want)
	}
}

// ---------------------------------------------------------------------------
// TestMatchNoShorter
// ---------------------------------------------------------------------------

func TestMatchNoShorter(t *testing.T) {
	// alias "gitstat" is longer than expansion "git st" — should NOT suggest
	entries := []kb.KBEntry{
		makeAlias("gitstat", "git st", "zsh", "/home/user/.zshrc", 30),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git st")
	if got != nil {
		t.Errorf("Match returned %+v, want nil (alias longer than expansion)", got)
	}
}

// ---------------------------------------------------------------------------
// TestActionID
// ---------------------------------------------------------------------------

func TestActionID(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	want := "zsh:gs"
	if got.ActionID != want {
		t.Errorf("ActionID = %q, want %q", got.ActionID, want)
	}
}

// ---------------------------------------------------------------------------
// TestMatchSourceFile
// ---------------------------------------------------------------------------

func TestMatchSourceFile(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 42),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	if got.SourceFile != "/home/user/.zshrc" {
		t.Errorf("SourceFile = %q, want %q", got.SourceFile, "/home/user/.zshrc")
	}
	if got.SourceLine != 42 {
		t.Errorf("SourceLine = %d, want %d", got.SourceLine, 42)
	}
}

// ---------------------------------------------------------------------------
// TestMatchFromDB — replicates the E2E scenario: seed DB via SQL, read entries
// via GetEntriesByTypes, build Matcher, verify it matches "git status" → "gs".
// ---------------------------------------------------------------------------

func TestMatchFromDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/test_matcher.db"
	db, err := kb.Open(dbPath)
	if err != nil {
		t.Fatalf("kb.Open: %v", err)
	}
	defer db.Close()

	// Seed entries via raw SQL, exactly as the E2E test does.
	conn := db.Conn()
	_, err = conn.Exec(`INSERT INTO entries (tool, type, raw_binding, raw_action, description, source_file, source_line, category, see_also, indexed_at, file_hash)
		VALUES ('zsh', 'alias', 'gs', 'git status', 'git status alias', '/home/test/.zshrc', 4, 'shell', '[]', '2025-01-01T00:00:00Z', 'seed')`)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Read entries via the same method the daemon uses.
	entries, err := db.GetEntriesByTypes([]string{"alias", "function", "keybind"})
	if err != nil {
		t.Fatalf("GetEntriesByTypes: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("GetEntriesByTypes returned 0 entries")
	}

	// Verify the entry fields are populated correctly.
	e := entries[0]
	if e.RawBinding == nil {
		t.Fatal("RawBinding is nil after DB scan")
	}
	if e.RawAction == nil {
		t.Fatal("RawAction is nil after DB scan")
	}
	if *e.RawBinding != "gs" {
		t.Errorf("RawBinding = %q, want %q", *e.RawBinding, "gs")
	}
	if *e.RawAction != "git status" {
		t.Errorf("RawAction = %q, want %q", *e.RawAction, "git status")
	}

	// Build matcher and verify the match works.
	m := NewMatcher(entries)
	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("Match(shell, 'git status') returned nil; expected suggestion for 'gs'")
	}
	if got.Optimal != "gs" {
		t.Errorf("Optimal = %q, want %q", got.Optimal, "gs")
	}
	if got.ActionID != "zsh:gs" {
		t.Errorf("ActionID = %q, want %q", got.ActionID, "zsh:gs")
	}
	if got.KeysSaved != 8 {
		t.Errorf("KeysSaved = %d, want 8", got.KeysSaved)
	}
}
