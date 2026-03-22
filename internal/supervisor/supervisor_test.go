package supervisor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
	"github.com/shaiknoorullah/wtfrc/internal/session"
)

// ---------------------------------------------------------------------------
// Mock LLM provider
// ---------------------------------------------------------------------------

type mockProvider struct {
	name      string
	responses []llm.CompletionResponse
	errors    []error
	callCount int
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	i := m.callCount
	m.callCount++
	if i < len(m.errors) && m.errors[i] != nil {
		return llm.CompletionResponse{}, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return llm.CompletionResponse{}, fmt.Errorf("unexpected call %d", i)
}
func (m *mockProvider) Stream(_ context.Context, _ llm.CompletionRequest) (<-chan string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *kb.DB {
	t.Helper()
	db, err := kb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func strPtr(s string) *string { return &s }

func insertTestEntry(t *testing.T, db *kb.DB, binding, action, desc string) int64 {
	t.Helper()
	e := kb.KBEntry{
		Tool:        "i3",
		Type:        parsers.EntryKeybind,
		RawBinding:  strPtr(binding),
		RawAction:   strPtr(action),
		Description: desc,
		SourceFile:  "/etc/i3/config",
		SourceLine:  1,
		Category:    "wm",
		SeeAlso:     []string{},
		IndexedAt:   time.Now().UTC(),
		FileHash:    "abc123",
	}
	id, err := db.InsertEntry(&e, []string{"test intent"})
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReviewDetectsPhantomEntryID(t *testing.T) {
	db := openTestDB(t)
	mgr := session.NewManager(db)

	// Insert two real entries.
	realID1 := insertTestEntry(t, db, "$mod+Return", "exec alacritty", "Launch terminal")
	realID2 := insertTestEntry(t, db, "$mod+d", "exec rofi", "Launch app launcher")

	// Start a session with queries.
	sess, err := mgr.StartSession("test-model")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Query 1: cites real entries — should be clean.
	err = mgr.LogQuery(sess.ID, &kb.Query{
		Question:       "How do I open a terminal?",
		Answer:         "Press `$mod+Return` to launch alacritty.",
		EntriesUsed:    []int64{realID1},
		ResponseTimeMs: 80,
		Timestamp:      time.Now().UTC(),
		Issues:         []string{},
	})
	if err != nil {
		t.Fatalf("log query 1: %v", err)
	}

	// Query 2: cites a real ID + a nonexistent ID 9999.
	err = mgr.LogQuery(sess.ID, &kb.Query{
		Question:       "How do I launch rofi?",
		Answer:         "Use `$mod+d` to open the app launcher.",
		EntriesUsed:    []int64{realID2, 9999},
		ResponseTimeMs: 90,
		Timestamp:      time.Now().UTC(),
		Issues:         []string{},
	})
	if err != nil {
		t.Fatalf("log query 2: %v", err)
	}

	// Run supervisor review (no LLM needed for this test).
	sup := New(db, nil, mgr)
	report, err := sup.Review(context.Background())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Verify the session was reviewed.
	if report.SessionsReviewed != 1 {
		t.Errorf("expected 1 session reviewed, got %d", report.SessionsReviewed)
	}

	// Verify exactly one flagged query (the one with fake ID 9999).
	if len(report.FlaggedQueries) != 1 {
		t.Fatalf("expected 1 flagged query, got %d", len(report.FlaggedQueries))
	}

	fq := report.FlaggedQueries[0]
	if fq.Question != "How do I launch rofi?" {
		t.Errorf("unexpected flagged question: %s", fq.Question)
	}

	foundPhantom := false
	for _, iss := range fq.Issues {
		if strings.Contains(iss, "9999") && strings.Contains(iss, "does not exist") {
			foundPhantom = true
		}
	}
	if !foundPhantom {
		t.Errorf("expected phantom-ID issue, got issues: %v", fq.Issues)
	}

	// Verify the supervisor run was logged.
	var runCount int
	err = db.Conn().QueryRow(`SELECT COUNT(*) FROM supervisor_runs`).Scan(&runCount)
	if err != nil {
		t.Fatalf("count supervisor_runs: %v", err)
	}
	if runCount != 1 {
		t.Errorf("expected 1 supervisor_run, got %d", runCount)
	}
}

func TestReviewDeterministicRefMismatch(t *testing.T) {
	db := openTestDB(t)
	mgr := session.NewManager(db)

	realID := insertTestEntry(t, db, "$mod+Return", "exec alacritty", "Launch terminal")

	sess, err := mgr.StartSession("test-model")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	// The answer claims $mod+Shift+q but the cited entry only has $mod+Return.
	err = mgr.LogQuery(sess.ID, &kb.Query{
		Question:       "How do I close a window?",
		Answer:         "Press `$mod+Shift+q` to close the focused window.",
		EntriesUsed:    []int64{realID},
		ResponseTimeMs: 70,
		Timestamp:      time.Now().UTC(),
		Issues:         []string{},
	})
	if err != nil {
		t.Fatalf("log query: %v", err)
	}

	sup := New(db, nil, mgr)
	report, err := sup.Review(context.Background())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	if len(report.FlaggedQueries) != 1 {
		t.Fatalf("expected 1 flagged query, got %d", len(report.FlaggedQueries))
	}

	foundMismatch := false
	for _, iss := range report.FlaggedQueries[0].Issues {
		if strings.Contains(iss, "$mod+Shift+q") && strings.Contains(iss, "not found in cited entries") {
			foundMismatch = true
		}
	}
	if !foundMismatch {
		t.Errorf("expected ref-mismatch issue, got: %v", report.FlaggedQueries[0].Issues)
	}
}

func TestReviewTier2LLMCrossCheck(t *testing.T) {
	db := openTestDB(t)
	mgr := session.NewManager(db)

	sess, err := mgr.StartSession("test-model")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Query with no entries_used but answer cites a specific keybind — triggers Tier 2.
	err = mgr.LogQuery(sess.ID, &kb.Query{
		Question:       "How do I switch workspaces?",
		Answer:         "Press `$mod+1` through `$mod+9` to switch workspaces.",
		EntriesUsed:    []int64{},
		ResponseTimeMs: 60,
		Timestamp:      time.Now().UTC(),
		Issues:         []string{},
	})
	if err != nil {
		t.Fatalf("log query: %v", err)
	}

	// Mock LLM says the answer is inaccurate.
	mock := &mockProvider{
		name: "mock-verifier",
		responses: []llm.CompletionResponse{
			{Content: `{"accurate":false,"hallucinated_refs":["$mod+1"],"contradictions":["no entries support workspace switching"]}`},
		},
	}

	sup := New(db, mock, mgr)
	report, err := sup.Review(context.Background())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	if mock.callCount == 0 {
		t.Fatal("expected LLM provider to be called for Tier 2 verification")
	}

	if len(report.FlaggedQueries) != 1 {
		t.Fatalf("expected 1 flagged query, got %d", len(report.FlaggedQueries))
	}

	fq := report.FlaggedQueries[0]
	if fq.Tier != "llm" {
		t.Errorf("expected tier=llm, got %s", fq.Tier)
	}

	foundHallucinated := false
	foundContradiction := false
	for _, iss := range fq.Issues {
		if strings.Contains(iss, "hallucinated reference") && strings.Contains(iss, "$mod+1") {
			foundHallucinated = true
		}
		if strings.Contains(iss, "contradiction") {
			foundContradiction = true
		}
	}
	if !foundHallucinated {
		t.Errorf("expected hallucinated ref issue, got: %v", fq.Issues)
	}
	if !foundContradiction {
		t.Errorf("expected contradiction issue, got: %v", fq.Issues)
	}
}

func TestReviewNoIssues(t *testing.T) {
	db := openTestDB(t)
	mgr := session.NewManager(db)

	realID := insertTestEntry(t, db, "$mod+Return", "exec alacritty", "Launch terminal")

	sess, err := mgr.StartSession("test-model")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	err = mgr.LogQuery(sess.ID, &kb.Query{
		Question:       "How do I open a terminal?",
		Answer:         "Press `$mod+Return` to launch alacritty.",
		EntriesUsed:    []int64{realID},
		ResponseTimeMs: 50,
		Timestamp:      time.Now().UTC(),
		Issues:         []string{},
	})
	if err != nil {
		t.Fatalf("log query: %v", err)
	}

	sup := New(db, nil, mgr)
	report, err := sup.Review(context.Background())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	if report.IssuesFound != 0 {
		t.Errorf("expected 0 issues, got %d", report.IssuesFound)
	}
	if len(report.FlaggedQueries) != 0 {
		t.Errorf("expected 0 flagged queries, got %d", len(report.FlaggedQueries))
	}
}

func TestGenerateMarkdown(t *testing.T) {
	r := &Report{
		RunAt:            time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		SessionsReviewed: 3,
		IssuesFound:      2,
		FlaggedQueries: []FlaggedQuery{
			{
				QueryID:   42,
				SessionID: "sess-abc",
				Question:  "How to close a window?",
				Issues:    []string{"cited entry 9999 does not exist in the KB"},
				Tier:      "deterministic",
			},
		},
		Suggestions: []string{"Re-index source files."},
	}

	md := GenerateMarkdown(r)

	checks := []string{
		"# Supervisor Report",
		"Sessions reviewed:** 3",
		"Issues found:** 2",
		"Query 42",
		"sess-abc",
		"deterministic",
		"9999",
		"Re-index source files.",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestExtractAnswerRefs(t *testing.T) {
	answer := "Press `$mod+Shift+q` to quit, or use `rofi` launcher with $mod+d."
	refs := extractAnswerRefs(answer)

	expected := map[string]bool{
		"$mod+Shift+q": false,
		"$mod+d":       false,
		"rofi":         false,
	}
	for _, r := range refs {
		if _, ok := expected[r]; ok {
			expected[r] = true
		}
	}
	for k, found := range expected {
		if !found {
			t.Errorf("expected ref %q not found in %v", k, refs)
		}
	}
}

func TestVerifyAnswerDeterministicClean(t *testing.T) {
	db := openTestDB(t)
	mgr := session.NewManager(db)

	realID := insertTestEntry(t, db, "$mod+Return", "exec alacritty", "Launch terminal")

	entry, err := db.GetEntry(realID)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}

	q := kb.Query{
		Question:    "How do I open a terminal?",
		Answer:      "Press `$mod+Return` to launch alacritty.",
		EntriesUsed: []int64{realID},
	}

	sup := New(db, nil, mgr)
	issues := sup.verifyAnswerDeterministic(&q, []kb.KBEntry{*entry})
	if len(issues) != 0 {
		t.Errorf("expected no issues, got: %v", issues)
	}
}
