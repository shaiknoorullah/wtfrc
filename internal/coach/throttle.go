package coach

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
)

// sourceCategory maps event sources to their coaching category.
var sourceCategory = map[string]string{
	SourceShell:       "shell",
	SourceHyprland:    "wm",
	SourceKitty:       "wm",
	SourceQutebrowser: "wm",
	SourceYazi:        "wm",
	SourceNvim:        "editor",
	SourceTmux:        "terminal",
}

// Throttle checks 5 anti-annoyance gates before allowing a coaching message.
// If ANY gate blocks, coaching is suppressed.
type Throttle struct {
	cfg        *config.CoachConfig
	grad       *GraduationManager
	lastSeen   map[string]time.Time // per-action last-allowed timestamp
	mu         sync.Mutex
	budget     int              // messages remaining this hour
	budgetHour int              // which hour the budget applies to
	now        func() time.Time // injectable clock for testing
}

// NewThrottle creates a Throttle with the given config and graduation manager.
// If now is nil, time.Now is used.
func NewThrottle(cfg *config.CoachConfig, grad *GraduationManager, now func() time.Time) *Throttle {
	if now == nil {
		now = time.Now
	}
	return &Throttle{
		cfg:        cfg,
		grad:       grad,
		lastSeen:   make(map[string]time.Time),
		budget:     cfg.BudgetPerHour,
		budgetHour: -1, // sentinel: uninitialized
		now:        now,
	}
}

// Allow runs all 5 throttle gates and returns true only if all pass.
// Gates are checked in order; the first to fail short-circuits.
func (t *Throttle) Allow(actionID string, source string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()

	// Gate 1: Quiet hours.
	if t.cfg.QuietHours != "" {
		start, end, err := parseQuietHours(t.cfg.QuietHours)
		if err == nil && isQuietTime(now, start, end) {
			return false
		}
	}

	// Gate 2: Focus mode.
	if t.cfg.FocusCategory != "" {
		cat := sourceCategory[source]
		if cat != t.cfg.FocusCategory {
			return false
		}
	}

	// Gate 3: Hourly budget.
	currentHour := now.Hour()
	if t.budgetHour != currentHour {
		t.budget = t.cfg.BudgetPerHour
		t.budgetHour = currentHour
	}
	if t.budget <= 0 {
		return false
	}
	t.budget--

	// Gate 4: Per-action cooldown.
	// Track last allowed time manually so the injectable clock works in tests.
	cooldown := time.Duration(t.cfg.CooldownSeconds) * time.Second
	if last, ok := t.lastSeen[actionID]; ok {
		if now.Sub(last) < cooldown {
			// Restore budget — cooldown blocked before the message was delivered.
			t.budget++
			return false
		}
	}
	t.lastSeen[actionID] = now

	// Gate 5: Graduation + per-state interval.
	if !t.grad.ShouldCoach(actionID) {
		// Restore budget — graduation blocked.
		t.budget++
		// Also restore lastSeen — we didn't actually allow.
		delete(t.lastSeen, actionID)
		return false
	}

	return true
}

// Persist saves the current budget counter to the DB.
// No-op implementation — budget resets hourly anyway.
func (t *Throttle) Persist(_ *sql.DB) error {
	return nil
}

// Load restores the budget counter from the DB.
// No-op implementation — budget resets hourly anyway.
func (t *Throttle) Load(_ *sql.DB) error {
	return nil
}

// quietBounds holds parsed hour and minute for a quiet-hours boundary.
type quietBounds struct {
	hour   int
	minute int
}

// parseQuietHours parses a "HH:MM-HH:MM" quiet hours specification.
// Returns the start and end bounds or an error if the format is invalid.
func parseQuietHours(spec string) (start, end quietBounds, err error) {
	var startHour, startMin, endHour, endMin int
	n, scanErr := fmt.Sscanf(spec, "%d:%d-%d:%d", &startHour, &startMin, &endHour, &endMin)
	if scanErr != nil || n != 4 {
		err = fmt.Errorf("invalid quiet hours format %q: expected HH:MM-HH:MM", spec)
		return
	}
	start = quietBounds{hour: startHour, minute: startMin}
	end = quietBounds{hour: endHour, minute: endMin}
	return
}

// isQuietTime returns true if now falls within the quiet window defined by start..end.
// Handles midnight crossing (e.g., 22:00-08:00).
func isQuietTime(now time.Time, start, end quietBounds) bool {
	// Convert everything to minutes-since-midnight for easy comparison.
	nowMins := now.Hour()*60 + now.Minute()
	startMins := start.hour*60 + start.minute
	endMins := end.hour*60 + end.minute

	if startMins <= endMins {
		// Same-day window: e.g., 09:00-17:00.
		return nowMins >= startMins && nowMins < endMins
	}
	// Midnight-crossing window: e.g., 22:00-08:00.
	// Quiet if >= start OR < end.
	return nowMins >= startMins || nowMins < endMins
}
