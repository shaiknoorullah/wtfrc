package kb

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

type SearchResult struct {
	KBEntry
	Score float64
}

// ftsEscape wraps each word in double quotes for FTS5 literal matching.
func ftsEscape(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}
	var escaped []string
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		escaped = append(escaped, `"`+w+`"`)
	}
	return strings.Join(escaped, " ")
}

func (db *DB) Search(query string, limit int) ([]SearchResult, error) {
	escaped := ftsEscape(query)
	if escaped == "" {
		return nil, nil
	}

	sql := `
	WITH matches AS (
		SELECT i.entry_id AS eid, (rank * -1.0) AS score
		FROM intents_fts
		JOIN intents i ON i.id = intents_fts.rowid
		WHERE intents_fts MATCH ?
		UNION ALL
		SELECT e.id AS eid, (rank * -0.5) AS score
		FROM entries_fts
		JOIN entries e ON e.id = entries_fts.rowid
		WHERE entries_fts MATCH ?
	),
	deduped AS (
		SELECT eid, MAX(score) AS score
		FROM matches
		GROUP BY eid
	)
	SELECT e.id, e.tool, e.type, e.raw_binding, e.raw_action, e.description,
	       e.source_file, e.source_line, e.category, e.see_also, e.indexed_at, e.file_hash,
	       d.score
	FROM deduped d
	JOIN entries e ON e.id = d.eid
	ORDER BY d.score DESC
	LIMIT ?`

	rows, err := db.conn.Query(sql, escaped, escaped, limit)
	if err != nil {
		// FTS5 MATCH can fail on invalid queries — return empty results
		if strings.Contains(err.Error(), "fts5") {
			return nil, nil
		}
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var typ string
		var seeAlsoJSON string
		var indexedAtStr string

		err := rows.Scan(&r.ID, &r.Tool, &typ, &r.RawBinding, &r.RawAction,
			&r.Description, &r.SourceFile, &r.SourceLine, &r.Category,
			&seeAlsoJSON, &indexedAtStr, &r.FileHash, &r.Score)
		if err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}

		r.Type = parsers.EntryType(typ)
		if seeAlsoJSON != "" {
			json.Unmarshal([]byte(seeAlsoJSON), &r.SeeAlso)
		}
		r.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)

		// Load intents for this entry
		intentRows, err := db.conn.Query("SELECT phrase FROM intents WHERE entry_id = ?", r.ID)
		if err != nil {
			return nil, fmt.Errorf("get intents for search result: %w", err)
		}
		for intentRows.Next() {
			var phrase string
			if err := intentRows.Scan(&phrase); err != nil {
				intentRows.Close()
				return nil, fmt.Errorf("scan intent: %w", err)
			}
			r.Intents = append(r.Intents, phrase)
		}
		intentRows.Close()

		results = append(results, r)
	}

	return results, nil
}
