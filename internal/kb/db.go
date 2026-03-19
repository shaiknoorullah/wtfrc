package kb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tool        TEXT NOT NULL,
	type        TEXT NOT NULL,
	raw_binding TEXT,
	raw_action  TEXT,
	description TEXT NOT NULL,
	source_file TEXT NOT NULL,
	source_line INTEGER,
	category    TEXT,
	see_also    TEXT,
	indexed_at  TEXT NOT NULL,
	file_hash   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS intents (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
	phrase   TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS intents_fts USING fts5(
	phrase,
	content='intents',
	content_rowid='id'
);

CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
	description,
	content='entries',
	content_rowid='id'
);

-- Triggers for intents_fts sync
CREATE TRIGGER IF NOT EXISTS intents_ai AFTER INSERT ON intents BEGIN
	INSERT INTO intents_fts(rowid, phrase) VALUES (new.id, new.phrase);
END;
CREATE TRIGGER IF NOT EXISTS intents_ad AFTER DELETE ON intents BEGIN
	INSERT INTO intents_fts(intents_fts, rowid, phrase) VALUES ('delete', old.id, old.phrase);
END;
CREATE TRIGGER IF NOT EXISTS intents_au AFTER UPDATE ON intents BEGIN
	INSERT INTO intents_fts(intents_fts, rowid, phrase) VALUES ('delete', old.id, old.phrase);
	INSERT INTO intents_fts(rowid, phrase) VALUES (new.id, new.phrase);
END;

-- Triggers for entries_fts sync
CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
	INSERT INTO entries_fts(rowid, description) VALUES (new.id, new.description);
END;
CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, description) VALUES ('delete', old.id, old.description);
END;
CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, description) VALUES ('delete', old.id, old.description);
	INSERT INTO entries_fts(rowid, description) VALUES (new.id, new.description);
END;

CREATE TABLE IF NOT EXISTS manifest (
	file_path    TEXT PRIMARY KEY,
	sha256       TEXT NOT NULL,
	mtime        TEXT NOT NULL,
	last_indexed TEXT NOT NULL,
	entry_count  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
	id          TEXT PRIMARY KEY,
	started_at  TEXT NOT NULL,
	ended_at    TEXT,
	query_count INTEGER DEFAULT 0,
	model_used  TEXT,
	archived    INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS queries (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id       TEXT NOT NULL REFERENCES sessions(id),
	question         TEXT NOT NULL,
	answer           TEXT NOT NULL,
	entries_used     TEXT,
	response_time_ms INTEGER,
	timestamp        TEXT NOT NULL,
	accuracy_score   REAL,
	issues           TEXT
);

CREATE TABLE IF NOT EXISTS usage_events (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	tool           TEXT NOT NULL,
	action_type    TEXT NOT NULL DEFAULT '',
	action         TEXT NOT NULL,
	optimal_action TEXT,
	entry_id       INTEGER REFERENCES entries(id),
	timestamp      TEXT NOT NULL,
	was_optimal    INTEGER NOT NULL DEFAULT 0,
	coached        INTEGER DEFAULT 0,
	time_saved_ms  INTEGER
);

CREATE TABLE IF NOT EXISTS supervisor_runs (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	run_at                TEXT NOT NULL,
	sessions_reviewed     INTEGER,
	issues_found          INTEGER,
	optimizations_applied TEXT,
	model_used            TEXT
);

CREATE INDEX IF NOT EXISTS idx_entries_tool ON entries(tool);
CREATE INDEX IF NOT EXISTS idx_entries_type ON entries(type);
CREATE INDEX IF NOT EXISTS idx_intents_entry_id ON intents(entry_id);
CREATE INDEX IF NOT EXISTS idx_queries_session_id ON queries(session_id);
CREATE INDEX IF NOT EXISTS idx_usage_events_tool ON usage_events(tool);
CREATE INDEX IF NOT EXISTS idx_usage_events_timestamp ON usage_events(timestamp);
`

func (db *DB) InsertEntry(e KBEntry, intents []string) (int64, error) {
	seeAlsoJSON, err := json.Marshal(e.SeeAlso)
	if err != nil {
		return 0, fmt.Errorf("marshal see_also: %w", err)
	}

	res, err := db.conn.Exec(
		`INSERT INTO entries (tool, type, raw_binding, raw_action, description, source_file, source_line, category, see_also, indexed_at, file_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Tool, string(e.Type), e.RawBinding, e.RawAction, e.Description,
		e.SourceFile, e.SourceLine, e.Category, string(seeAlsoJSON),
		e.IndexedAt.Format(time.RFC3339), e.FileHash,
	)
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	for _, phrase := range intents {
		_, err := db.conn.Exec("INSERT INTO intents (entry_id, phrase) VALUES (?, ?)", id, phrase)
		if err != nil {
			return 0, fmt.Errorf("insert intent: %w", err)
		}
	}

	return id, nil
}

func (db *DB) GetEntry(id int64) (*KBEntry, error) {
	var e KBEntry
	var typ string
	var seeAlsoJSON string
	var indexedAtStr string

	err := db.conn.QueryRow(
		`SELECT id, tool, type, raw_binding, raw_action, description, source_file, source_line, category, see_also, indexed_at, file_hash
		 FROM entries WHERE id = ?`, id,
	).Scan(&e.ID, &e.Tool, &typ, &e.RawBinding, &e.RawAction, &e.Description,
		&e.SourceFile, &e.SourceLine, &e.Category, &seeAlsoJSON, &indexedAtStr, &e.FileHash)
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}

	e.Type = parsers.EntryType(typ)
	if seeAlsoJSON != "" {
		json.Unmarshal([]byte(seeAlsoJSON), &e.SeeAlso)
	}
	e.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)

	rows, err := db.conn.Query("SELECT phrase FROM intents WHERE entry_id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("get intents: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var phrase string
		if err := rows.Scan(&phrase); err != nil {
			return nil, fmt.Errorf("scan intent: %w", err)
		}
		e.Intents = append(e.Intents, phrase)
	}

	return &e, nil
}

func (db *DB) DeleteEntriesByFile(filePath string) error {
	_, err := db.conn.Exec("DELETE FROM entries WHERE source_file = ?", filePath)
	return err
}
