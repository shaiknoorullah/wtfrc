package coach

import (
	"database/sql"
	"sync"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/config"
)

// States for the graduation state machine.
const (
	StateNovice    = "novice"
	StateLearning  = "learning"
	StateImproving = "improving"
	StateGraduated = "graduated"
)

// actionState holds in-memory graduation state for a single action.
type actionState struct {
	State              string
	ConsecutiveOptimal int
	TotalCoached       int
	TotalAdopted       int
	FirstCoachedAt     *time.Time
	LastCoachedAt      *time.Time
	LastAdoptedAt      *time.Time
	NextCoachAfter     *time.Time
	GraduatedAt        *time.Time
}

// GraduationManager manages per-action graduation state machines backed by SQLite.
type GraduationManager struct {
	db    *sql.DB
	cache map[string]*actionState
	mu    sync.Mutex
	now   func() time.Time
	cfg   *config.CoachConfig
}

// NewGraduationManager creates a new GraduationManager.
// If now is nil, time.Now is used.
func NewGraduationManager(db *sql.DB, cfg *config.CoachConfig, now func() time.Time) *GraduationManager {
	if now == nil {
		now = time.Now
	}
	return &GraduationManager{
		db:    db,
		cache: make(map[string]*actionState),
		now:   now,
		cfg:   cfg,
	}
}

// ShouldCoach returns true if coaching should be shown for the given action.
// It checks graduation state and spaced repetition intervals.
func (g *GraduationManager) ShouldCoach(actionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	s := g.getOrLoad(actionID)

	switch s.State {
	case StateGraduated:
		return false
	case StateImproving:
		if s.NextCoachAfter != nil && g.now().Before(*s.NextCoachAfter) {
			return false
		}
	case StateLearning:
		if s.NextCoachAfter != nil && g.now().Before(*s.NextCoachAfter) {
			return false
		}
	}
	return true
}

// RecordCoached increments coaching counters and sets spaced-repetition intervals.
func (g *GraduationManager) RecordCoached(actionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	s := g.getOrLoad(actionID)
	now := g.now()

	s.TotalCoached++
	s.LastCoachedAt = &now

	if s.FirstCoachedAt == nil {
		s.FirstCoachedAt = &now
	}

	// Set NextCoachAfter based on state and how many times we've coached.
	switch s.State {
	case StateNovice:
		// Coach every time — no delay.
	case StateLearning:
		// Spaced intervals: 1d after 1st coaching in this state, 3d after 2nd, 7d after 3rd+.
		var delay time.Duration
		switch {
		case s.TotalCoached <= 1:
			delay = 24 * time.Hour
		case s.TotalCoached == 2:
			delay = 3 * 24 * time.Hour
		default:
			delay = 7 * 24 * time.Hour
		}
		next := now.Add(delay)
		s.NextCoachAfter = &next
	case StateImproving:
		next := now.Add(7 * 24 * time.Hour)
		s.NextCoachAfter = &next
	case StateGraduated:
		// Graduated — should not be coached, but if somehow called, no-op.
	}

	g.saveState(actionID, s)
}

// RecordOptimal records that the user performed the optimal action.
// It handles state transitions: novice→learning, learning→improving, improving→graduated.
func (g *GraduationManager) RecordOptimal(actionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	s := g.getOrLoad(actionID)
	now := g.now()

	s.ConsecutiveOptimal++
	s.TotalAdopted++
	s.LastAdoptedAt = &now

	// Track the first time we see any activity for this action,
	// so the graduation time-span check works even if RecordCoached was not called.
	if s.FirstCoachedAt == nil {
		s.FirstCoachedAt = &now
	}

	// State transition logic.
	switch s.State {
	case StateNovice:
		// First adoption transitions to learning.
		s.State = StateLearning
		s.ConsecutiveOptimal = 1

	case StateLearning:
		// 3 consecutive optimal → improving.
		if s.ConsecutiveOptimal >= 3 {
			s.State = StateImproving
		}

	case StateImproving:
		// 7 consecutive optimal AND at least 3 days since first_coached_at → graduated.
		if s.ConsecutiveOptimal >= 7 && s.FirstCoachedAt != nil {
			durationSinceFirst := now.Sub(*s.FirstCoachedAt)
			if durationSinceFirst >= 3*24*time.Hour {
				s.State = StateGraduated
				s.GraduatedAt = &now
			}
		}

	case StateGraduated:
		// Stays graduated.
	}

	g.saveState(actionID, s)
}

