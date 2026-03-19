package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellParserCanParse(t *testing.T) {
	p := &ShellParser{}

	accepts := []string{
		"/home/user/.zshrc",
		"/home/user/.bashrc",
		"/home/user/.bash_profile",
		"/home/user/.zprofile",
	}
	for _, path := range accepts {
		if !p.CanParse(path) {
			t.Errorf("expected CanParse(%q) = true", path)
		}
	}

	rejects := []string{
		"/home/user/.config/kitty/kitty.conf",
		"/home/user/.vimrc",
		"/home/user/.config/sway/config",
		"/home/user/scripts/deploy.sh",
	}
	for _, path := range rejects {
		if p.CanParse(path) {
			t.Errorf("expected CanParse(%q) = false", path)
		}
	}
}

func TestShellParserParse(t *testing.T) {
	content := `# zsh config
alias k='kitty'
alias ll='ls -la'
alias gs='git status'
export EDITOR=nvim
export GOPATH=$HOME/go
function greet() {
  echo "hello"
}
backup() {
  tar czf backup.tar.gz "$@"
}
setopt auto_cd
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	p := &ShellParser{}
	entries, err := p.Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	var aliases, exports, functions []RawEntry
	for _, e := range entries {
		switch e.Type {
		case EntryAlias:
			aliases = append(aliases, e)
		case EntryExport:
			exports = append(exports, e)
		case EntryFunction:
			functions = append(functions, e)
		}
	}

	if got := len(aliases); got != 3 {
		t.Errorf("expected 3 aliases, got %d", got)
	}
	if got := len(exports); got != 2 {
		t.Errorf("expected 2 exports, got %d", got)
	}
	if got := len(functions); got != 2 {
		t.Errorf("expected 2 functions, got %d", got)
	}

	// Verify first alias details.
	if len(aliases) > 0 {
		a := aliases[0]
		if a.RawBinding != "k" {
			t.Errorf("expected first alias binding %q, got %q", "k", a.RawBinding)
		}
		if a.RawAction != "kitty" {
			t.Errorf("expected first alias action %q, got %q", "kitty", a.RawAction)
		}
		if a.Tool != "zsh" {
			t.Errorf("expected tool %q, got %q", "zsh", a.Tool)
		}
		if a.SourceFile != path {
			t.Errorf("expected SourceFile %q, got %q", path, a.SourceFile)
		}
	}
}
