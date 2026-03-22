package coach

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/config"
)

// mockDeliverer records delivered messages for assertions.
type mockDeliverer struct {
	messages []string
}

func (m *mockDeliverer) Deliver(_ context.Context, message string) error {
	m.messages = append(m.messages, message)
	return nil
}

func (m *mockDeliverer) called() bool { return len(m.messages) > 0 }

// ----------------------------------------------------------------------------
// TestDispatcherRouting
// ----------------------------------------------------------------------------

func TestDispatcherRouting(t *testing.T) {
	mockA := &mockDeliverer{} // shell
	mockB := &mockDeliverer{} // hyprland
	mockC := &mockDeliverer{} // fallback

	d := &Dispatcher{
		routes: map[string]Deliverer{
			SourceShell:    mockA,
			SourceHyprland: mockB,
		},
		fallback: mockC,
	}

	tests := []struct {
		source   string
		expected *mockDeliverer
		label    string
	}{
		{SourceShell, mockA, "shell → mockA"},
		{SourceHyprland, mockB, "hyprland → mockB"},
		{SourceTmux, mockC, "tmux → fallback mockC"},
		{"unknown", mockC, "unknown → fallback mockC"},
	}

	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			// Reset all mocks.
			mockA.messages = nil
			mockB.messages = nil
			mockC.messages = nil

			err := d.Send(context.Background(), "hello", tc.source)
			if err != nil {
				t.Fatalf("Send returned error: %v", err)
			}
			if !tc.expected.called() {
				t.Errorf("expected deliverer was not called")
			}
			// Ensure the other mocks were NOT called.
			for _, other := range []*mockDeliverer{mockA, mockB, mockC} {
				if other != tc.expected && other.called() {
					t.Errorf("unexpected deliverer was called")
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TestDispatcherConfigOverride
// ----------------------------------------------------------------------------

func TestDispatcherConfigOverride(t *testing.T) {
	// Override shell to "dunst". We can't actually connect to D-Bus in CI,
	// so we just verify the route maps to a *DunstDeliverer (type assertion).
	cfg := &config.CoachDeliveryConfig{
		Shell:    "dunst",
		Hyprland: "dunst",
		Tmux:     "status",
		Neovim:   "notify",
		Default:  "dunst",
	}

	d := NewDispatcher(cfg, t.TempDir())

	deliverer, ok := d.routes[SourceShell]
	if !ok {
		t.Fatalf("shell route not found in dispatcher")
	}
	if _, isDunst := deliverer.(*DunstDeliverer); !isDunst {
		t.Errorf("expected *DunstDeliverer for shell=dunst, got %T", deliverer)
	}
}

// ----------------------------------------------------------------------------
// TestInlineShellDeliverer
// ----------------------------------------------------------------------------

func TestInlineShellDeliverer(t *testing.T) {
	dir := t.TempDir()
	msgPath := filepath.Join(dir, "wtfrc", "coach-msg")

	d := &InlineShellDeliverer{msgPath: msgPath}

	const msg1 = "first coaching message"
	if err := d.Deliver(context.Background(), msg1); err != nil {
		t.Fatalf("Deliver error: %v", err)
	}

	data, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != msg1 {
		t.Errorf("file content = %q, want %q", string(data), msg1)
	}

	// Second delivery must overwrite (not append).
	const msg2 = "second coaching message"
	if err := d.Deliver(context.Background(), msg2); err != nil {
		t.Fatalf("Deliver error: %v", err)
	}

	data2, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data2) != msg2 {
		t.Errorf("after overwrite file content = %q, want %q", string(data2), msg2)
	}
	if string(data2) == msg1+msg2 {
		t.Errorf("file was appended instead of overwritten")
	}
}

// ----------------------------------------------------------------------------
// TestTmuxStatusDeliverer
// ----------------------------------------------------------------------------

func TestTmuxStatusDeliverer(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "wtfrc", "tmux-status")

	d := &TmuxStatusDeliverer{statusPath: statusPath}

	const msg = "tmux coaching hint"
	// Deliver will write the file and attempt tmux refresh; tmux may not be
	// running in CI but that is a non-fatal warning — Deliver should not
	// return an error for a missing tmux.
	_ = d.Deliver(context.Background(), msg)

	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != msg {
		t.Errorf("tmux status file = %q, want %q", string(data), msg)
	}
}

// ----------------------------------------------------------------------------
// TestWaybarDeliverer
// ----------------------------------------------------------------------------

func TestWaybarDeliverer(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "wtfrc", "waybar-status.json")

	d := &WaybarDeliverer{statusPath: statusPath, signal: 8}

	const msg = "waybar coaching hint"
	// pkill may not find waybar in CI but the file should still be written.
	_ = d.Deliver(context.Background(), msg)

	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if payload["text"] != msg {
		t.Errorf("JSON text = %q, want %q", payload["text"], msg)
	}
}

