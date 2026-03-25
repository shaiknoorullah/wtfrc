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

func TestCoachingTablesExist(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	tables := []string{"coaching_state", "coaching_log", "coaching_messages"}
	for _, table := range tables {
		var name string
		err := db.Conn().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("coaching table %q not found: %v", table, err)
		}
	}
}

func TestGetEntriesByTypes(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	binding := "ctrl+a"
	action := "select-all"

	entries := []struct {
		entry    KBEntry
		intents  []string
	}{
		{KBEntry{Tool: "bash", Type: parsers.EntryAlias, RawBinding: &binding, RawAction: &action, Description: "alias one", SourceFile: "/a", SourceLine: 1, Category: "shell", FileHash: "h1", IndexedAt: now}, []string{"alias one"}},
		{KBEntry{Tool: "bash", Type: parsers.EntryFunction, RawBinding: &binding, RawAction: &action, Description: "func one", SourceFile: "/b", SourceLine: 2, Category: "shell", FileHash: "h2", IndexedAt: now}, []string{"func one"}},
		{KBEntry{Tool: "i3", Type: parsers.EntryKeybind, RawBinding: &binding, RawAction: &action, Description: "keybind one", SourceFile: "/c", SourceLine: 3, Category: "wm", FileHash: "h3", IndexedAt: now}, []string{"keybind one"}},
	}

	for i := range entries {
		if _, err := db.InsertEntry(&entries[i].entry, entries[i].intents); err != nil {
			t.Fatalf("InsertEntry failed: %v", err)
		}
	}

	results, err := db.GetEntriesByTypes([]string{"alias", "function"})
	if err != nil {
		t.Fatalf("GetEntriesByTypes failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Type != parsers.EntryAlias && r.Type != parsers.EntryFunction {
			t.Errorf("unexpected entry type %q", r.Type)
		}
		if len(r.Intents) == 0 {
			t.Errorf("expected intents for entry %d, got none", r.ID)
		}
	}
}

func TestGetEntriesByToolAndType(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	binding := "mod+h"
	action := "move left"

	entries := []struct {
		entry   KBEntry
		intents []string
	}{
		{KBEntry{Tool: "hyprland", Type: parsers.EntryKeybind, RawBinding: &binding, RawAction: &action, Description: "hypr keybind", SourceFile: "/hypr", SourceLine: 1, Category: "wm", FileHash: "hh1", IndexedAt: now}, []string{"move left"}},
		{KBEntry{Tool: "hyprland", Type: parsers.EntrySetting, RawBinding: &binding, RawAction: &action, Description: "hypr setting", SourceFile: "/hypr", SourceLine: 2, Category: "wm", FileHash: "hh2", IndexedAt: now}, []string{"setting"}},
		{KBEntry{Tool: "i3", Type: parsers.EntryKeybind, RawBinding: &binding, RawAction: &action, Description: "i3 keybind", SourceFile: "/i3", SourceLine: 3, Category: "wm", FileHash: "ih1", IndexedAt: now}, []string{"i3 move"}},
	}

	for i := range entries {
		if _, err := db.InsertEntry(&entries[i].entry, entries[i].intents); err != nil {
			t.Fatalf("InsertEntry failed: %v", err)
		}
	}

	results, err := db.GetEntriesByToolAndType("hyprland", "keybind")
	if err != nil {
		t.Fatalf("GetEntriesByToolAndType failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results) > 0 {
		if results[0].Tool != "hyprland" {
			t.Errorf("expected tool 'hyprland', got %q", results[0].Tool)
		}
		if results[0].Type != parsers.EntryKeybind {
			t.Errorf("expected type 'keybind', got %q", results[0].Type)
		}
		if results[0].Description != "hypr keybind" {
			t.Errorf("expected description 'hypr keybind', got %q", results[0].Description)
		}
		if len(results[0].Intents) == 0 {
			t.Error("expected intents, got none")
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

	id, err := db.InsertEntry(&entry, intents)
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
