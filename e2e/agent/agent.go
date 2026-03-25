//go:build e2e

// Package agent provides guest-side test utilities for E2E tests.
// It runs inside the VM (or on the local host in local mode) and provides
// helpers for input simulation, D-Bus capture, and state assertions.
package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Agent provides test utilities for the guest environment.
type Agent struct {
	runtimeDir string
	dbPath     string
	db         *sql.DB
}

// NewAgent creates a new Agent. runtimeDir is XDG_RUNTIME_DIR,
// dbPath is the path to the wtfrc SQLite database.
func NewAgent(runtimeDir, dbPath string) (*Agent, error) {
	a := &Agent{
		runtimeDir: runtimeDir,
		dbPath:     dbPath,
	}

	if dbPath != "" {
		db, err := sql.Open("sqlite", dbPath+"?mode=ro")
		if err != nil {
			return nil, fmt.Errorf("open db: %w", err)
		}
		a.db = db
	}

	return a, nil
}

// Close releases resources.
func (a *Agent) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// --- Hyprland helpers ---

// HyprctlJSON runs hyprctl with JSON output and returns the raw JSON bytes.
func (a *Agent) HyprctlJSON(ctx context.Context, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-j"}, args...)
	cmd := exec.CommandContext(ctx, "hyprctl", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("hyprctl %v: %w", args, err)
	}
	return out, nil
}

// ActiveWindow returns the title and class of the currently active Hyprland window.
func (a *Agent) ActiveWindow(ctx context.Context) (title, class string, err error) {
	data, err := a.HyprctlJSON(ctx, "activewindow")
	if err != nil {
		return "", "", err
	}
	var win struct {
		Title string `json:"title"`
		Class string `json:"class"`
	}
	if err := json.Unmarshal(data, &win); err != nil {
		return "", "", fmt.Errorf("parse activewindow: %w", err)
	}
	return win.Title, win.Class, nil
}

// ClientCount returns the number of open windows.
func (a *Agent) ClientCount(ctx context.Context) (int, error) {
	data, err := a.HyprctlJSON(ctx, "clients")
	if err != nil {
		return 0, err
	}
	var clients []json.RawMessage
	if err := json.Unmarshal(data, &clients); err != nil {
		return 0, fmt.Errorf("parse clients: %w", err)
	}
	return len(clients), nil
}

// --- File-based assertions ---

// ReadCoachMsg reads the coach-msg file written by the inline shell deliverer.
func (a *Agent) ReadCoachMsg() (string, error) {
	path := filepath.Join(a.runtimeDir, "wtfrc", "coach-msg")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ClearCoachMsg removes the coach-msg file.
func (a *Agent) ClearCoachMsg() error {
	path := filepath.Join(a.runtimeDir, "wtfrc", "coach-msg")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// --- Database assertions ---

// QueryCoachingLogCount returns the number of rows in coaching_log.
func (a *Agent) QueryCoachingLogCount(ctx context.Context) (int, error) {
	if a.db == nil {
		return 0, fmt.Errorf("no database connection")
	}
	var count int
	err := a.db.QueryRowContext(ctx, "SELECT count(*) FROM coaching_log").Scan(&count)
	return count, err
}

// QueryCoachingLogOptimalAction returns the optimal_action of the latest coaching_log entry.
func (a *Agent) QueryCoachingLogOptimalAction(ctx context.Context) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("no database connection")
	}
	var action string
	err := a.db.QueryRowContext(ctx,
		"SELECT optimal_action FROM coaching_log ORDER BY id DESC LIMIT 1").Scan(&action)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return action, err
}

// QueryUsageEventsCount returns the number of rows in usage_events.
func (a *Agent) QueryUsageEventsCount(ctx context.Context) (int, error) {
	if a.db == nil {
		return 0, fmt.Errorf("no database connection")
	}
	var count int
	err := a.db.QueryRowContext(ctx, "SELECT count(*) FROM usage_events").Scan(&count)
	return count, err
}

// QueryGraduationState returns the state field for a given action_id in coaching_state.
func (a *Agent) QueryGraduationState(ctx context.Context, actionID string) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("no database connection")
	}
	var state string
	err := a.db.QueryRowContext(ctx,
		"SELECT state FROM coaching_state WHERE action_id = ?", actionID).Scan(&state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return state, err
}

// QueryCoachingLogMode returns the mode field of the latest coaching_log entry.
func (a *Agent) QueryCoachingLogMode(ctx context.Context) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("no database connection")
	}
	var mode string
	err := a.db.QueryRowContext(ctx,
		"SELECT mode FROM coaching_log ORDER BY id DESC LIMIT 1").Scan(&mode)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return mode, err
}

// --- tmux helpers ---

// TmuxListPanes returns the output of `tmux list-panes`.
func (a *Agent) TmuxListPanes(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "list-panes")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux list-panes: %w", err)
	}
	return string(out), nil
}

// TmuxSendKeys sends keys to the current tmux pane.
func (a *Agent) TmuxSendKeys(ctx context.Context, keys ...string) error {
	args := append([]string{"send-keys"}, keys...)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd.Run()
}

// --- FIFO helpers ---

// WriteFIFO writes a message to the coach FIFO.
func (a *Agent) WriteFIFO(message string) error {
	fifoPath := filepath.Join(a.runtimeDir, "wtfrc", "coach.fifo")
	f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open fifo: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(message + "\n")
	return err
}

// --- General helpers ---

// WaitForFile waits for a file to exist and contain expected content.
func (a *Agent) WaitForFile(path, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), expected) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("file %s did not contain %q within %v", path, expected, timeout)
}

// FileExists returns true if the given path exists.
func (a *Agent) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
