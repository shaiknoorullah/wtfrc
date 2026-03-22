package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// Archiver exports old sessions to JSONL files and marks them archived in the DB.
type Archiver struct {
	db *kb.DB
}

// NewArchiver returns an Archiver that operates on the given database.
func NewArchiver(db *kb.DB) *Archiver {
	return &Archiver{db: db}
}

// archivedSession is the JSON representation written to the JSONL file.
type archivedSession struct {
	ID         string         `json:"id"`
	StartedAt  string         `json:"started_at"`
	EndedAt    *string        `json:"ended_at,omitempty"`
	QueryCount int            `json:"query_count"`
	ModelUsed  string         `json:"model_used"`
	Queries    []archivedQuery `json:"queries"`
}

// archivedQuery is the JSON representation of a query inside an archived session.
type archivedQuery struct {
	ID             int64    `json:"id"`
	Question       string   `json:"question"`
	Answer         string   `json:"answer"`
	EntriesUsed    []int64  `json:"entries_used"`
	ResponseTimeMs int64    `json:"response_time_ms"`
	Timestamp      string   `json:"timestamp"`
	AccuracyScore  *float64 `json:"accuracy_score,omitempty"`
	Issues         []string `json:"issues,omitempty"`
}

// Archive finds non-archived sessions older than retainDays, writes them to a
// JSONL file named sessions-YYYY-MM.jsonl in archiveDir, and marks them
// archived in the database.
func (a *Archiver) Archive(archiveDir string, retainDays int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -retainDays).Format(time.RFC3339)

	rows, err := a.db.Conn().Query(
		`SELECT id, started_at, ended_at, query_count, model_used
		 FROM sessions
		 WHERE archived = 0 AND started_at < ?
		 ORDER BY started_at ASC`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("query archivable sessions: %w", err)
	}
	defer rows.Close()

	type sessionRow struct {
		id         string
		startedAt  string
		endedAt    *string
		queryCount int
		modelUsed  string
	}

	var toArchive []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.startedAt, &r.endedAt, &r.queryCount, &r.modelUsed); err != nil {
			return fmt.Errorf("scan session: %w", err)
		}
		toArchive = append(toArchive, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sessions: %w", err)
	}

	if len(toArchive) == 0 {
		return nil
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	// Group sessions by year-month of their started_at for file naming.
	fileWriters := map[string]*os.File{}
	defer func() {
		for _, f := range fileWriters {
			f.Close()
		}
	}()

	getWriter := func(startedAt string) (*os.File, error) {
		t, _ := time.Parse(time.RFC3339, startedAt)
		key := t.Format("2006-01")
		if f, ok := fileWriters[key]; ok {
			return f, nil
		}
		fname := filepath.Join(archiveDir, fmt.Sprintf("sessions-%s.jsonl", key))
		f, err := os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open archive file: %w", err)
		}
		fileWriters[key] = f
		return f, nil
	}

	tx, err := a.db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, sr := range toArchive {
		// Fetch queries for this session.
		qRows, err := a.db.Conn().Query(
			`SELECT id, question, answer, entries_used, response_time_ms, timestamp, accuracy_score, issues
			 FROM queries WHERE session_id = ? ORDER BY timestamp ASC`,
			sr.id,
		)
		if err != nil {
			return fmt.Errorf("query session queries: %w", err)
		}

		var aqs []archivedQuery
		for qRows.Next() {
			var aq archivedQuery
			var entriesJSON *string
			var issuesJSON *string
			if err := qRows.Scan(
				&aq.ID, &aq.Question, &aq.Answer,
				&entriesJSON, &aq.ResponseTimeMs, &aq.Timestamp,
				&aq.AccuracyScore, &issuesJSON,
			); err != nil {
				qRows.Close()
				return fmt.Errorf("scan query: %w", err)
			}
			if entriesJSON != nil && *entriesJSON != "" {
				_ = json.Unmarshal([]byte(*entriesJSON), &aq.EntriesUsed)
			}
			if issuesJSON != nil && *issuesJSON != "" {
				_ = json.Unmarshal([]byte(*issuesJSON), &aq.Issues)
			}
			aqs = append(aqs, aq)
		}
		qRows.Close()
		if err := qRows.Err(); err != nil {
			return fmt.Errorf("iterate queries: %w", err)
		}

		as := archivedSession{
			ID:         sr.id,
			StartedAt:  sr.startedAt,
			EndedAt:    sr.endedAt,
			QueryCount: sr.queryCount,
			ModelUsed:  sr.modelUsed,
			Queries:    aqs,
		}

		line, err := json.Marshal(as)
		if err != nil {
			return fmt.Errorf("marshal session: %w", err)
		}

		f, err := getWriter(sr.startedAt)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
			return fmt.Errorf("write archive line: %w", err)
		}

		if _, err := tx.Exec(`UPDATE sessions SET archived = 1 WHERE id = ?`, sr.id); err != nil {
			return fmt.Errorf("mark archived: %w", err)
		}
	}

	return tx.Commit()
}
