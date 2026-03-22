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
// Helpers
// ---------------------------------------------------------------------------

// openDaemonTestDB opens a full kb.DB in a temp dir using the real schema
// (which includes usage_events, coaching_log, coaching_state, etc.).
func openDaemonTestDB(t *testing.T) (*kb.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon_test.db")
	db, err := kb.Open(path)
	if err != nil {
		t.Fatalf("openDaemonTestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

// defaultCoachConfig returns a CoachConfig suitable for tests (permissive).
func defaultCoachConfig() config.CoachConfig {
	return config.CoachConfig{
		Enabled:          true,
		Mode:             "chill",
		BudgetPerHour:    100,
		CooldownSeconds:  0,
		QuietHours:       "",
		FocusCategory:    "",
		GraduationStreak: 7,
		Delivery: config.CoachDeliveryConfig{
			Shell:    "inline",
			Hyprland: "inline",
			Tmux:     "inline",
			Neovim:   "inline",
			Default:  "inline",
		},
	}
}

// insertAliasEntry inserts a zsh alias "gs" → "git status" into the kb.DB.
func insertAliasEntry(t *testing.T, db *kb.DB) {
	t.Helper()
	binding := "gs"
	action := "git status"
	_, err := db.InsertEntry(kb.KBEntry{
		Tool:        "zsh",
		Type:        parsers.EntryAlias,
		RawBinding:  &binding,
		RawAction:   &action,
		Description: "git status shortcut",
		SourceFile:  "~/.zshrc",
		SourceLine:  10,
		FileHash:    "abc123",
		IndexedAt:   time.Now(),
	}, nil)
	if err != nil {
		t.Fatalf("insertAliasEntry: %v", err)
	}
}

// waitForFIFO polls until the FIFO file exists, failing if it takes too long.
func waitForFIFO(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for FIFO at %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writeFIFO opens the FIFO for writing and writes the given line.
func writeFIFO(t *testing.T, path string, line string) {
	t.Helper()
	wf, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("writeFIFO open: %v", err)
	}
	defer wf.Close()
	if _, err := wf.WriteString(line + "\n"); err != nil {
		t.Fatalf("writeFIFO write: %v", err)
	}
}

// countRows returns the number of rows in a table.
func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
	if err != nil {
		t.Fatalf("countRows(%s): %v", table, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// TestDaemonShutdown: cancel context → Run returns nil
// ---------------------------------------------------------------------------

func TestDaemonShutdown(t *testing.T) {
	db, _ := openDaemonTestDB(t)
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(ctx)
	}()

	// Wait for FIFO creation.
	waitForFIFO(t, fifoPath)

	// Cancel context and expect clean shutdown.
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run should return nil on context cancel, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// TestDaemonNoMatch: event with no alias → usage_events recorded, no coaching
// ---------------------------------------------------------------------------

func TestDaemonNoMatch(t *testing.T) {
	db, _ := openDaemonTestDB(t)
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}

	// No aliases inserted — nothing to match.
	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = d.Run(ctx) }()

	waitForFIFO(t, fifoPath)

	// Write an event that has no matching alias.
	writeFIFO(t, fifoPath, "shell\tgit commit -m test\t")

	// Allow async processing.
	time.Sleep(200 * time.Millisecond)

	conn := db.Conn()

	// usage_events must have 1 row.
	if n := countRows(t, conn, "usage_events"); n != 1 {
		t.Errorf("usage_events: want 1, got %d", n)
	}

	// coaching_log must be empty (no match → no coaching).
	if n := countRows(t, conn, "coaching_log"); n != 0 {
		t.Errorf("coaching_log: want 0, got %d", n)
	}

	// No coach-msg file should be written.
	msgPath := filepath.Join(dir, "wtfrc", "coach-msg")
	if _, err := os.Stat(msgPath); err == nil {
		t.Error("coach-msg file should not exist when there is no match")
	}
}

// ---------------------------------------------------------------------------
// TestDaemonPipeline: matching event → usage_events + coaching_log + msg file
// ---------------------------------------------------------------------------

func TestDaemonPipeline(t *testing.T) {
	db, _ := openDaemonTestDB(t)
	insertAliasEntry(t, db)

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = d.Run(ctx) }()

	waitForFIFO(t, fifoPath)

	// Write the long form that has an alias.
	writeFIFO(t, fifoPath, "shell\tgit status\t")

	// Allow async processing.
	time.Sleep(300 * time.Millisecond)

	conn := db.Conn()

	// usage_events must have 1 row.
	if n := countRows(t, conn, "usage_events"); n != 1 {
		t.Errorf("usage_events: want 1, got %d", n)
	}

	// coaching_log must have 1 row.
	if n := countRows(t, conn, "coaching_log"); n != 1 {
		t.Errorf("coaching_log: want 1, got %d", n)
	}

	// The coach-msg file must exist (inline shell deliverer).
	msgPath := filepath.Join(dir, "wtfrc", "coach-msg")
	data, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("coach-msg not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("coach-msg is empty")
	}
	t.Logf("coaching message: %s", string(data))
}

// ---------------------------------------------------------------------------
// TestDaemonThrottled: budget=1, two matching events → only first gets coached
// ---------------------------------------------------------------------------

func TestDaemonThrottled(t *testing.T) {
	db, _ := openDaemonTestDB(t)
	insertAliasEntry(t, db)

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}
	cfg.Coach.BudgetPerHour = 1
	cfg.Coach.CooldownSeconds = 0

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = d.Run(ctx) }()

	waitForFIFO(t, fifoPath)

	// Write two matching events.
	writeFIFO(t, fifoPath, "shell\tgit status\t")
	time.Sleep(150 * time.Millisecond)
	writeFIFO(t, fifoPath, "shell\tgit status\t")

	// Allow async processing.
	time.Sleep(300 * time.Millisecond)

	conn := db.Conn()

	// Both events recorded in usage_events.
	if n := countRows(t, conn, "usage_events"); n != 2 {
		t.Errorf("usage_events: want 2, got %d", n)
	}

	// Only 1 coaching entry (throttle blocked the second).
	if n := countRows(t, conn, "coaching_log"); n != 1 {
		t.Errorf("coaching_log: want 1 (throttled), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonReload: reload updates mode in config pointer
// ---------------------------------------------------------------------------

func TestDaemonReload(t *testing.T) {
	db, _ := openDaemonTestDB(t)

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}
	cfg.Coach.Mode = "chill"

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// Verify initial mode.
	loaded := d.cfgPtr.Load()
	if loaded.Coach.Mode != "chill" {
		t.Errorf("initial mode: want chill, got %s", loaded.Coach.Mode)
	}

	// Simulate reload by directly updating cfgPtr (reload reads from disk;
	// here we just verify the atomic pointer swap works correctly).
	newCfg := *cfg
	newCfg.Coach.Mode = "moderate"
	d.cfgPtr.Store(&newCfg)

	loaded2 := d.cfgPtr.Load()
	if loaded2.Coach.Mode != "moderate" {
		t.Errorf("after reload mode: want moderate, got %s", loaded2.Coach.Mode)
	}

	// Also verify d.cfg was updated via d.reload (using a write a config file).
	cfgFilePath := filepath.Join(dir, "config.toml")
	content := `
[coach]
mode = "strict"
budget_per_hour = 100
cooldown_seconds = 0
`
	if err := os.WriteFile(cfgFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d.cfgPath = cfgFilePath

	if err := d.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	loaded3 := d.cfgPtr.Load()
	if loaded3.Coach.Mode != "strict" {
		t.Errorf("after file reload mode: want strict, got %s", loaded3.Coach.Mode)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonKeybindCorrelation: keybind+result pair → no coaching
// ---------------------------------------------------------------------------

func TestDaemonKeybindCorrelation(t *testing.T) {
	db, _ := openDaemonTestDB(t)
	insertAliasEntry(t, db)

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")
	runtimeDir := dir

	cfg := &config.Config{Coach: defaultCoachConfig()}

	d, err := NewDaemon(cfg, db, nil, fifoPath, runtimeDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = d.Run(ctx) }()

	waitForFIFO(t, fifoPath)

	// Send a keybind event followed immediately by the result event.
	// The correlator should suppress coaching.
	writeFIFO(t, fifoPath, "shell\tkb:git status\t")
	time.Sleep(20 * time.Millisecond)
	writeFIFO(t, fifoPath, "shell\tgit status\t")

	// Allow async processing.
	time.Sleep(300 * time.Millisecond)

	conn := db.Conn()

	// usage_events: both events recorded.
	if n := countRows(t, conn, "usage_events"); n != 2 {
		t.Errorf("usage_events: want 2, got %d", n)
	}

	// coaching_log: no coaching because keybind was used.
	if n := countRows(t, conn, "coaching_log"); n != 0 {
		t.Errorf("coaching_log: want 0 (keybind suppressed), got %d", n)
	}
}
