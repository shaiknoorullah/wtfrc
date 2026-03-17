package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

func TestArchive(t *testing.T) {
	db := openTestDB(t)
	mgr := NewManager(db)
	arch := NewArchiver(db)

	// Create two sessions with old timestamps (40 days ago).
	oldTime := time.Now().UTC().AddDate(0, 0, -40)
	var sessionIDs []string

	for i := 0; i < 2; i++ {
		sess, err := mgr.StartSession("gpt-4")
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		sessionIDs = append(sessionIDs, sess.ID)

		// Backdate the session's started_at and ended_at.
		started := oldTime.Add(time.Duration(i) * time.Hour)
		ended := started.Add(30 * time.Minute)
		_, err = db.Conn().Exec(
			`UPDATE sessions SET started_at = ?, ended_at = ? WHERE id = ?`,
			started.Format(time.RFC3339), ended.Format(time.RFC3339), sess.ID,
		)
		if err != nil {
			t.Fatalf("backdate session: %v", err)
		}

		// Log a query with old timestamp.
		q := kb.Query{
			Question:       "old question",
			Answer:         "old answer",
			EntriesUsed:    []int64{1, 2},
			ResponseTimeMs: 200,
			Timestamp:      started.Add(5 * time.Minute),
			Issues:         []string{"slow"},
		}
		if err := mgr.LogQuery(sess.ID, q); err != nil {
			t.Fatalf("LogQuery: %v", err)
		}
	}

	// Create one recent session that should NOT be archived.
	recentSess, err := mgr.StartSession("gpt-4")
	if err != nil {
		t.Fatalf("StartSession recent: %v", err)
	}

	archiveDir := filepath.Join(t.TempDir(), "archive")

	// Archive sessions older than 30 days.
	if err := arch.Archive(archiveDir, 30); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Verify the JSONL file was written.
	expectedFile := filepath.Join(archiveDir, "sessions-"+oldTime.Format("2006-01")+".jsonl")
	f, err := os.Open(expectedFile)
	if err != nil {
		t.Fatalf("open archive file: %v", err)
	}
	defer f.Close()

	var lines []archivedSession
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var as archivedSession
		if err := json.Unmarshal(scanner.Bytes(), &as); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		lines = append(lines, as)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 archived sessions in file, got %d", len(lines))
	}

	// Each archived session should have 1 query.
	for i, as := range lines {
		if len(as.Queries) != 1 {
			t.Errorf("session %d: expected 1 query, got %d", i, len(as.Queries))
		}
		if as.Queries[0].Question != "old question" {
			t.Errorf("session %d: unexpected question %q", i, as.Queries[0].Question)
		}
		if len(as.Queries[0].EntriesUsed) != 2 {
			t.Errorf("session %d: expected 2 entries_used, got %d", i, len(as.Queries[0].EntriesUsed))
		}
	}

	// Verify DB rows are marked archived.
	for _, sid := range sessionIDs {
		var archived int
		err := db.Conn().QueryRow(`SELECT archived FROM sessions WHERE id = ?`, sid).Scan(&archived)
		if err != nil {
			t.Fatalf("scan archived: %v", err)
		}
		if archived != 1 {
			t.Errorf("session %s: expected archived=1, got %d", sid, archived)
		}
	}

	// Verify recent session is NOT archived.
	var archived int
	err = db.Conn().QueryRow(`SELECT archived FROM sessions WHERE id = ?`, recentSess.ID).Scan(&archived)
	if err != nil {
		t.Fatalf("scan recent archived: %v", err)
	}
	if archived != 0 {
		t.Errorf("recent session: expected archived=0, got %d", archived)
	}
}
