package coach

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	_ "modernc.org/sqlite"
)

// openTestDB opens an in-memory (or temp-file) SQLite DB and applies the schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})

	// Apply the coaching_state table schema.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS coaching_state (
		    action_id TEXT PRIMARY KEY,
		    state TEXT NOT NULL DEFAULT 'novice',
		    consecutive_optimal INTEGER NOT NULL DEFAULT 0,
		    total_coached INTEGER NOT NULL DEFAULT 0,
		    total_adopted INTEGER NOT NULL DEFAULT 0,
		    first_coached_at TEXT,
		    last_coached_at TEXT,
		    last_adopted_at TEXT,
		    next_coach_after TEXT,
		    graduated_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

func defaultCoachCfg() *config.CoachConfig {
	return &config.CoachConfig{
		BudgetPerHour:    5,
		CooldownSeconds:  120,
		GraduationStreak: 7,
		QuietHours:       "22:00-08:00",
	}
}

// pinClock returns a time pointer and a GraduationManager with that clock.
func newGradWithClock(db *sql.DB, clockPtr *time.Time) *GraduationManager {
	cfg := defaultCoachCfg()
	return NewGraduationManager(db, cfg, func() time.Time { return *clockPtr })
}

// ----------------------------------------------------------------------------
// TestGraduationTransitions: full state machine walk-through
// ----------------------------------------------------------------------------

func TestGraduationTransitions(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })

	const action = "shell:gs"

	// Step 1: New action starts as novice.
	if got := grad.GetState(action); got != StateNovice {
		t.Errorf("step 1: expected novice, got %q", got)
	}

	// Step 2: RecordOptimal → transitions to learning.
	grad.RecordOptimal(action)
	if got := grad.GetState(action); got != StateLearning {
		t.Errorf("step 2: expected learning after first optimal, got %q", got)
	}

	// Step 3: RecordSuboptimal → back to novice (streak broken in learning).
	grad.RecordSuboptimal(action)
	if got := grad.GetState(action); got != StateNovice {
		t.Errorf("step 3: expected novice after suboptimal in learning, got %q", got)
	}

	// Step 4: RecordOptimal → learning again.
	grad.RecordOptimal(action)
	if got := grad.GetState(action); got != StateLearning {
		t.Errorf("step 4: expected learning after optimal from novice, got %q", got)
	}

	// Step 5: 3x RecordOptimal → transitions to improving.
	grad.RecordOptimal(action) // 2nd consecutive
	grad.RecordOptimal(action) // 3rd consecutive
	if got := grad.GetState(action); got != StateImproving {
		t.Errorf("step 5: expected improving after 3 consecutive optimal, got %q", got)
	}

	// Step 6: RecordSuboptimal → back to learning (partial retention).
	grad.RecordSuboptimal(action)
	if got := grad.GetState(action); got != StateLearning {
		t.Errorf("step 6: expected learning after suboptimal in improving, got %q", got)
	}

	// Step 7: RecordOptimal x3 → improving again.
	grad.RecordOptimal(action) // 1 → learning→improving at 3
	grad.RecordOptimal(action)
	grad.RecordOptimal(action)
	if got := grad.GetState(action); got != StateImproving {
		t.Errorf("step 7: expected improving after 3 optimal from learning, got %q", got)
	}

	// Step 8: RecordOptimal x7 with 3+ day span → graduated.
	// Advance clock by 4 days to satisfy the 3-day constraint.
	advanced := base.Add(4 * 24 * time.Hour)
	*clockPtr = advanced

	grad.RecordOptimal(action) // 4
	grad.RecordOptimal(action) // 5
	grad.RecordOptimal(action) // 6
	grad.RecordOptimal(action) // 7
	if got := grad.GetState(action); got != StateGraduated {
		t.Errorf("step 8: expected graduated after 7 consecutive optimal with 4d span, got %q", got)
	}
	if !grad.IsGraduated(action) {
		t.Error("step 8: IsGraduated should return true")
	}

	// Step 9: RecordSuboptimal → improving (relapse from graduated).
	grad.RecordSuboptimal(action)
	if got := grad.GetState(action); got != StateImproving {
		t.Errorf("step 9: expected improving after relapse from graduated, got %q", got)
	}
	if grad.IsGraduated(action) {
		t.Error("step 9: IsGraduated should return false after relapse")
	}
}

// ----------------------------------------------------------------------------
// TestGraduationNotGraduatedWithoutTimespan
// ----------------------------------------------------------------------------

func TestGraduationNotGraduatedWithoutTimespan(t *testing.T) {
	// All 7 optionals happen at the same time — should NOT graduate (< 3 days).
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })

	const action = "shell:cd"

	// Get to improving state (need 1 optimal for learning, then 3 more for improving).
	grad.RecordOptimal(action) // novice→learning (consecutive=1)
	grad.RecordOptimal(action) // 2
	grad.RecordOptimal(action) // 3 → improving

	// Now 7 more optionals without time advance.
	for i := 0; i < 7; i++ {
		grad.RecordOptimal(action)
	}

	if got := grad.GetState(action); got == StateGraduated {
		t.Error("should NOT graduate without 3 days since first_coached_at")
	}
}

