package kb

import (
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

type KBEntry struct {
	ID          int64
	Tool        string
	Type        parsers.EntryType
	RawBinding  *string
	RawAction   *string
	Description string
	Intents     []string
	SourceFile  string
	SourceLine  int
	Category    string
	SeeAlso     []string
	IndexedAt   time.Time
	FileHash    string
}

type Session struct {
	ID        string
	StartedAt time.Time
	EndedAt   *time.Time
	Queries   []Query
	ModelUsed string
}

type Query struct {
	ID             int64
	SessionID      string
	Question       string
	Answer         string
	EntriesUsed    []int64
	ResponseTimeMs int64
	Timestamp      time.Time
	AccuracyScore  *float64
	Issues         []string
}

type ManifestEntry struct {
	FilePath    string
	SHA256      string
	Mtime       time.Time
	LastIndexed time.Time
	EntryCount  int
}
