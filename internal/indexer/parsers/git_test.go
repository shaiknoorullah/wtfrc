package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitParserCanParse(t *testing.T) {
	p := &GitParser{}

	shouldMatch := []string{
		"/home/user/.gitconfig",
		"/home/user/.config/git/config",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.bashrc",
		"/home/user/.config/kitty/kitty.conf",
		"/home/user/git/config.bak",
		"/home/user/.gitignore",
		"/home/user/.config/git/config.d/extra",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestGitParserName(t *testing.T) {
	p := &GitParser{}
	if got := p.Name(); got != "git" {
		t.Errorf("Name() = %q, want %q", got, "git")
	}
}

func TestGitParserParse(t *testing.T) {
	content := `[user]
	name = John Doe
	email = john@example.com
[alias]
	co = checkout
	st = status
	lg = log --oneline --graph
[core]
	editor = nvim
	autocrlf = false
[push]
	default = current
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, ".gitconfig")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &GitParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}

	// Verify first alias: co = checkout
	first := entries[0]
	if first.RawBinding != "co" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "co")
	}
	if first.RawAction != "checkout" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "checkout")
	}
	if first.Type != EntryAlias {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryAlias)
	}
	if first.Tool != "git" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "git")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Verify second alias: st = status
	second := entries[1]
	if second.RawBinding != "st" {
		t.Errorf("entries[1].RawBinding = %q, want %q", second.RawBinding, "st")
	}
	if second.RawAction != "status" {
		t.Errorf("entries[1].RawAction = %q, want %q", second.RawAction, "status")
	}

	// Verify third alias: lg = log --oneline --graph
	third := entries[2]
	if third.RawBinding != "lg" {
		t.Errorf("entries[2].RawBinding = %q, want %q", third.RawBinding, "lg")
	}
	if third.RawAction != "log --oneline --graph" {
		t.Errorf("entries[2].RawAction = %q, want %q", third.RawAction, "log --oneline --graph")
	}

	// All entries should be aliases with tool "git"
	for i, e := range entries {
		if e.Tool != "git" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "git")
		}
		if e.Type != EntryAlias {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryAlias)
		}
	}
}

func TestGitParserParseMultipleSections(t *testing.T) {
	content := `[user]
	name = Jane
[alias]
	br = branch
[merge]
	tool = vimdiff
[alias]
	ci = commit
`

	dir := t.TempDir()
	confDir := filepath.Join(dir, "git")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	confPath := filepath.Join(confDir, "config")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &GitParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 2 {
		t.Fatalf("got %d entries, want 2", got)
	}

	if entries[0].RawBinding != "br" {
		t.Errorf("entries[0].RawBinding = %q, want %q", entries[0].RawBinding, "br")
	}
	if entries[1].RawBinding != "ci" {
		t.Errorf("entries[1].RawBinding = %q, want %q", entries[1].RawBinding, "ci")
	}
}