// ----------------------------------------------------------------------------
// TestDunstDeliverer
// ----------------------------------------------------------------------------

func TestDunstDelivererGracefulWithoutDBus(t *testing.T) {
	// This test verifies that NewDunstDeliverer does not panic when D-Bus
	// session bus is unavailable (e.g., in a headless CI environment).
	d := NewDunstDeliverer()
	if d == nil {
		t.Fatal("NewDunstDeliverer returned nil")
	}
	// If no D-Bus session, conn should be nil and Deliver should return an error
	// (not panic).
	if d.conn == nil {
		err := d.Deliver(context.Background(), "test")
		if err == nil {
			t.Error("expected error when D-Bus conn is nil, got nil")
		}
		return
	}
	// D-Bus is available — we don't send a real notification in tests.
	t.Log("D-Bus session available; skipping notification send")
}

// ----------------------------------------------------------------------------
// TestNeovimDeliverer
// ----------------------------------------------------------------------------

func TestNeovimDeliverer(t *testing.T) {
	d := &NeovimDeliverer{}
	if err := d.Deliver(context.Background(), "anything"); err != nil {
		t.Errorf("NeovimDeliverer placeholder returned error: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TestNewDispatcherDefaults
// ----------------------------------------------------------------------------

func TestNewDispatcherDefaults(t *testing.T) {
	cfg := &config.CoachDeliveryConfig{
		Shell:    "inline",
		Hyprland: "dunst",
		Tmux:     "status",
		Neovim:   "notify",
		Default:  "dunst",
	}

	d := NewDispatcher(cfg, t.TempDir())

	// shell → InlineShellDeliverer
	if r, ok := d.routes[SourceShell]; !ok {
		t.Error("missing shell route")
	} else if _, ok := r.(*InlineShellDeliverer); !ok {
		t.Errorf("shell route: want *InlineShellDeliverer, got %T", r)
	}

	// hyprland → DunstDeliverer
	if r, ok := d.routes[SourceHyprland]; !ok {
		t.Error("missing hyprland route")
	} else if _, ok := r.(*DunstDeliverer); !ok {
		t.Errorf("hyprland route: want *DunstDeliverer, got %T", r)
	}

	// tmux → TmuxStatusDeliverer
	if r, ok := d.routes[SourceTmux]; !ok {
		t.Error("missing tmux route")
	} else if _, ok := r.(*TmuxStatusDeliverer); !ok {
		t.Errorf("tmux route: want *TmuxStatusDeliverer, got %T", r)
	}

	// nvim → NeovimDeliverer
	if r, ok := d.routes[SourceNvim]; !ok {
		t.Error("missing nvim route")
	} else if _, ok := r.(*NeovimDeliverer); !ok {
		t.Errorf("nvim route: want *NeovimDeliverer, got %T", r)
	}

	// fallback → DunstDeliverer
	if d.fallback == nil {
		t.Error("fallback deliverer is nil")
	} else if _, ok := d.fallback.(*DunstDeliverer); !ok {
		t.Errorf("fallback: want *DunstDeliverer, got %T", d.fallback)
	}
}