// ----------------------------------------------------------------------------
// TestGraduationPersistence
// ----------------------------------------------------------------------------

func TestGraduationPersistence(t *testing.T) {
	db := openTestDB(t)

	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base

	grad1 := newGradWithClock(db, clockPtr)

	const action = "shell:gp"

	// Drive to learning state.
	grad1.RecordOptimal(action)
	if got := grad1.GetState(action); got != StateLearning {
		t.Fatalf("expected learning, got %q", got)
	}

	// Record coaching so next_coach_after is set.
	grad1.RecordCoached(action)

	// Create a new GraduationManager pointing to the same DB — simulating restart.
	grad2 := newGradWithClock(db, clockPtr)

	if got := grad2.GetState(action); got != StateLearning {
		t.Errorf("persistence: expected learning after reload, got %q", got)
	}
	s := grad2.getOrLoad(action)
	if s.TotalAdopted != 1 {
		t.Errorf("persistence: expected TotalAdopted=1, got %d", s.TotalAdopted)
	}
	if s.TotalCoached != 1 {
		t.Errorf("persistence: expected TotalCoached=1, got %d", s.TotalCoached)
	}
}

// ----------------------------------------------------------------------------
// TestListGraduated
// ----------------------------------------------------------------------------

func TestListGraduated(t *testing.T) {
	db := openTestDB(t)

	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	// Use 5 days later so the 3-day constraint is satisfied.
	later := base.Add(5 * 24 * time.Hour)
	clockPtr := &later
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(db, cfg, func() time.Time { return *clockPtr })

	// Manually insert a graduated state into the DB.
	_, err := db.Exec(`
		INSERT INTO coaching_state (action_id, state, consecutive_optimal, total_coached, total_adopted)
		VALUES ('shell:ls', 'graduated', 7, 20, 18)
	`)
	if err != nil {
		t.Fatalf("insert graduated state: %v", err)
	}

	ids, err := grad.ListGraduated()
	if err != nil {
		t.Fatalf("ListGraduated error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "shell:ls" {
		t.Errorf("ListGraduated: expected [shell:ls], got %v", ids)
	}
}

// ----------------------------------------------------------------------------
// TestShouldCoachSpacing
// ----------------------------------------------------------------------------

func TestShouldCoachSpacing(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })

	const action = "shell:gco"

	// Drive to learning state.
	grad.RecordOptimal(action) // novice → learning

	// Call RecordCoached — this sets NextCoachAfter = now + 1 day (first coaching in learning).
	grad.RecordCoached(action)

	// ShouldCoach should return false right after being coached (within the interval).
	if grad.ShouldCoach(action) {
		t.Error("ShouldCoach should return false immediately after coaching in learning state")
	}

	// Advance clock by 25 hours — past the 24h delay.
	future := base.Add(25 * time.Hour)
	*clockPtr = future

	if !grad.ShouldCoach(action) {
		t.Error("ShouldCoach should return true after NextCoachAfter has passed")
	}
}

// ----------------------------------------------------------------------------
// TestShouldCoachGraduated
// ----------------------------------------------------------------------------

func TestShouldCoachGraduated(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })

	const action = "shell:gst"

	// Manually set graduated state by driving the state machine.
	// novice → learning (1 optimal)
	grad.RecordOptimal(action)
	// Set first_coached_at via RecordCoached
	grad.RecordCoached(action)

	// learning → improving (3 consecutive optimal)
	// Reset clock so next_coach_after is in the past
	past := base.Add(-48 * time.Hour)
	*clockPtr = past
	grad.RecordOptimal(action)
	grad.RecordOptimal(action)
	grad.RecordOptimal(action) // 3 total → improving

	// improving → graduated (7 consecutive optimal, 3+ days)
	// Use an absolute date that is 5 days after firstCoachedAt (2024-01-01).
	// We cannot use base.Add() here because *clockPtr = past mutated base above.
	later := time.Date(2024, 1, 6, 12, 0, 0, 0, time.UTC) // Jan 6 = 5 days after Jan 1
	*clockPtr = later

	for i := 0; i < 7; i++ {
		grad.RecordOptimal(action)
	}

	if !grad.IsGraduated(action) {
		t.Fatalf("expected graduated state, got %q", grad.GetState(action))
	}

	if grad.ShouldCoach(action) {
		t.Error("ShouldCoach should return false for graduated action")
	}
}

// ----------------------------------------------------------------------------
// TestNoviceCoachesEveryTime
// ----------------------------------------------------------------------------

func TestNoviceCoachesEveryTime(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clockPtr := &base
	cfg := defaultCoachCfg()
	grad := NewGraduationManager(nil, cfg, func() time.Time { return *clockPtr })

	const action = "shell:mkdirp"

	// In novice state, ShouldCoach should always return true.
	for i := 0; i < 5; i++ {
		if !grad.ShouldCoach(action) {
			t.Errorf("iteration %d: ShouldCoach in novice should return true", i)
		}
	}
}

