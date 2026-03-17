package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
)

// mockEnricher returns canned enrichment data for any batch of entries.
type mockEnricher struct{}

func (m *mockEnricher) Enrich(_ context.Context, entries []parsers.RawEntry) ([]EnrichedEntry, error) {
	var out []EnrichedEntry
	for _, e := range entries {
		out = append(out, EnrichedEntry{
			Description: e.RawBinding + " runs " + e.RawAction,
			Intents:     []string{"use " + e.RawBinding, e.RawAction},
			Category:    "general",
			SeeAlso:     []string{},
		})
	}
	return out, nil
}

func TestIndexerFullPipeline(t *testing.T) {
	db := openTestDB(t)
	enricher := &mockEnricher{}
	redactor := NewRedactor(nil)
	idx := New(db, enricher, redactor)

	// Create a temp i3 config.
	tmpDir := t.TempDir()
	i3Dir := filepath.Join(tmpDir, "i3")
	if err := os.MkdirAll(i3Dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	i3Config := filepath.Join(i3Dir, "config")
	i3Content := `# i3 config
bindsym $mod+Return exec kitty
bindsym $mod+Shift+q kill
`
	if err := os.WriteFile(i3Config, []byte(i3Content), 0o644); err != nil {
		t.Fatalf("write i3 config: %v", err)
	}

	// Create a temp .zshrc.
	zshrc := filepath.Join(tmpDir, ".zshrc")
	zshrcContent := `alias ll='ls -la'
export EDITOR=nvim
`
	if err := os.WriteFile(zshrc, []byte(zshrcContent), 0o644); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}

	ctx := context.Background()
	if err := idx.Index(ctx, []string{i3Config, zshrc}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Verify search finds entries.
	results, err := db.Search("kitty", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'kitty', got none")
	}

	// Verify manifest reports both files.
	manifest := NewManifest(db)
	tracked, err := manifest.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked: %v", err)
	}
	if len(tracked) != 2 {
		t.Fatalf("expected 2 tracked files, got %d", len(tracked))
	}

	trackedPaths := map[string]bool{}
	for _, e := range tracked {
		trackedPaths[e.FilePath] = true
	}
	if !trackedPaths[i3Config] {
		t.Errorf("expected %s to be tracked", i3Config)
	}
	if !trackedPaths[zshrc] {
		t.Errorf("expected %s to be tracked", zshrc)
	}
}

func TestIndexerIncremental(t *testing.T) {
	db := openTestDB(t)
	enricher := &mockEnricher{}
	redactor := NewRedactor(nil)
	idx := New(db, enricher, redactor)

	// Create a temp i3 config with one binding.
	tmpDir := t.TempDir()
	i3Dir := filepath.Join(tmpDir, "i3")
	if err := os.MkdirAll(i3Dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	configPath := filepath.Join(i3Dir, "config")
	if err := os.WriteFile(configPath, []byte("bindsym $mod+Return exec kitty\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	if err := idx.Index(ctx, []string{configPath}); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	// Verify one entry.
	results, err := db.Search("kitty", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after first index, got %d", len(results))
	}

	// Re-index without changes — should be a no-op (same hash).
	if err := idx.Index(ctx, []string{configPath}); err != nil {
		t.Fatalf("second Index (no-op): %v", err)
	}

	// Modify the file: add a second binding.
	newContent := `bindsym $mod+Return exec kitty
bindsym $mod+d exec rofi
`
	if err := os.WriteFile(configPath, []byte(newContent), 0o644); err != nil {
		t.Fatalf("modify config: %v", err)
	}

	// Re-index — should detect change and update.
	if err := idx.Index(ctx, []string{configPath}); err != nil {
		t.Fatalf("third Index (changed): %v", err)
	}

	// Verify new entry appears.
	results, err = db.Search("rofi", 10)
	if err != nil {
		t.Fatalf("Search rofi: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'rofi' after re-index, got none")
	}

	// Verify manifest entry count updated.
	manifest := NewManifest(db)
	tracked, err := manifest.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked: %v", err)
	}
	if len(tracked) != 1 {
		t.Fatalf("expected 1 tracked file, got %d", len(tracked))
	}
	if tracked[0].EntryCount != 2 {
		t.Errorf("expected entry_count=2, got %d", tracked[0].EntryCount)
	}
}

func TestIndexerSkipsUnchangedFiles(t *testing.T) {
	db := openTestDB(t)

	// Use a counting enricher to verify skipping.
	counter := &countingEnricher{}
	redactor := NewRedactor(nil)
	idx := New(db, counter, redactor)

	tmpDir := t.TempDir()
	i3Dir := filepath.Join(tmpDir, "i3")
	if err := os.MkdirAll(i3Dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	configPath := filepath.Join(i3Dir, "config")
	if err := os.WriteFile(configPath, []byte("bindsym $mod+Return exec kitty\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := context.Background()
	if err := idx.Index(ctx, []string{configPath}); err != nil {
		t.Fatalf("first Index: %v", err)
	}
	if counter.calls != 1 {
		t.Fatalf("expected 1 enrich call, got %d", counter.calls)
	}

	// Index again without changes.
	if err := idx.Index(ctx, []string{configPath}); err != nil {
		t.Fatalf("second Index: %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("expected enricher not to be called again, but calls=%d", counter.calls)
	}
}

type countingEnricher struct {
	calls int
}

func (c *countingEnricher) Enrich(_ context.Context, entries []parsers.RawEntry) ([]EnrichedEntry, error) {
	c.calls++
	var out []EnrichedEntry
	for _, e := range entries {
		out = append(out, EnrichedEntry{
			Description: e.RawBinding + " runs " + e.RawAction,
			Intents:     []string{e.RawAction},
			Category:    "cat",
		})
	}
	return out, nil
}
