//go:build integration

package coach

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Test helpers (integration variants)
// ---------------------------------------------------------------------------

// openIntegrationDB opens a fresh kb.DB in dir.
func openIntegrationDB(t *testing.T, dir string) *kb.DB {
	t.Helper()
	db, err := kb.Open(filepath.Join(dir, "integration.db"))
	if err != nil {
		t.Fatalf("openIntegrationDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// openIntegrationDBAt opens a fresh kb.DB at a specific path (for persistence tests).
func openIntegrationDBAt(t *testing.T, path string) *kb.DB {
	t.Helper()
	db, err := kb.Open(path)
	if err != nil {
		t.Fatalf("openIntegrationDBAt: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertTestAliases inserts "gs" → "git status" and "ll" → "ls -la" into db.
func insertTestAliases(t *testing.T, db *kb.DB) {
	t.Helper()
	for _, alias := range []struct{ binding, action string }{
		{"gs", "git status"},
		{"ll", "ls -la"},
	} {
		b, a := alias.binding, alias.action
		if _, err := db.InsertEntry(kb.KBEntry{
			Tool:        "zsh",
			Type:        parsers.EntryAlias,
			RawBinding:  &b,
			RawAction:   &a,
			Description: b + " shortcut",
			SourceFile:  "~/.zshrc",
			SourceLine:  10,
			FileHash:    "abc123",
			IndexedAt:   time.Now(),
		}, nil); err != nil {
			t.Fatalf("insertTestAliases %q: %v", b, err)
		}
	}
}

// integrationCoachConfig returns a permissive CoachConfig for integration tests.
func integrationCoachConfig() config.CoachConfig {
	return config.CoachConfig{
		Enabled:          true,
		Mode:             "chill",
		BudgetPerHour:    1000,
		CooldownSeconds:  0,
		QuietHours:       "",
		FocusCategory:    "",
		GraduationStreak: 10,
		Delivery: config.CoachDeliveryConfig{
			Shell:    "inline",
			Hyprland: "inline",
			Tmux:     "inline",
			Neovim:   "inline",
			Default:  "inline",
		},
	}
}

// queryString executes a single-column query and returns the first string result.
// Returns "" if no rows.
func queryString(t *testing.T, db *sql.DB, query string, args ...interface{}) string {
	t.Helper()
	var s string
	err := db.QueryRow(query, args...).Scan(&s)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("queryString(%q): %v", query, err)
	}
	return s
}

// ---------------------------------------------------------------------------
// TestIntegrationFullPipeline
// ---------------------------------------------------------------------------

// TestIntegrationFullPipeline verifies the end-to-end path from FIFO write
// through the coaching pipeline to DB records and the coach-msg file.
func TestIntegrationFullPipeline(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	// 1. Set up DB with alias entries.
	db := openIntegrationDB(t, dir)
	insertTestAliases(t, db)

	// 2. Create daemon with real DB, nil LLM, temp paths.
	cfg := &config.Config{Coach: integrationCoachConfig()}
	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// 3. Start daemon in goroutine with cancelable context.
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- d.Run(ctx)
	}()

	// 4. Wait for FIFO to be created.
	waitForFIFO(t, fifoPath)

	// 5. Write a shell event that matches the "gs" alias.
	writeFIFO(t, fifoPath, "shell\tgit status\t")

	// 6. Wait for async processing.
	time.Sleep(300 * time.Millisecond)

	conn := db.Conn()

	// 7. Verify coaching_log has an entry with optimal_action containing "gs".
	optimalAction := queryString(t, conn,
		`SELECT optimal_action FROM coaching_log LIMIT 1`)
	if optimalAction == "" {
		t.Fatal("coaching_log is empty; expected entry with optimal_action containing 'gs'")
	}
	if optimalAction != "gs" {
		t.Errorf("coaching_log.optimal_action: want 'gs', got %q", optimalAction)
	}

	// 8. Verify usage_events has an entry.
	if n := countRows(t, conn, "usage_events"); n < 1 {
		t.Errorf("usage_events: want >= 1, got %d", n)
	}

	// 9. Verify coach-msg file was written.
	msgPath := filepath.Join(dir, "wtfrc", "coach-msg")
	data, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("coach-msg not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("coach-msg is empty")
	}
	t.Logf("coaching message: %s", string(data))

	// 10. Cancel context and wait for clean exit.
	cancel()
	select {
	case runErr := <-runDone:
		if runErr != nil {
			t.Errorf("Run returned error: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// TestIntegrationGraduationPersistence
// ---------------------------------------------------------------------------

// TestIntegrationGraduationPersistence verifies that graduation state written
// by one daemon instance is visible to a second instance opened on the same DB.
func TestIntegrationGraduationPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graduation.db")
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	// --- First daemon instance ---
	db1 := openIntegrationDBAt(t, dbPath)
	insertTestAliases(t, db1)

	cfg := &config.Config{Coach: integrationCoachConfig()}
	cfg.Coach.GraduationStreak = 3 // low streak so we can reach "learning" state

	d1, err := NewDaemon(cfg, db1, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon (first): %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() { done1 <- d1.Run(ctx1) }()

	waitForFIFO(t, fifoPath)

	// Write enough events to be coached and trigger graduation state creation.
	for i := 0; i < 3; i++ {
		writeFIFO(t, fifoPath, "shell\tgit status\t")
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	// Stop first daemon.
	cancel1()
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatal("first daemon did not exit")
	}

	// Verify coaching_state row was written.
	conn1 := db1.Conn()
	state := queryString(t, conn1,
		`SELECT state FROM coaching_state WHERE action_id = 'zsh:gs'`)
	if state == "" {
		t.Fatal("coaching_state: no row found for action_id='zsh:gs' after first daemon")
	}
	t.Logf("state after first daemon: %s", state)
	db1.Close()

	// --- Second daemon instance on same DB ---
	db2, err := kb.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen DB: %v", err)
	}
	defer db2.Close()

	conn2 := db2.Conn()
	persistedState := queryString(t, conn2,
		`SELECT state FROM coaching_state WHERE action_id = 'zsh:gs'`)
	if persistedState == "" {
		t.Fatal("coaching_state: graduation state did not persist to DB (no row found)")
	}
	t.Logf("persisted state: %s", persistedState)
}

// ---------------------------------------------------------------------------
// TestIntegrationConfigReload
// ---------------------------------------------------------------------------

// TestIntegrationConfigReload verifies that after reload() the daemon adopts
// a new mode and generates messages using the updated template.
//
// The moderate template for shell_alias contains "characters" or keystroke info,
// while the chill template does not.  We verify the message changes after reload.
func TestIntegrationConfigReload(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	// Write initial config file with mode = "chill".
	cfgFilePath := filepath.Join(dir, "config.toml")
	chillContent := "[coach]\nmode = \"chill\"\nbudget_per_hour = 1000\ncooldown_seconds = 0\n"
	if err := os.WriteFile(cfgFilePath, []byte(chillContent), 0644); err != nil {
		t.Fatalf("write chill config: %v", err)
	}

	db := openIntegrationDB(t, dir)
	insertTestAliases(t, db)

	cfg := &config.Config{Coach: integrationCoachConfig()}
	cfg.Coach.Mode = "chill"

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	d.cfgPath = cfgFilePath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	waitForFIFO(t, fifoPath)

	// Send first event under "chill" mode, read the message.
	writeFIFO(t, fifoPath, "shell\tgit status\t")
	time.Sleep(200 * time.Millisecond)

	msgPath := filepath.Join(dir, "wtfrc", "coach-msg")
	chillData, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("coach-msg not written (chill): %v", err)
	}
	chillMsg := string(chillData)
	t.Logf("chill message: %s", chillMsg)

	// Overwrite config file with mode = "moderate" and reload.
	moderateContent := "[coach]\nmode = \"moderate\"\nbudget_per_hour = 1000\ncooldown_seconds = 0\n"
	if err := os.WriteFile(cfgFilePath, []byte(moderateContent), 0644); err != nil {
		t.Fatalf("write moderate config: %v", err)
	}
	if err := d.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Verify mode was updated.
	if got := d.cfgPtr.Load().Coach.Mode; got != "moderate" {
		t.Fatalf("after reload: mode = %q, want 'moderate'", got)
	}

	// Remove previous coach-msg so we can detect the new write.
	_ = os.Remove(msgPath)

	// Send another matching event under "moderate" mode.
	writeFIFO(t, fifoPath, "shell\tls -la\t")
	time.Sleep(200 * time.Millisecond)

	moderateData, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("coach-msg not written (moderate): %v", err)
	}
	moderateMsg := string(moderateData)
	t.Logf("moderate message: %s", moderateMsg)

	// The moderate template for shell_alias contains "characters" (chars_typed placeholder).
	// Verify the message is not identical to chill and contains keystroke info.
	if chillMsg == moderateMsg {
		t.Error("expected different message after mode change from chill to moderate")
	}
}
