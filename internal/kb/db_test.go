package kb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

func TestOpenCreatesSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Check regular tables
	tables := []string{"entries", "intents", "manifest", "sessions", "queries", "usage_events", "supervisor_runs"}
	for _, table := range tables {
		var name string
		err := db.Conn().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}

	// Check FTS5 virtual tables
	fts := []string{"intents_fts", "entries_fts"}
	for _, table := range fts {
		var name string
		err := db.Conn().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("FTS5 table %q not found: %v", table, err)
		}
	}
}

func TestInsertAndGetEntry(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	binding := "$mod+Shift+q"
	action := "kill"
	entry := KBEntry{
		Tool:        "i3",
		Type:        parsers.EntryKeybind,
		RawBinding:  &binding,
		RawAction:   &action,
		Description: "Close the focused window",
		SourceFile:  "/home/user/.config/i3/config",
		SourceLine:  42,
		Category:    "window_management",
		FileHash:    "abc123",
		IndexedAt:   time.Now(),
	}

	intents := []string{"close window", "kill window", "how to close"}

	id, err := db.InsertEntry(entry, intents)
	if err != nil {
		t.Fatalf("InsertEntry failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := db.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if got.Tool != "i3" {
		t.Errorf("expected tool 'i3', got %q", got.Tool)
	}
	if got.Description != "Close the focused window" {
		t.Errorf("expected description 'Close the focused window', got %q", got.Description)
	}
	if len(got.Intents) != 3 {
		t.Errorf("expected 3 intents, got %d", len(got.Intents))
	}
}
