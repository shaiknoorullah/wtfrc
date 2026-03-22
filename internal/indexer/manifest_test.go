package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

func openTestDB(t *testing.T) *kb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := kb.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestManifestComputeHashAndIsChanged(t *testing.T) {
	db := openTestDB(t)
	m := NewManifest(db)

	// Create a temp file with known content.
	tmpFile := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(tmpFile, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// Compute hash.
	hash, err := m.ComputeHash(tmpFile)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// File is not tracked yet — IsChanged should return true.
	changed, err := m.IsChanged(tmpFile)
	if err != nil {
		t.Fatalf("IsChanged: %v", err)
	}
	if !changed {
		t.Error("expected IsChanged=true for untracked file")
	}

	// Save to manifest.
	now := time.Now()
	err = m.Update(&kb.ManifestEntry{
		FilePath:    tmpFile,
		SHA256:      hash,
		Mtime:       now,
		LastIndexed: now,
		EntryCount:  1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Now IsChanged should return false.
	changed, err = m.IsChanged(tmpFile)
	if err != nil {
		t.Fatalf("IsChanged after update: %v", err)
	}
	if changed {
		t.Error("expected IsChanged=false after saving same hash")
	}

	// Modify the file.
	if err := os.WriteFile(tmpFile, []byte("modified content\n"), 0o644); err != nil {
		t.Fatalf("modify temp file: %v", err)
	}

	// Now IsChanged should return true.
	changed, err = m.IsChanged(tmpFile)
	if err != nil {
		t.Fatalf("IsChanged after modify: %v", err)
	}
	if !changed {
		t.Error("expected IsChanged=true after file modification")
	}
}

func TestManifestListTracked(t *testing.T) {
	db := openTestDB(t)
	m := NewManifest(db)

	now := time.Now().Truncate(time.Second)

	entries := []kb.ManifestEntry{
		{FilePath: "/tmp/a", SHA256: "aaa", Mtime: now, LastIndexed: now, EntryCount: 2},
		{FilePath: "/tmp/b", SHA256: "bbb", Mtime: now, LastIndexed: now, EntryCount: 5},
	}
	for i := range entries {
		if err := m.Update(&entries[i]); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	tracked, err := m.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked: %v", err)
	}
	if len(tracked) != 2 {
		t.Fatalf("expected 2 tracked entries, got %d", len(tracked))
	}

	// Build a map for easier lookup.
	byPath := map[string]kb.ManifestEntry{}
	for _, e := range tracked {
		byPath[e.FilePath] = e
	}

	if e, ok := byPath["/tmp/a"]; !ok {
		t.Error("missing /tmp/a")
	} else if e.EntryCount != 2 {
		t.Errorf("entry count for /tmp/a = %d, want 2", e.EntryCount)
	}
	if e, ok := byPath["/tmp/b"]; !ok {
		t.Error("missing /tmp/b")
	} else if e.SHA256 != "bbb" {
		t.Errorf("sha256 for /tmp/b = %q, want %q", e.SHA256, "bbb")
	}
}

func TestManifestRemove(t *testing.T) {
	db := openTestDB(t)
	m := NewManifest(db)

	now := time.Now()
	err := m.Update(&kb.ManifestEntry{
		FilePath:    "/tmp/remove-me",
		SHA256:      "xyz",
		Mtime:       now,
		LastIndexed: now,
		EntryCount:  1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := m.Remove("/tmp/remove-me"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	tracked, err := m.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked: %v", err)
	}
	for _, e := range tracked {
		if e.FilePath == "/tmp/remove-me" {
			t.Error("entry should have been removed")
		}
	}
}
