package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTmuxParserParse(t *testing.T) {
	content := `# tmux configuration
set -g prefix C-a

bind-key r source-file ~/.tmux.conf
bind -r h select-pane -L
bind -r j select-pane -D
bind [ copy-mode
bind -T copy-mode-vi v send -X begin-selection
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	p := &TmuxParser{}

	// Verify Name()
	if got := p.Name(); got != "tmux" {
		t.Errorf("Name() = %q, want %q", got, "tmux")
	}

	// Verify CanParse()
	if !p.CanParse(confPath) {
		t.Errorf("CanParse(%q) = false, want true", confPath)
	}
	if !p.CanParse("/home/user/.tmux.conf") {
		t.Error("CanParse should return true for .tmux.conf")
	}
	if p.CanParse("/home/user/.bashrc") {
		t.Error("CanParse should return false for .bashrc")
	}

	// Parse
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if got := len(entries); got != 5 {
		t.Fatalf("Parse() returned %d entries, want 5", got)
	}

	// First entry: bind-key r source-file ~/.tmux.conf
	first := entries[0]
	if first.RawBinding != "r" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "r")
	}
	if first.RawAction != "source-file ~/.tmux.conf" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "source-file ~/.tmux.conf")
	}
	if first.Tool != "tmux" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "tmux")
	}
	if first.Type != EntryKeybind {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Spot-check additional entries
	// bind -r h select-pane -L
	if entries[1].RawBinding != "h" {
		t.Errorf("entries[1].RawBinding = %q, want %q", entries[1].RawBinding, "h")
	}
	if entries[1].RawAction != "select-pane -L" {
		t.Errorf("entries[1].RawAction = %q, want %q", entries[1].RawAction, "select-pane -L")
	}

	// bind -r j select-pane -D
	if entries[2].RawBinding != "j" {
		t.Errorf("entries[2].RawBinding = %q, want %q", entries[2].RawBinding, "j")
	}
	if entries[2].RawAction != "select-pane -D" {
		t.Errorf("entries[2].RawAction = %q, want %q", entries[2].RawAction, "select-pane -D")
	}

	// bind [ copy-mode
	if entries[3].RawBinding != "[" {
		t.Errorf("entries[3].RawBinding = %q, want %q", entries[3].RawBinding, "[")
	}
	if entries[3].RawAction != "copy-mode" {
		t.Errorf("entries[3].RawAction = %q, want %q", entries[3].RawAction, "copy-mode")
	}

	// bind -T copy-mode-vi v send -X begin-selection
	if entries[4].RawBinding != "v" {
		t.Errorf("entries[4].RawBinding = %q, want %q", entries[4].RawBinding, "v")
	}
	if entries[4].RawAction != "send -X begin-selection" {
		t.Errorf("entries[4].RawAction = %q, want %q", entries[4].RawAction, "send -X begin-selection")
	}

	// All entries should have Tool=tmux and Type=keybind
	for i, e := range entries {
		if e.Tool != "tmux" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "tmux")
		}
		if e.Type != EntryKeybind {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryKeybind)
		}
	}
}
