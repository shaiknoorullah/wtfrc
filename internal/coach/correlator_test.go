package coach

import (
	"testing"
	"time"
)

// newFakeClock returns a pointer-based fake clock whose value can be advanced.
func newFakeClock(t0 time.Time) *fakeClock {
	return &fakeClock{now: t0}
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

// helpers to build events quickly.
func keybindEvent(source string) *Event {
	return &Event{Source: source, Action: "some-action", IsKeybind: true}
}

func resultEvent(source string) *Event {
	return &Event{Source: source, Action: "some-action", IsKeybind: false}
}

// TestCorrelatorKeybindThenResult: keybind at T=0, result at T=50ms → nil (consumed).
func TestCorrelatorKeybindThenResult(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	kb := keybindEvent(SourceHyprland)
	got := c.Process(kb)
	if got != nil {
		t.Fatalf("Process(keybind): expected nil, got %+v", got)
	}

	clk.Advance(50 * time.Millisecond)
	res := resultEvent(SourceHyprland)
	got = c.Process(res)
	if got != nil {
		t.Fatalf("Process(result after keybind within window): expected nil (consumed), got %+v", got)
	}
}

// TestCorrelatorResultWithoutKeybind: result with no preceding keybind → coaching opportunity.
func TestCorrelatorResultWithoutKeybind(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	res := resultEvent(SourceHyprland)
	got := c.Process(res)
	if got == nil {
		t.Fatal("Process(result without keybind): expected non-nil coaching event, got nil")
	}
	if got.Source != SourceHyprland {
		t.Errorf("returned event Source: got %q, want %q", got.Source, SourceHyprland)
	}
}

// TestCorrelatorExpiredKeybind: keybind at T=0, result at T=200ms (past 150ms window) → coaching opportunity.
func TestCorrelatorExpiredKeybind(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	kb := keybindEvent(SourceHyprland)
	c.Process(kb)

	clk.Advance(200 * time.Millisecond)
	res := resultEvent(SourceHyprland)
	got := c.Process(res)
	if got == nil {
		t.Fatal("Process(result after expired keybind): expected non-nil coaching event, got nil")
	}
}

// TestCorrelatorCrossToolNoMatch: keybind for tmux, result for hyprland → coaching opportunity.
func TestCorrelatorCrossToolNoMatch(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	kb := keybindEvent(SourceTmux)
	c.Process(kb)

	clk.Advance(50 * time.Millisecond)
	res := resultEvent(SourceHyprland)
	got := c.Process(res)
	if got == nil {
		t.Fatal("Process(result for different tool): expected non-nil coaching event, got nil")
	}
}

// TestCorrelatorMultipleKeybinds: 3 rapid keybinds, 1 result → nil; 2 keybinds remain.
func TestCorrelatorMultipleKeybinds(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	for i := 0; i < 3; i++ {
		c.Process(keybindEvent(SourceHyprland))
	}

	res := resultEvent(SourceHyprland)
	got := c.Process(res)
	if got != nil {
		t.Fatalf("Process(result after 3 keybinds): expected nil, got %+v", got)
	}

	// Verify 2 keybinds remain in the buffer.
	c.mu.Lock()
	remaining := len(c.keybinds[SourceHyprland])
	c.mu.Unlock()
	if remaining != 2 {
		t.Errorf("buffer after consuming one keybind: got %d, want 2", remaining)
	}
}

// TestCorrelatorCleanup: keybind at T=0, clock advances to T=500ms, unrelated result
// from different tool is processed → old keybind cleaned from buffer.
func TestCorrelatorCleanup(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	c := NewCorrelator(150*time.Millisecond, clk.Now)

	c.Process(keybindEvent(SourceHyprland))

	// Advance far past the window.
	clk.Advance(500 * time.Millisecond)

	// Process a result for a different tool; expiry runs on every Process call.
	c.Process(resultEvent(SourceTmux))

	c.mu.Lock()
	remaining := len(c.keybinds[SourceHyprland])
	c.mu.Unlock()
	if remaining != 0 {
		t.Errorf("stale keybind not cleaned up: got %d entries, want 0", remaining)
	}
}

// TestCorrelatorDefaultWindow: zero window → defaults to 150ms.
func TestCorrelatorDefaultWindow(t *testing.T) {
	c := NewCorrelator(0, nil)
	if c.window != 150*time.Millisecond {
		t.Errorf("default window: got %v, want 150ms", c.window)
	}
}