// RecordSuboptimal records that the user performed a suboptimal action.
// It handles regression transitions.
func (g *GraduationManager) RecordSuboptimal(actionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	s := g.getOrLoad(actionID)
	s.ConsecutiveOptimal = 0

	// State regression logic.
	switch s.State {
	case StateLearning:
		// Streak broken — back to novice.
		s.State = StateNovice

	case StateImproving:
		// Streak broken but partial retention — back to learning.
		s.State = StateLearning

	case StateGraduated:
		// Relapse — back to improving.
		s.State = StateImproving
		s.GraduatedAt = nil

	case StateNovice:
		// Stays novice.
	}

	g.saveState(actionID, s)
}

// GetState returns the current graduation state for an action.
func (g *GraduationManager) GetState(actionID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.getOrLoad(actionID)
	return s.State
}

// IsGraduated returns true if the action has reached the graduated state.
func (g *GraduationManager) IsGraduated(actionID string) bool {
	return g.GetState(actionID) == StateGraduated
}

// ListGraduated queries the DB for all graduated action IDs.
func (g *GraduationManager) ListGraduated() ([]string, error) {
	if g.db == nil {
		return nil, nil
	}
	rows, err := g.db.Query(`SELECT action_id FROM coaching_state WHERE state = ?`, StateGraduated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// getOrLoad retrieves the state from cache, loading from DB if needed.
// Must be called with mu held.
func (g *GraduationManager) getOrLoad(actionID string) *actionState {
	if s, ok := g.cache[actionID]; ok {
		return s
	}
	s := g.loadState(actionID)
	g.cache[actionID] = s
	return s
}

// loadState loads the graduation state for an action from the DB.
// Returns a default novice state if not found or DB is nil.
func (g *GraduationManager) loadState(actionID string) *actionState {
	s := &actionState{State: StateNovice}

	if g.db == nil {
		return s
	}

	var (
		state              string
		consecutiveOptimal int
		totalCoached       int
		totalAdopted       int
		firstCoachedAt     sql.NullString
		lastCoachedAt      sql.NullString
		lastAdoptedAt      sql.NullString
		nextCoachAfter     sql.NullString
		graduatedAt        sql.NullString
	)

	err := g.db.QueryRow(`
		SELECT state, consecutive_optimal, total_coached, total_adopted,
		       first_coached_at, last_coached_at, last_adopted_at,
		       next_coach_after, graduated_at
		FROM coaching_state WHERE action_id = ?`, actionID,
	).Scan(
		&state, &consecutiveOptimal, &totalCoached, &totalAdopted,
		&firstCoachedAt, &lastCoachedAt, &lastAdoptedAt,
		&nextCoachAfter, &graduatedAt,
	)
	if err == sql.ErrNoRows {
		return s
	}
	if err != nil {
		// Return default on error.
		return s
	}

	s.State = state
	s.ConsecutiveOptimal = consecutiveOptimal
	s.TotalCoached = totalCoached
	s.TotalAdopted = totalAdopted

	if firstCoachedAt.Valid {
		t, err := time.Parse(time.RFC3339, firstCoachedAt.String)
		if err == nil {
			s.FirstCoachedAt = &t
		}
	}
	if lastCoachedAt.Valid {
		t, err := time.Parse(time.RFC3339, lastCoachedAt.String)
		if err == nil {
			s.LastCoachedAt = &t
		}
	}
	if lastAdoptedAt.Valid {
		t, err := time.Parse(time.RFC3339, lastAdoptedAt.String)
		if err == nil {
			s.LastAdoptedAt = &t
		}
	}
	if nextCoachAfter.Valid {
		t, err := time.Parse(time.RFC3339, nextCoachAfter.String)
		if err == nil {
			s.NextCoachAfter = &t
		}
	}
	if graduatedAt.Valid {
		t, err := time.Parse(time.RFC3339, graduatedAt.String)
		if err == nil {
			s.GraduatedAt = &t
		}
	}

	return s
}

// saveState persists the graduation state for an action to the DB.
// Must be called with mu held.
func (g *GraduationManager) saveState(actionID string, s *actionState) {
	if g.db == nil {
		return
	}

	nullableTime := func(t *time.Time) sql.NullString {
		if t == nil {
			return sql.NullString{}
		}
		return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
	}

	_, _ = g.db.Exec(`
		INSERT OR REPLACE INTO coaching_state
		    (action_id, state, consecutive_optimal, total_coached, total_adopted,
		     first_coached_at, last_coached_at, last_adopted_at,
		     next_coach_after, graduated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		actionID,
		s.State,
		s.ConsecutiveOptimal,
		s.TotalCoached,
		s.TotalAdopted,
		nullableTime(s.FirstCoachedAt),
		nullableTime(s.LastCoachedAt),
		nullableTime(s.LastAdoptedAt),
		nullableTime(s.NextCoachAfter),
		nullableTime(s.GraduatedAt),
	)
}
