package session

import (
	"fmt"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

func openTestDB(t *testing.T) *kb.DB {
	t.Helper()
	db, err := kb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSessionLifecycle(t *testing.T) {
	db := openTestDB(t)
	mgr := NewManager(db)

	// Start a session.
	sess, err := mgr.StartSession("gpt-4")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if sess.ModelUsed != "gpt-4" {
		t.Fatalf("expected model_used=gpt-4, got %s", sess.ModelUsed)
	}

	// Log 3 queries.
	for i := 0; i < 3; i++ {
		q := kb.Query{
			Question:       fmt.Sprintf("question %d", i),
			Answer:         fmt.Sprintf("answer %d", i),
			EntriesUsed:    []int64{int64(i + 1)},
			ResponseTimeMs: 100 + int64(i),
			Timestamp:      time.Now().UTC(),
			Issues:         []string{},
		}
		if err := mgr.LogQuery(sess.ID, q); err != nil {
			t.Fatalf("LogQuery %d: %v", i, err)
		}
	}

	// End the session.
	if err := mgr.EndSession(sess.ID); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	// Retrieve and verify.
	got, err := mgr.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if got.EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
	if len(got.Queries) != 3 {
		t.Fatalf("expected 3 queries, got %d", len(got.Queries))
	}

	// Verify query_count in DB directly.
	var queryCount int
	err = db.Conn().QueryRow(`SELECT query_count FROM sessions WHERE id = ?`, sess.ID).Scan(&queryCount)
	if err != nil {
		t.Fatalf("scan query_count: %v", err)
	}
	if queryCount != 3 {
		t.Fatalf("expected query_count=3, got %d", queryCount)
	}
}

func TestRecentQueries(t *testing.T) {
	db := openTestDB(t)
	mgr := NewManager(db)

	sess, err := mgr.StartSession("gpt-4")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Log 6 queries with increasing timestamps.
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		q := kb.Query{
			Question:       fmt.Sprintf("q%d", i),
			Answer:         fmt.Sprintf("a%d", i),
			EntriesUsed:    []int64{},
			ResponseTimeMs: 50,
			Timestamp:      base.Add(time.Duration(i) * time.Minute),
			Issues:         []string{},
		}
		if err := mgr.LogQuery(sess.ID, q); err != nil {
			t.Fatalf("LogQuery %d: %v", i, err)
		}
	}

	// Request last 3 queries.
	recent, err := mgr.RecentQueries(sess.ID, 3)
	if err != nil {
		t.Fatalf("RecentQueries: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 queries, got %d", len(recent))
	}

	// Verify they are the 3 most recent (q5, q4, q3) in descending order.
	expected := []string{"q5", "q4", "q3"}
	for i, q := range recent {
		if q.Question != expected[i] {
			t.Errorf("recent[%d]: expected %s, got %s", i, expected[i], q.Question)
		}
	}
}
