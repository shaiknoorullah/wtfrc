//go:build e2e

// wtfrc-agent is the guest-side E2E test executor. It runs inside the VM
// (or on the local host) and receives JSON commands on stdin, dispatches
// them to the appropriate agent helpers, and returns JSON results on stdout.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaiknoorullah/wtfrc/e2e/agent"

	_ "modernc.org/sqlite"
)

// request is the JSON envelope received on stdin.
type request struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
}

// response is the JSON envelope written to stdout.
type response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("wtfrc-agent: ")

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}

	dbPath := os.Getenv("WTFRC_DB_PATH")
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".local", "share", "wtfrc", "kb.db")
	}

	a, err := agent.NewAgent(runtimeDir, dbPath)
	if err != nil {
		log.Printf("warning: agent init (non-fatal): %v", err)
		// Create agent without DB -- it may still be useful for non-DB actions
		a, _ = agent.NewAgent(runtimeDir, "")
	}
	defer a.Close()

	// Try to create a real uinput device; fall back to nil (errors on use).
	inputDev := initInputDevice()

	// D-Bus capture state (lazily created).
	var dbusCapture *agent.DBusCapture

	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	// Allow large lines (1 MB).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(response{OK: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
			continue
		}

		resp := dispatch(req, a, inputDev, &dbusCapture, dbPath)
		if err := enc.Encode(resp); err != nil {
			log.Printf("encode response: %v", err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("stdin read error: %v", err)
	}

	// Cleanup D-Bus capture if active.
	if dbusCapture != nil {
		dbusCapture.Stop()
	}
}

// initInputDevice tries to create a real UinputDevice. If /dev/uinput is
// not available, it returns nil. Actions that need input will return an error.
func initInputDevice() agent.InputDevice {
	if _, err := os.Stat("/dev/uinput"); err != nil {
		log.Printf("warning: /dev/uinput not available, input simulation disabled")
		return nil
	}
	dev, err := agent.NewUinputDevice()
	if err != nil {
		log.Printf("warning: failed to create uinput device: %v", err)
		return nil
	}
	return dev
}

// dispatch routes a request to the appropriate handler.
func dispatch(
	req request,
	a *agent.Agent,
	inputDev agent.InputDevice,
	dbusCapture **agent.DBusCapture,
	dbPath string,
) response {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch req.Action {
	case "type_text":
		return handleTypeText(req.Params, inputDev)
	case "press_combo":
		return handlePressCombo(req.Params, inputDev)
	case "mouse_click":
		return handleMouseClick(req.Params, inputDev)
	case "query_db":
		return handleQueryDB(req.Params, dbPath)
	case "read_file":
		return handleReadFile(req.Params)
	case "write_fifo":
		return handleWriteFIFO(req.Params, a)
	case "wait_notification":
		return handleWaitNotification(req.Params, dbusCapture)
	case "hyprctl":
		return handleHyprctl(ctx, req.Params, a)
	case "tmux":
		return handleTmux(ctx, req.Params)
	case "start_dbus_capture":
		return handleStartDBusCapture(ctx, dbusCapture)
	case "stop_dbus_capture":
		return handleStopDBusCapture(dbusCapture)
	case "get_notifications":
		return handleGetNotifications(dbusCapture)
	case "clear_notifications":
		return handleClearNotifications(dbusCapture)
	default:
		return response{OK: false, Error: fmt.Sprintf("unknown action: %q", req.Action)}
	}
}

// --- Input handlers ---

func requireInputDev(dev agent.InputDevice) *response {
	if dev == nil {
		return &response{OK: false, Error: "input device not available (/dev/uinput not accessible)"}
	}
	return nil
}

func handleTypeText(params json.RawMessage, dev agent.InputDevice) response {
	if r := requireInputDev(dev); r != nil {
		return *r
	}
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	if err := agent.TypeText(dev, p.Text); err != nil {
		return response{OK: false, Error: fmt.Sprintf("type_text: %v", err)}
	}
	return response{OK: true}
}

