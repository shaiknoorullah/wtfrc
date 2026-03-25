package cli

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// openCoachTestDB opens a full kb.DB in a temp dir (includes all coach tables).
func openCoachTestDB(t *testing.T) *kb.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "coach_test.db")
	db, err := kb.Open(path)
	if err != nil {
		t.Fatalf("openCoachTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertCoachingStateRow inserts a coaching_state row directly.
func insertCoachingStateRow(t *testing.T, db *kb.DB, actionID, state string, totalCoached, totalAdopted int, graduatedAt string) {
	t.Helper()
	_, err := db.Conn().Exec(`
		INSERT INTO coaching_state (action_id, state, total_coached, total_adopted, graduated_at)
		VALUES (?, ?, ?, ?, ?)`,
		actionID, state, totalCoached, totalAdopted, nullStr(graduatedAt),
	)
	if err != nil {
		t.Fatalf("insertCoachingStateRow: %v", err)
	}
}

// insertCoachingLogRow inserts a coaching_log row (requires coaching_state FK).
func insertCoachingLogRow(t *testing.T, db *kb.DB, ts, source, actionID, userAction, optimalAction string, wasAdopted int) {
	t.Helper()
	_, err := db.Conn().Exec(`
		INSERT INTO coaching_log (timestamp, source, action_id, user_action, optimal_action, message, mode, delivery, was_adopted)
		VALUES (?, ?, ?, ?, ?, '', 'chill', 'inline', ?)`,
		ts, source, actionID, userAction, optimalAction, wasAdopted,
	)
	if err != nil {
		t.Fatalf("insertCoachingLogRow: %v", err)
	}
}

// nullStr returns nil if s is empty, otherwise the string value for sql.
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------
// TestCoachStatsQuery
// ---------------------------------------------------------------------------

// TestCoachStatsQuery verifies that the stats queries produce correct counts.
func TestCoachStatsQuery(t *testing.T) {
	db := openCoachTestDB(t)

	// Insert coaching_state rows.
	insertCoachingStateRow(t, db, "action1", "graduated", 10, 8, time.Now().Format(time.RFC3339))
	insertCoachingStateRow(t, db, "action2", "learning", 5, 2, "")
	insertCoachingStateRow(t, db, "action3", "graduated", 7, 7, time.Now().Format(time.RFC3339))

	// Insert coaching_log rows (requires coaching_state rows first due to FK).
	ts := time.Now().UTC().Format(time.RFC3339)
	insertCoachingLogRow(t, db, ts, "shell", "action1", "git status", "gs", 1)
	insertCoachingLogRow(t, db, ts, "shell", "action1", "git status", "gs", 0)
	insertCoachingLogRow(t, db, ts, "shell", "action2", "git log", "gl", 1)

	// Insert usage_events.
	for i := 0; i < 5; i++ {
		_, err := db.Conn().Exec(
			`INSERT INTO usage_events (tool, action, timestamp, was_optimal) VALUES ('shell', 'git status', ?, 0)`,
			time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			t.Fatalf("insert usage_event: %v", err)
		}
	}

	// Query: total coaching_log rows.
	var totalLog int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM coaching_log`).Scan(&totalLog); err != nil {
		t.Fatalf("count coaching_log: %v", err)
	}
	if totalLog != 3 {
		t.Errorf("coaching_log total: want 3, got %d", totalLog)
	}

	// Query: adopted coaching_log rows (was_adopted=1).
	var adoptedLog int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM coaching_log WHERE was_adopted=1`).Scan(&adoptedLog); err != nil {
		t.Fatalf("count adopted coaching_log: %v", err)
	}
	if adoptedLog != 2 {
		t.Errorf("coaching_log adopted: want 2, got %d", adoptedLog)
	}

	// Query: graduated count.
	var graduated int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM coaching_state WHERE state='graduated'`).Scan(&graduated); err != nil {
		t.Fatalf("count graduated: %v", err)
	}
	if graduated != 2 {
		t.Errorf("graduated: want 2, got %d", graduated)
	}

	// Query: usage_events total.
	var usageTotal int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&usageTotal); err != nil {
		t.Fatalf("count usage_events: %v", err)
	}
	if usageTotal != 5 {
		t.Errorf("usage_events total: want 5, got %d", usageTotal)
	}
}

// ---------------------------------------------------------------------------
// TestCoachLogQuery
// ---------------------------------------------------------------------------

// TestCoachLogQuery verifies ORDER BY timestamp DESC LIMIT 20 behaviour.
func TestCoachLogQuery(t *testing.T) {
	db := openCoachTestDB(t)

	// Insert a coaching_state row to satisfy FK.
	insertCoachingStateRow(t, db, "action1", "learning", 30, 10, "")

	// Insert 25 rows with distinct timestamps.
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		insertCoachingLogRow(t, db, ts, "shell", "action1", "git status", "gs", 0)
	}

	// Query: last 20 ordered by timestamp DESC.
	rows, err := db.Conn().Query(
		`SELECT timestamp, source, user_action, optimal_action, was_adopted
		 FROM coaching_log ORDER BY timestamp DESC LIMIT 20`,
	)
	if err != nil {
		t.Fatalf("query coaching_log: %v", err)
	}
	defer rows.Close()

	var results []string
	var prevTS string
	for rows.Next() {
		var ts, source, userAction, optimalAction string
		var wasAdopted int
		if err := rows.Scan(&ts, &source, &userAction, &optimalAction, &wasAdopted); err != nil {
			t.Fatalf("scan coaching_log: %v", err)
		}
		// Verify descending order.
		if prevTS != "" && ts > prevTS {
			t.Errorf("rows not in DESC order: %s > %s", ts, prevTS)
		}
		prevTS = ts
		results = append(results, ts)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	// Expect exactly 20 rows (not 25).
	if len(results) != 20 {
		t.Errorf("coaching_log limit: want 20, got %d", len(results))
	}

	// First result should be the latest timestamp (index 24).
	wantFirst := base.Add(24 * time.Minute).Format(time.RFC3339)
	if results[0] != wantFirst {
		t.Errorf("first row: want %s, got %s", wantFirst, results[0])
	}
}

// ---------------------------------------------------------------------------
// TestCoachGraduatedQuery
// ---------------------------------------------------------------------------

// TestCoachGraduatedQuery verifies the graduated query returns all graduated actions.
func TestCoachGraduatedQuery(t *testing.T) {
	db := openCoachTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert mix of states.
	insertCoachingStateRow(t, db, "action-grad-1", "graduated", 10, 10, now)
	insertCoachingStateRow(t, db, "action-grad-2", "graduated", 8, 8, now)
	insertCoachingStateRow(t, db, "action-learning", "learning", 5, 2, "")
	insertCoachingStateRow(t, db, "action-novice", "novice", 1, 0, "")

	// Query: graduated only.
	rows, err := db.Conn().Query(
		`SELECT action_id, graduated_at, total_coached, total_adopted
		 FROM coaching_state WHERE state='graduated'`,
	)
	if err != nil {
		t.Fatalf("query graduated: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var actionID, graduatedAt string
		var totalCoached, totalAdopted int
		if err := rows.Scan(&actionID, &graduatedAt, &totalCoached, &totalAdopted); err != nil {
			t.Fatalf("scan graduated: %v", err)
		}
		ids = append(ids, actionID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("graduated count: want 2, got %d", len(ids))
	}

	// Verify the correct IDs are returned.
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, want := range []string{"action-grad-1", "action-grad-2"} {
		if !idSet[want] {
			t.Errorf("graduated: missing %s", want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCoachFlagParsing
// ---------------------------------------------------------------------------

// TestCoachFlagParsing verifies that the expected flags are registered on
// the coach start subcommand.
func TestCoachFlagParsing(t *testing.T) {
	// Build a fresh cobra tree (not the global one to avoid side effects).
	root := &cobra.Command{Use: "wtfrc"}
	coachParent := &cobra.Command{Use: "coach"}
	startCmd := buildCoachStartCmd()
	coachParent.AddCommand(startCmd)
	root.AddCommand(coachParent)

	// Verify --mode flag.
	if f := startCmd.Flags().Lookup("mode"); f == nil {
		t.Error("--mode flag not registered on coach start")
	}

	// Verify --focus flag.
	if f := startCmd.Flags().Lookup("focus"); f == nil {
		t.Error("--focus flag not registered on coach start")
	}

	// Verify --layer4 flag.
	if f := startCmd.Flags().Lookup("layer4"); f == nil {
		t.Error("--layer4 flag not registered on coach start")
	}

	// Verify --strict flag.
	if f := startCmd.Flags().Lookup("strict"); f == nil {
		t.Error("--strict flag not registered on coach start")
	}

	// Verify flag types.
	modeFlag := startCmd.Flags().Lookup("mode")
	if modeFlag != nil && modeFlag.Value.Type() != "string" {
		t.Errorf("--mode flag type: want string, got %s", modeFlag.Value.Type())
	}

	layer4Flag := startCmd.Flags().Lookup("layer4")
	if layer4Flag != nil && layer4Flag.Value.Type() != "bool" {
		t.Errorf("--layer4 flag type: want bool, got %s", layer4Flag.Value.Type())
	}

	strictFlag := startCmd.Flags().Lookup("strict")
	if strictFlag != nil && strictFlag.Value.Type() != "bool" {
		t.Errorf("--strict flag type: want bool, got %s", strictFlag.Value.Type())
	}
}
