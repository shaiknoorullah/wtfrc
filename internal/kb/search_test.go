package kb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

func seedTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	binding1 := "$mod+Shift+q"
	action1 := "kill"
	_, err = db.InsertEntry(&KBEntry{
		Tool:        "i3",
		Type:        parsers.EntryKeybind,
		RawBinding:  &binding1,
		RawAction:   &action1,
		Description: "Close the currently focused window",
		SourceFile:  "/home/user/.config/i3/config",
		SourceLine:  42,
		Category:    "window_management",
		FileHash:    "hash1",
		IndexedAt:   time.Now(),
	}, []string{"close window", "kill window", "quit window"})
	if err != nil {
		t.Fatalf("InsertEntry 1 failed: %v", err)
	}

	binding2 := "prefix + ["
	action2 := "copy-mode"
	_, err = db.InsertEntry(&KBEntry{
		Tool:        "tmux",
		Type:        parsers.EntryKeybind,
		RawBinding:  &binding2,
		RawAction:   &action2,
		Description: "Enter copy mode for scrolling and text selection",
		SourceFile:  "/home/user/.tmux.conf",
		SourceLine:  10,
		Category:    "navigation",
		FileHash:    "hash2",
		IndexedAt:   time.Now(),
	}, []string{"scroll in tmux", "copy mode", "select text tmux"})
	if err != nil {
		t.Fatalf("InsertEntry 2 failed: %v", err)
	}

	return db
}

func TestSearchByIntent(t *testing.T) {
	db := seedTestDB(t)
	defer db.Close()

	results, err := db.Search("close window", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Tool != "i3" {
		t.Errorf("expected first result tool 'i3', got %q", results[0].Tool)
	}
}

func TestSearchByDescription(t *testing.T) {
	db := seedTestDB(t)
	defer db.Close()

	results, err := db.Search("scroll", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Tool != "tmux" {
		t.Errorf("expected first result tool 'tmux', got %q", results[0].Tool)
	}
}

func TestSearchNoResults(t *testing.T) {
	db := seedTestDB(t)
	defer db.Close()

	results, err := db.Search("kubernetes deploy", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected zero results, got %d", len(results))
	}
}
