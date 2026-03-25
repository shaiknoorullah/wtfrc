package coach

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
	"github.com/shaiknoorullah/wtfrc/internal/config"
)

// Deliverer is the interface for all notification back-ends.
type Deliverer interface {
	Deliver(ctx context.Context, message string) error
}

// Dispatcher routes coaching messages to the appropriate Deliverer based on
// the event source.
type Dispatcher struct {
	routes   map[string]Deliverer // source → deliverer
	fallback Deliverer
}

// NewDispatcher builds a Dispatcher from the delivery config. runtimeDir is
// the XDG_RUNTIME_DIR (e.g. /run/user/1000) used to derive file paths.
func NewDispatcher(cfg *config.CoachDeliveryConfig, runtimeDir string) *Dispatcher {
	runtimeBase := filepath.Join(runtimeDir, "wtfrc")

	// makeSingle builds a single Deliverer for a named back-end.
	makeSingle := func(name string) Deliverer {
		switch name {
		case "inline":
			return &InlineShellDeliverer{
				msgPath: filepath.Join(runtimeBase, "coach-msg"),
			}
		case "dunst":
			return NewDunstDeliverer()
		case "status":
			return &TmuxStatusDeliverer{
				statusPath: filepath.Join(runtimeBase, "tmux-status"),
			}
		case "waybar":
			return &WaybarDeliverer{
				statusPath: filepath.Join(runtimeBase, "waybar-status.json"),
				signal:     cfg.WaybarSignal,
			}
		case "notify":
			return &NeovimDeliverer{}
		default:
			// Unknown back-end: fall back to DunstDeliverer so we degrade gracefully.
			return NewDunstDeliverer()
		}
	}

	routes := map[string]Deliverer{
		SourceShell:    makeSingle(cfg.Shell),
		SourceHyprland: makeSingle(cfg.Hyprland),
		SourceTmux:     makeSingle(cfg.Tmux),
		SourceNvim:     makeSingle(cfg.Neovim),
	}

	return &Dispatcher{
		routes:   routes,
		fallback: makeSingle(cfg.Default),
	}
}

// Send delivers message using the deliverer registered for source, or the
// fallback if no specific route exists.
func (d *Dispatcher) Send(ctx context.Context, message string, source string) error {
	deliverer, ok := d.routes[source]
	if !ok {
		deliverer = d.fallback
	}
	return deliverer.Deliver(ctx, message)
}

// ----------------------------------------------------------------------------
// InlineShellDeliverer
// ----------------------------------------------------------------------------

// InlineShellDeliverer writes the coaching message to a file in XDG_RUNTIME_DIR.
// The precmd shell hook reads this file and displays it inline.
type InlineShellDeliverer struct {
	msgPath string // e.g. /run/user/1000/wtfrc/coach-msg
}

// Deliver writes message to the configured path, creating parent dirs as needed.
func (d *InlineShellDeliverer) Deliver(_ context.Context, message string) error {
	if err := os.MkdirAll(filepath.Dir(d.msgPath), 0o755); err != nil {
		return fmt.Errorf("inline: mkdir: %w", err)
	}
	if err := os.WriteFile(d.msgPath, []byte(message), 0o644); err != nil {
		return fmt.Errorf("inline: write: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// DunstDeliverer
// ----------------------------------------------------------------------------

// DunstDeliverer sends a desktop notification via D-Bus (consumed by dunst or
// any compliant notification daemon).
type DunstDeliverer struct {
	conn *dbus.Conn // nil when no D-Bus session is available
}

// NewDunstDeliverer connects to the session bus. If the bus is unavailable
// (headless env), conn is set to nil so that Deliver fails gracefully.
func NewDunstDeliverer() *DunstDeliverer {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return &DunstDeliverer{conn: nil}
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return &DunstDeliverer{conn: nil}
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return &DunstDeliverer{conn: nil}
	}
	return &DunstDeliverer{conn: conn}
}

// Deliver sends a notification through the D-Bus connection.
func (d *DunstDeliverer) Deliver(_ context.Context, message string) error {
	if d.conn == nil {
		return fmt.Errorf("dunst: no D-Bus session available")
	}

	n := notify.Notification{
		AppName:       "wtfrc",
		ReplacesID:    0,
		AppIcon:       "",
		Summary:       "Coach",
		Body:          message,
		ExpireTimeout: notify.ExpireTimeoutSetByNotificationServer,
		Hints: map[string]dbus.Variant{
			"x-dunst-stack-tag": dbus.MakeVariant("wtfrc-coach"),
		},
	}

	_, err := notify.SendNotification(d.conn, n)
	if err != nil {
		return fmt.Errorf("dunst: send notification: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// TmuxStatusDeliverer
// ----------------------------------------------------------------------------

// TmuxStatusDeliverer writes the coaching message to a status file and then
// signals tmux to refresh its status bar.
type TmuxStatusDeliverer struct {
	statusPath string // e.g. /run/user/1000/wtfrc/tmux-status
}

// Deliver writes message to the status file and runs tmux refresh-client -S.
// A missing tmux binary or no running server is treated as a warning.
func (d *TmuxStatusDeliverer) Deliver(_ context.Context, message string) error {
	if err := os.MkdirAll(filepath.Dir(d.statusPath), 0o755); err != nil {
		return fmt.Errorf("tmux: mkdir: %w", err)
	}
	if err := os.WriteFile(d.statusPath, []byte(message), 0o644); err != nil {
		return fmt.Errorf("tmux: write: %w", err)
	}

	if err := exec.Command("tmux", "refresh-client", "-S").Run(); err != nil {
		log.Printf("wtfrc/coach: tmux refresh-client -S: %v (tmux may not be running)", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// WaybarDeliverer
// ----------------------------------------------------------------------------

// WaybarDeliverer writes a JSON status blob to a file and then signals waybar
// to re-read it via a real-time signal (SIGRTMIN+N).
type WaybarDeliverer struct {
	statusPath string // e.g. /run/user/1000/wtfrc/waybar-status.json
	signal     int    // SIGRTMIN+N offset (e.g. 8)
}

type waybarPayload struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Class   string `json:"class"`
}

// Deliver writes the JSON payload and pokes waybar via pkill.
func (d *WaybarDeliverer) Deliver(_ context.Context, message string) error {
	if err := os.MkdirAll(filepath.Dir(d.statusPath), 0o755); err != nil {
		return fmt.Errorf("waybar: mkdir: %w", err)
	}

	payload, err := json.Marshal(waybarPayload{
		Text:    message,
		Tooltip: message,
		Class:   "coaching",
	})
	if err != nil {
		return fmt.Errorf("waybar: marshal: %w", err)
	}

	if err := os.WriteFile(d.statusPath, payload, 0o644); err != nil {
		return fmt.Errorf("waybar: write: %w", err)
	}

	sigArg := "-SIGRTMIN+" + strconv.Itoa(d.signal)
	if err := exec.Command("pkill", sigArg, "waybar").Run(); err != nil {
		log.Printf("wtfrc/coach: pkill %s waybar: %v (waybar may not be running)", sigArg, err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// NeovimDeliverer
// ----------------------------------------------------------------------------

// NeovimDeliverer is a placeholder that will be wired to the Neovim RPC
// connection in Task 12. For now it is a no-op.
type NeovimDeliverer struct{}

// Deliver is a no-op until the Neovim RPC backend is implemented.
func (d *NeovimDeliverer) Deliver(_ context.Context, _ string) error {
	return nil
}
