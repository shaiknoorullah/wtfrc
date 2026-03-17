package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestI3ParserCanParse(t *testing.T) {
	p := &I3Parser{}

	shouldMatch := []string{
		"/home/user/.config/i3/config",
		"/home/user/.config/sway/config",
		"/etc/i3/config",
		"/etc/sway/config",
		"/some/deep/path/i3/config",
		"/some/deep/path/sway/config",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.config/kitty/kitty.conf",
		"/home/user/.bashrc",
		"/home/user/.config/i3/config.bak",
		"/home/user/.config/config",
		"/home/user/sway/config.txt",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestI3ParserParse(t *testing.T) {
	content := `# i3 config file
set $mod Mod4

# keybindings
bindsym $mod+Shift+q kill
bindsym $mod+Return exec kitty
bindsym $mod+1 workspace number 1
bindsym $mod+2 workspace number 2

# bindcode example
bindcode 133 exec rofi

# non-keybind exec line
exec --no-startup-id nm-applet
`

	dir := t.TempDir()
	configDir := filepath.Join(dir, "i3")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &I3Parser{}
	entries, err := p.Parse(configPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 5 {
		t.Fatalf("got %d entries, want 5", got)
	}

	// Verify first entry: bindsym $mod+Shift+q kill
	first := entries[0]
	if first.RawBinding != "$mod+Shift+q" {
		t.Errorf("first entry binding = %q, want %q", first.RawBinding, "$mod+Shift+q")
	}
	if first.RawAction != "kill" {
		t.Errorf("first entry action = %q, want %q", first.RawAction, "kill")
	}
	if first.Type != EntryKeybind {
		t.Errorf("first entry type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.Tool != "i3" {
		t.Errorf("first entry tool = %q, want %q", first.Tool, "i3")
	}
	if first.SourceFile != configPath {
		t.Errorf("first entry source file = %q, want %q", first.SourceFile, configPath)
	}

	// Verify bindcode entry: bindcode 133 exec rofi
	bc := entries[4]
	if bc.RawBinding != "133" {
		t.Errorf("bindcode entry binding = %q, want %q", bc.RawBinding, "133")
	}
	if bc.RawAction != "exec rofi" {
		t.Errorf("bindcode entry action = %q, want %q", bc.RawAction, "exec rofi")
	}
	if bc.Type != EntryKeybind {
		t.Errorf("bindcode entry type = %q, want %q", bc.Type, EntryKeybind)
	}

	// Verify context lines are non-empty
	for i, e := range entries {
		if e.ContextLines == "" {
			t.Errorf("entry %d has empty ContextLines", i)
		}
	}
}

func TestI3ParserName(t *testing.T) {
	p := &I3Parser{}
	if got := p.Name(); got != "i3" {
		t.Errorf("Name() = %q, want %q", got, "i3")
	}
}

func TestI3ParserSwayToolName(t *testing.T) {
	content := `bindsym $mod+Return exec foot
`
	dir := t.TempDir()
	configDir := filepath.Join(dir, "sway")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &I3Parser{}
	entries, err := p.Parse(configPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Tool != "sway" {
		t.Errorf("tool = %q, want %q", entries[0].Tool, "sway")
	}
}
