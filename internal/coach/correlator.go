package coach

import (
	"sync"
	"time"
)

// timestampedEvent pairs an Event with the wall-clock time it was received.
type timestampedEvent struct {
	event *Event
	at    time.Time
}

// Correlator pairs keybind notification events with result events to determine
// whether an action was triggered by a keybind or by mouse/manual input.
//
// When a keybind event arrives it is buffered per-tool.  When a result event
// arrives for the same tool within the correlation window, the oldest buffered
// keybind is consumed and the result is suppressed (no coaching needed).  If no
// matching keybind is found within the window, the result is returned as a
// coaching opportunity.
type Correlator struct {
	window   time.Duration              // how long a keybind stays valid (default 150ms)
	keybinds map[string][]timestampedEvent // per-tool FIFO buffer of recent keybind events
	mu       sync.Mutex
	now      func() time.Time // injectable clock for testing
}

// NewCorrelator creates a Correlator.
//   - window: 0 → defaults to 150ms.
//   - now: nil → defaults to time.Now.
func NewCorrelator(window time.Duration, now func() time.Time) *Correlator {
	if window == 0 {
		window = 150 * time.Millisecond
	}
	if now == nil {
		now = time.Now
	}
	return &Correlator{
		window:   window,
		keybinds: make(map[string][]timestampedEvent),
		now:      now,
	}
}

// Process handles a single event.
//
//   - Keybind event: buffered by source tool; returns nil.
//   - Result event: if a keybind for the same tool exists within the window, it
//     is consumed and nil is returned (no coaching needed).  Otherwise the event
//     is returned as a coaching opportunity.
//
// Expired keybind entries (older than window) are purged from all buffers on
// every call.
func (c *Correlator) Process(ev *Event) *Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()

	// Expire stale entries from every tool's buffer.
	c.expire(now)

	if ev.IsKeybind {
		// Buffer the keybind notification; it is not a coaching event itself.
		c.keybinds[ev.Source] = append(c.keybinds[ev.Source], timestampedEvent{
			event: ev,
			at:    now,
		})
		return nil
	}

	// Result event: look for a matching keybind in the same tool's buffer.
	buf := c.keybinds[ev.Source]
	if len(buf) > 0 {
		// Consume the oldest (first) keybind — it satisfied this result.
		c.keybinds[ev.Source] = buf[1:]
		return nil // keybind-triggered action; no coaching needed
	}

	// No matching keybind found → coaching opportunity.
	return ev
}

// expire removes all timestampedEvent entries from every buffer whose timestamp
// is older than now-window.  Must be called with c.mu held.
func (c *Correlator) expire(now time.Time) {
	cutoff := now.Add(-c.window)
	for tool, buf := range c.keybinds {
		start := 0
		for start < len(buf) && buf[start].at.Before(cutoff) {
			start++
		}
		if start == len(buf) {
			delete(c.keybinds, tool)
		} else if start > 0 {
			c.keybinds[tool] = buf[start:]
		}
	}
}
