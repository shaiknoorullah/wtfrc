package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKittyParserParse(t *testing.T) {
	content := `# Kitty terminal config
font_size 12.0

map ctrl+shift+c copy_to_clipboard
map ctrl+shift+v paste_from_clipboard
map ctrl+shift+t new_tab
map ctrl+shift+enter new_window

scrollback_lines 10000
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "kitty.conf")
	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	p := &KittyParser{}

	if name := p.Name(); name != "kitty" {
		t.Errorf("Name() = %q, want %q", name, "kitty")
	}

	if !p.CanParse(confPath) {
		t.Error("CanParse should return true for kitty.conf")
	}

	if p.CanParse("/some/random/file.txt") {
		t.Error("CanParse should return false for non-kitty files")
	}

	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	first := entries[0]
	if first.RawBinding != "ctrl+shift+c" {
		t.Errorf("first entry binding = %q, want %q", first.RawBinding, "ctrl+shift+c")
	}
	if first.RawAction != "copy_to_clipboard" {
		t.Errorf("first entry action = %q, want %q", first.RawAction, "copy_to_clipboard")
	}
	if first.Tool != "kitty" {
		t.Errorf("first entry tool = %q, want %q", first.Tool, "kitty")
	}
	if first.Type != EntryKeybind {
		t.Errorf("first entry type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.SourceFile != confPath {
		t.Errorf("first entry source file = %q, want %q", first.SourceFile, confPath)
	}

	// Verify all entries are keybinds with tool "kitty"
	for i, e := range entries {
		if e.Tool != "kitty" {
			t.Errorf("entry[%d].Tool = %q, want %q", i, e.Tool, "kitty")
		}
		if e.Type != EntryKeybind {
			t.Errorf("entry[%d].Type = %q, want %q", i, e.Type, EntryKeybind)
		}
	}

	// Verify all four expected bindings are present
	expectedBindings := []string{
		"ctrl+shift+c",
		"ctrl+shift+v",
		"ctrl+shift+t",
		"ctrl+shift+enter",
	}
	for i, want := range expectedBindings {
		if entries[i].RawBinding != want {
			t.Errorf("entry[%d].RawBinding = %q, want %q", i, entries[i].RawBinding, want)
		}
	}
}
