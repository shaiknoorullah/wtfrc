package coach

import (
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
)

// makeTestClock returns a pointer-to-time and a function that returns that time.
// Advancing *t advances the clock.
func makeTestClock(t time.Time) (*time.Time, func() time.Time) {
	current := t
	return &current, func() time.Time { return current }
}

// makeThrottleWithClock creates a Throttle with a pinned clock for testing.
func makeThrottleWithClock(cfg *config.CoachConfig, clockPtr *time.Time) *Throttle {
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })
	return NewThrottle(cfg, grad, func() time.Time { return *clockPtr })
}

// ----------------------------------------------------------------------------
// TestQuietHours
// ----------------------------------------------------------------------------

func TestQuietHours(t *testing.T) {
	type testCase struct {
		spec     string
		hour     int
		minute   int
		wantQuiet bool
		name     string
	}

	cases := []testCase{
		{"22:00-08:00", 23, 0, true, "midnight-crossing: 23:00 is quiet"},
		{"22:00-08:00", 7, 59, true, "midnight-crossing: 07:59 is quiet"},
		{"22:00-08:00", 8, 1, false, "midnight-crossing: 08:01 is not quiet"},
		{"22:00-08:00", 15, 0, false, "midnight-crossing: 15:00 is not quiet"},
		{"22:00-08:00", 22, 0, true, "midnight-crossing: 22:00 is quiet (inclusive start)"},
		{"09:00-17:00", 12, 0, true, "same-day: 12:00 is quiet"},
		{"09:00-17:00", 20, 0, false, "same-day: 20:00 is not quiet"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := parseQuietHours(tc.spec)
			if err != nil {
				t.Fatalf("parseQuietHours(%q) error: %v", tc.spec, err)
			}
			// Build a time with the desired hour/minute.
			now := time.Date(2024, 1, 15, tc.hour, tc.minute, 0, 0, time.UTC)
			got := isQuietTime(now, start, end)
			if got != tc.wantQuiet {
				t.Errorf("isQuietTime(%02d:%02d, %q) = %v, want %v",
					tc.hour, tc.minute, tc.spec, got, tc.wantQuiet)
			}
		})
	}
}

func TestParseQuietHoursInvalid(t *testing.T) {
	_, _, err := parseQuietHours("not-valid")
	if err == nil {
		t.Error("expected error for invalid spec, got nil")
	}
}

// ----------------------------------------------------------------------------
// TestFocusMode
// ----------------------------------------------------------------------------

func TestFocusMode(t *testing.T) {
	type testCase struct {
		focusCategory string
		source        string
		wantAllowed   bool
		name          string
	}

	cases := []testCase{
		{"shell", SourceShell, true, "shell focus + shell source: allowed"},
		{"shell", SourceHyprland, false, "shell focus + hyprland source: blocked"},
		{"editor", SourceNvim, true, "editor focus + nvim source: allowed"},
		{"wm", SourceKitty, true, "wm focus + kitty source: allowed"},
		{"", SourceShell, true, "no focus + any source: allowed"},
		{"", SourceHyprland, true, "no focus + hyprland: allowed"},
		{"terminal", SourceTmux, true, "terminal focus + tmux: allowed"},
		{"wm", SourceQutebrowser, true, "wm focus + qutebrowser: allowed"},
		{"wm", SourceYazi, true, "wm focus + yazi: allowed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use 12:00 on a weekday — outside any quiet hours.
			base := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
			clockPtr, _ := makeTestClock(base)

			cfg := &config.CoachConfig{
				BudgetPerHour:   100,
				CooldownSeconds: 0,
				QuietHours:      "", // no quiet hours
				FocusCategory:   tc.focusCategory,
			}
			thr := makeThrottleWithClock(cfg, clockPtr)

			got := thr.Allow("test:action", tc.source)
			if got != tc.wantAllowed {
				t.Errorf("Allow with FocusCategory=%q source=%q = %v, want %v",
					tc.focusCategory, tc.source, got, tc.wantAllowed)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TestBudget
// ----------------------------------------------------------------------------

func TestBudget(t *testing.T) {
	base := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clockPtr := &base

	cfg := &config.CoachConfig{
		BudgetPerHour:   2,
		CooldownSeconds: 0, // no cooldown for this test
		QuietHours:      "",
		FocusCategory:   "",
	}
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })
	thr := NewThrottle(cfg, grad, func() time.Time { return *clockPtr })

	// First two calls succeed (budget = 2).
	if !thr.Allow("action:a", SourceShell) {
		t.Error("first Allow should return true")
	}
	if !thr.Allow("action:b", SourceShell) {
		t.Error("second Allow should return true")
	}

	// Third call fails — budget exhausted.
	if thr.Allow("action:c", SourceShell) {
		t.Error("third Allow should return false (budget exhausted)")
	}

	// Advance clock by 1 hour to trigger budget reset.
	advanced := base.Add(time.Hour)
	*clockPtr = advanced

	// Budget should reset for the new hour.
	if !thr.Allow("action:a", SourceShell) {
		t.Error("Allow after hour advance should return true (budget reset)")
	}
}

// ----------------------------------------------------------------------------
// TestCooldown
// ----------------------------------------------------------------------------

func TestCooldown(t *testing.T) {
	base := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	clockPtr := &base

	cfg := &config.CoachConfig{
		BudgetPerHour:   100,
		CooldownSeconds: 60,
		QuietHours:      "",
		FocusCategory:   "",
	}
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })
	thr := NewThrottle(cfg, grad, func() time.Time { return *clockPtr })

	const action = "shell:myalias"

	// First call: allowed.
	if !thr.Allow(action, SourceShell) {
		t.Error("first Allow should return true")
	}

	// Immediate second call: blocked by cooldown.
	if thr.Allow(action, SourceShell) {
		t.Error("immediate second Allow should return false (cooldown)")
	}

	// Advance clock by 61 seconds.
	advanced := base.Add(61 * time.Second)
	*clockPtr = advanced

	// Third call: allowed (cooldown expired).
	if !thr.Allow(action, SourceShell) {
		t.Error("Allow after cooldown expiry should return true")
	}
}

// ----------------------------------------------------------------------------
// TestQuietHoursGate (integration: quiet hours blocks Allow)
// ----------------------------------------------------------------------------

func TestQuietHoursGate(t *testing.T) {
	// 23:00 is within the default 22:00-08:00 quiet window.
	quietTime := time.Date(2024, 1, 15, 23, 0, 0, 0, time.UTC)
	clockPtr := &quietTime

	cfg := &config.CoachConfig{
		BudgetPerHour:   100,
		CooldownSeconds: 0,
		QuietHours:      "22:00-08:00",
		FocusCategory:   "",
	}
	thr := makeThrottleWithClock(cfg, clockPtr)

	if thr.Allow("any:action", SourceShell) {
		t.Error("Allow during quiet hours should return false")
	}

	// Advance to 10:00 — outside quiet window.
	activeTime := time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC)
	*clockPtr = activeTime

	if !thr.Allow("any:action", SourceShell) {
		t.Error("Allow outside quiet hours should return true")
	}
}
