package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVSCodeParserCanParse(t *testing.T) {
	p := &VSCodeParser{}

	shouldMatch := []string{
		"/home/user/.config/Code/User/keybindings.json",
		"/home/user/Library/Application Support/Code/User/keybindings.json",
		"/some/path/keybindings.json",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.config/Code/User/settings.json",
		"/home/user/.bashrc",
		"/home/user/keybindings.yaml",
		"/home/user/keybindings.json.bak",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestVSCodeParserName(t *testing.T) {
	p := &VSCodeParser{}
	if got := p.Name(); got != "vscode" {
		t.Errorf("Name() = %q, want %q", got, "vscode")
	}
}

func TestVSCodeParserParse(t *testing.T) {
	content := `[
  {
    "key": "ctrl+shift+p",
    "command": "workbench.action.showCommands"
  },
  {
    "key": "ctrl+p",
    "command": "workbench.action.quickOpen"
  },
  {
    "key": "ctrl+shift+f",
    "command": "workbench.action.findInFiles",
    "when": "!searchViewletVisible"
  }
]
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &VSCodeParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}

	// Verify first entry
	first := entries[0]
	if first.RawBinding != "ctrl+shift+p" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "ctrl+shift+p")
	}
	if first.RawAction != "workbench.action.showCommands" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "workbench.action.showCommands")
	}
	if first.Type != EntryKeybind {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.Tool != "vscode" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "vscode")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Verify second entry
	second := entries[1]
	if second.RawBinding != "ctrl+p" {
		t.Errorf("entries[1].RawBinding = %q, want %q", second.RawBinding, "ctrl+p")
	}
	if second.RawAction != "workbench.action.quickOpen" {
		t.Errorf("entries[1].RawAction = %q, want %q", second.RawAction, "workbench.action.quickOpen")
	}

	// Verify third entry
	third := entries[2]
	if third.RawBinding != "ctrl+shift+f" {
		t.Errorf("entries[2].RawBinding = %q, want %q", third.RawBinding, "ctrl+shift+f")
	}
	if third.RawAction != "workbench.action.findInFiles" {
		t.Errorf("entries[2].RawAction = %q, want %q", third.RawAction, "workbench.action.findInFiles")
	}

	// All entries should be keybinds with tool "vscode"
	for i, e := range entries {
		if e.Tool != "vscode" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "vscode")
		}
		if e.Type != EntryKeybind {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryKeybind)
		}
	}
}

func TestVSCodeParserParseInvalid(t *testing.T) {
	content := `this is not valid JSON`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &VSCodeParser{}
	_, err := p.Parse(confPath)
	if err == nil {
		t.Fatal("Parse should return error for invalid JSON")
	}
}
