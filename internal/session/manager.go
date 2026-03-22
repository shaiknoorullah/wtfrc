package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// Manager provides session lifecycle operations backed by the knowledge base.
type Manager struct {
	db *kb.DB
}

// NewManager returns a Manager that operates on the given database.
func NewManager(db *kb.DB) *Manager {
	return &Manager{db: db}
}

// StartSession inserts a new session row and returns the populated Session.
func (m *Manager) StartSession(modelUsed string) (*kb.Session, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := m.db.Conn().Exec(
		`INSERT INTO sessions (id, started_at, model_used) VALUES (?, ?, ?)`,
		id, now.Format(time.RFC3339), modelUsed,
	)
	if err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}

	return &kb.Session{
		ID:        id,
		StartedAt: now,
		ModelUsed: modelUsed,
	}, nil
}

// LogQuery inserts a query row and increments the session's query_count.
func (m *Manager) LogQuery(sessionID string, q *kb.Query) error {
	entriesJSON, err := json.Marshal(q.EntriesUsed)
	if err != nil {
		return fmt.Errorf("marshal entries_used: %w", err)
	}

	issuesJSON, err := json.Marshal(q.Issues)
	if err != nil {
		return fmt.Errorf("marshal issues: %w", err)
	}

	tx, err := m.db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(
		`INSERT INTO queries (session_id, question, answer, entries_used, response_time_ms, timestamp, accuracy_score, issues)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, q.Question, q.Answer, string(entriesJSON),
		q.ResponseTimeMs, q.Timestamp.Format(time.RFC3339),
		q.AccuracyScore, string(issuesJSON),
	)
	if err != nil {
		return fmt.Errorf("insert query: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE sessions SET query_count = query_count + 1 WHERE id = ?`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("increment query_count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// EndSession sets the ended_at timestamp for the given session.
func (m *Manager) EndSession(sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := m.db.Conn().Exec(
		`UPDATE sessions SET ended_at = ? WHERE id = ?`,
		now, sessionID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("end session: no session with id %s", sessionID)
	}
	return nil
}

// GetSession retrieves a session and all of its queries.
func (m *Manager) GetSession(id string) (*kb.Session, error) {
	var s kb.Session
	var startedAtStr string
	var endedAtStr *string
	var queryCount int

	err := m.db.Conn().QueryRow(
		`SELECT id, started_at, ended_at, query_count, model_used FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &startedAtStr, &endedAtStr, &queryCount, &s.ModelUsed)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	s.StartedAt, _ = time.Parse(time.RFC3339, startedAtStr)
	if endedAtStr != nil {
		t, _ := time.Parse(time.RFC3339, *endedAtStr)
		s.EndedAt = &t
	}

	queries, err := m.sessionQueries(id)
	if err != nil {
		return nil, err
	}
	s.Queries = queries

	return &s, nil
}

// RecentSessions returns the N most recently started sessions (without their queries).
func (m *Manager) RecentSessions(limit int) ([]kb.Session, error) {
	rows, err := m.db.Conn().Query(
		`SELECT id, started_at, ended_at, query_count, model_used
		 FROM sessions ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recent sessions: %w", err)
	}
	defer rows.Close()

	var sessions []kb.Session
	for rows.Next() {
		var s kb.Session
		var startedAtStr string
		var endedAtStr *string
		var queryCount int

		if err := rows.Scan(&s.ID, &startedAtStr, &endedAtStr, &queryCount, &s.ModelUsed); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		s.StartedAt, _ = time.Parse(time.RFC3339, startedAtStr)
		if endedAtStr != nil {
			t, _ := time.Parse(time.RFC3339, *endedAtStr)
			s.EndedAt = &t
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// RecentQueries returns the N most recent queries for a session, ordered newest-first.
func (m *Manager) RecentQueries(sessionID string, limit int) ([]kb.Query, error) {
	rows, err := m.db.Conn().Query(
		`SELECT id, session_id, question, answer, entries_used, response_time_ms, timestamp, accuracy_score, issues
		 FROM queries WHERE session_id = ? ORDER BY timestamp DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recent queries: %w", err)
	}
	defer rows.Close()

	return scanQueries(rows)
}

// sessionQueries returns all queries for a session ordered by timestamp ascending.
func (m *Manager) sessionQueries(sessionID string) ([]kb.Query, error) {
	rows, err := m.db.Conn().Query(
		`SELECT id, session_id, question, answer, entries_used, response_time_ms, timestamp, accuracy_score, issues
		 FROM queries WHERE session_id = ? ORDER BY timestamp ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session queries: %w", err)
	}
	defer rows.Close()

	return scanQueries(rows)
}

// scanQueries reads query rows into a slice.
func scanQueries(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]kb.Query, error) {
	var queries []kb.Query
	for rows.Next() {
		var q kb.Query
		var entriesJSON *string
		var issuesJSON *string
		var tsStr string
		var accScore *float64

		if err := rows.Scan(
			&q.ID, &q.SessionID, &q.Question, &q.Answer,
			&entriesJSON, &q.ResponseTimeMs, &tsStr,
			&accScore, &issuesJSON,
		); err != nil {
			return nil, fmt.Errorf("scan query: %w", err)
		}

		q.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
		q.AccuracyScore = accScore

		if entriesJSON != nil && *entriesJSON != "" {
			_ = json.Unmarshal([]byte(*entriesJSON), &q.EntriesUsed)
		}
		if issuesJSON != nil && *issuesJSON != "" {
			_ = json.Unmarshal([]byte(*issuesJSON), &q.Issues)
		}

		queries = append(queries, q)
	}
	return queries, rows.Err()
}
