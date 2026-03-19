package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHyprlandParserCanParse(t *testing.T) {
	p := &HyprlandParser{}

	shouldMatch := []string{
		"/home/user/.config/hypr/hyprland.conf",
		"/etc/hypr/hyprland.conf",
		"/some/deep/path/hyprland.conf",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.config/hypr/hyprpaper.conf",
		"/home/user/.config/kitty/kitty.conf",
		"/home/user/.bashrc",
		"/home/user/hyprland.conf.bak",
		"/home/user/.config/hypr/hyprland.txt",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestHyprlandParserName(t *testing.T) {
	p := &HyprlandParser{}
	if got := p.Name(); got != "hyprland" {
		t.Errorf("Name() = %q, want %q", got, "hyprland")
	}
}

func TestHyprlandParserParse(t *testing.T) {
	content := `# Hyprland config
$mainMod = SUPER

# Keybindings
bind = $mainMod, Q, killactive
bind = $mainMod SHIFT, E, exit
bind = $mainMod, Return, exec, kitty
bind = $mainMod, D, exec, wofi --show drun
bind = $mainMod, V, togglefloating

# Window rules
windowrule = float,^(pavucontrol)$
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "hyprland.conf")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &HyprlandParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 5 {
		t.Fatalf("got %d entries, want 5", got)
	}

	// Verify first entry: bind = $mainMod, Q, killactive
	first := entries[0]
	if first.RawBinding != "$mainMod, Q" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "$mainMod, Q")
	}
	if first.RawAction != "killactive" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "killactive")
	}
	if first.Type != EntryKeybind {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.Tool != "hyprland" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "hyprland")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Verify second entry: bind = $mainMod SHIFT, E, exit
	second := entries[1]
	if second.RawBinding != "$mainMod SHIFT, E" {
		t.Errorf("entries[1].RawBinding = %q, want %q", second.RawBinding, "$mainMod SHIFT, E")
	}
	if second.RawAction != "exit" {
		t.Errorf("entries[1].RawAction = %q, want %q", second.RawAction, "exit")
	}

	// Verify third entry: bind = $mainMod, Return, exec, kitty
	third := entries[2]
	if third.RawBinding != "$mainMod, Return" {
		t.Errorf("entries[2].RawBinding = %q, want %q", third.RawBinding, "$mainMod, Return")
	}
	if third.RawAction != "exec, kitty" {
		t.Errorf("entries[2].RawAction = %q, want %q", third.RawAction, "exec, kitty")
	}

	// Verify fourth entry: bind = $mainMod, D, exec, wofi --show drun
	fourth := entries[3]
	if fourth.RawBinding != "$mainMod, D" {
		t.Errorf("entries[3].RawBinding = %q, want %q", fourth.RawBinding, "$mainMod, D")
	}
	if fourth.RawAction != "exec, wofi --show drun" {
		t.Errorf("entries[3].RawAction = %q, want %q", fourth.RawAction, "exec, wofi --show drun")
	}

	// All entries should have non-empty ContextLines
	for i, e := range entries {
		if e.ContextLines == "" {
			t.Errorf("entry %d has empty ContextLines", i)
		}
	}

	// All entries should be keybinds with tool "hyprland"
	for i, e := range entries {
		if e.Tool != "hyprland" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "hyprland")
		}
		if e.Type != EntryKeybind {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryKeybind)
		}
	}
}
