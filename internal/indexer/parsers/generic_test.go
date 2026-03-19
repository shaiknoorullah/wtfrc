package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenericParserCanParse(t *testing.T) {
	p := &GenericParser{}

	// Generic parser should accept any file path.
	paths := []string{
		"/home/user/.config/some/random.conf",
		"/etc/whatever.cfg",
		"/home/user/.bashrc",
		"/home/user/settings.toml",
	}
	for _, path := range paths {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}
}

func TestGenericParserName(t *testing.T) {
	p := &GenericParser{}
	if got := p.Name(); got != "generic" {
		t.Errorf("Name() = %q, want %q", got, "generic")
	}
}

func TestGenericParserParse(t *testing.T) {
	content := `# This is a comment
key1 = value1

key2 = value2
; another comment style
key3 = value3

`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "app.conf")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &GenericParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	// Should get 3 entries (non-blank, non-comment lines)
	if got := len(entries); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}

	// Verify first entry
	first := entries[0]
	if first.RawBinding != "key1 = value1" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "key1 = value1")
	}
	if first.RawAction != "" {
		t.Errorf("entries[0].RawAction = %q, want empty", first.RawAction)
	}
	if first.Type != EntrySetting {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntrySetting)
	}
	if first.Tool != "generic" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "generic")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}
	if first.SourceLine != 2 {
		t.Errorf("entries[0].SourceLine = %d, want 2", first.SourceLine)
	}

	// Verify line numbers are correct
	if entries[1].SourceLine != 4 {
		t.Errorf("entries[1].SourceLine = %d, want 4", entries[1].SourceLine)
	}
	if entries[2].SourceLine != 6 {
		t.Errorf("entries[2].SourceLine = %d, want 6", entries[2].SourceLine)
	}

	// All entries should be settings with tool "generic"
	for i, e := range entries {
		if e.Tool != "generic" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "generic")
		}
		if e.Type != EntrySetting {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntrySetting)
		}
	}
}

func TestGenericParserNotRegistered(t *testing.T) {
	// GenericParser should NOT be in the registry (it's a fallback).
	for _, p := range All() {
		if p.Name() == "generic" {
			t.Error("GenericParser should not be registered in the global registry")
		}
	}
}