func handlePressCombo(params json.RawMessage, dev agent.InputDevice) response {
	if r := requireInputDev(dev); r != nil {
		return *r
	}
	var p struct {
		Combo string `json:"combo"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	combo, err := agent.ParseKeyCombo(p.Combo)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("parse combo: %v", err)}
	}
	if err := agent.PressCombo(dev, combo); err != nil {
		return response{OK: false, Error: fmt.Sprintf("press_combo: %v", err)}
	}
	return response{OK: true}
}

func handleMouseClick(params json.RawMessage, dev agent.InputDevice) response {
	if r := requireInputDev(dev); r != nil {
		return *r
	}
	var p struct {
		X int32 `json:"x"`
		Y int32 `json:"y"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	// Move the mouse to the target position and click.
	if err := dev.MouseMove(p.X, p.Y); err != nil {
		return response{OK: false, Error: fmt.Sprintf("mouse_move: %v", err)}
	}
	time.Sleep(agent.InputDelay)
	if err := dev.MouseClick(); err != nil {
		return response{OK: false, Error: fmt.Sprintf("mouse_click: %v", err)}
	}
	return response{OK: true}
}

// --- Database handler ---

func handleQueryDB(params json.RawMessage, dbPath string) response {
	var p struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	if p.SQL == "" {
		return response{OK: false, Error: "empty SQL query"}
	}

	// Open a fresh read-only connection for each query to avoid locking issues
	// with the running daemon.
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("open db: %v", err)}
	}
	defer db.Close()

	rows, err := db.Query(p.SQL)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("query: %v", err)}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("columns: %v", err)}
	}

	var result [][]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return response{OK: false, Error: fmt.Sprintf("scan: %v", err)}
		}
		// Convert []byte values to strings for JSON serialization.
		row := make([]any, len(values))
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = v
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return response{OK: false, Error: fmt.Sprintf("rows iteration: %v", err)}
	}

	return response{OK: true, Data: result}
}

// --- File handlers ---

func handleReadFile(params json.RawMessage) response {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("read_file: %v", err)}
	}
	return response{OK: true, Data: string(data)}
}

func handleWriteFIFO(params json.RawMessage, a *agent.Agent) response {
	var p struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	if err := a.WriteFIFO(p.Message); err != nil {
		return response{OK: false, Error: fmt.Sprintf("write_fifo: %v", err)}
	}
	return response{OK: true}
}

// --- D-Bus handlers ---

func handleStartDBusCapture(ctx context.Context, capture **agent.DBusCapture) response {
	if *capture != nil {
		(*capture).Stop()
	}
	c, err := agent.NewDBusCapture()
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("create dbus capture: %v", err)}
	}
	if err := c.Start(ctx); err != nil {
		return response{OK: false, Error: fmt.Sprintf("start dbus capture: %v", err)}
	}
	*capture = c
	return response{OK: true}
}

func handleStopDBusCapture(capture **agent.DBusCapture) response {
	if *capture == nil {
		return response{OK: false, Error: "no active dbus capture"}
	}
	(*capture).Stop()
	*capture = nil
	return response{OK: true}
}

func handleGetNotifications(capture **agent.DBusCapture) response {
	if *capture == nil {
		return response{OK: false, Error: "no active dbus capture"}
	}
	notifications := (*capture).Notifications()
	return response{OK: true, Data: notifications}
}

func handleClearNotifications(capture **agent.DBusCapture) response {
	if *capture == nil {
		return response{OK: false, Error: "no active dbus capture"}
	}
	(*capture).Clear()
	return response{OK: true}
}

func handleWaitNotification(params json.RawMessage, capture **agent.DBusCapture) response {
	if *capture == nil {
		return response{OK: false, Error: "no active dbus capture (call start_dbus_capture first)"}
	}
	var p struct {
		Contains  string `json:"contains"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	n, err := (*capture).WaitForNotificationContaining(timeout, p.Contains)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("wait_notification: %v", err)}
	}
	return response{OK: true, Data: n}
}

// --- Hyprctl handler ---

func handleHyprctl(ctx context.Context, params json.RawMessage, a *agent.Agent) response {
	var p struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	if len(p.Args) == 0 {
		return response{OK: false, Error: "hyprctl: args must not be empty"}
	}
	data, err := a.HyprctlJSON(ctx, p.Args...)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("hyprctl: %v", err)}
	}
	// Return raw JSON -- parse it into any so it nests properly in the response.
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		// If it's not valid JSON, return as string.
		return response{OK: true, Data: strings.TrimSpace(string(data))}
	}
	return response{OK: true, Data: parsed}
}

// --- tmux handler ---

func handleTmux(ctx context.Context, params json.RawMessage) response {
	var p struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{OK: false, Error: fmt.Sprintf("invalid params: %v", err)}
	}
	if len(p.Args) == 0 {
		return response{OK: false, Error: "tmux: args must not be empty"}
	}
	cmd := exec.CommandContext(ctx, "tmux", p.Args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("tmux %v: %v: %s", p.Args, err, string(out))}
	}
	return response{OK: true, Data: strings.TrimSpace(string(out))}
}
