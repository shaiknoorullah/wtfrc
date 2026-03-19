package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHParserCanParse(t *testing.T) {
	p := &SSHParser{}

	shouldMatch := []string{
		"/home/user/.ssh/config",
		"/etc/ssh/config",
		"/home/user/ssh/config",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.bashrc",
		"/home/user/.ssh/known_hosts",
		"/home/user/.ssh/id_rsa",
		"/home/user/.config/sshd/config",
		"/home/user/config",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestSSHParserName(t *testing.T) {
	p := &SSHParser{}
	if got := p.Name(); got != "ssh" {
		t.Errorf("Name() = %q, want %q", got, "ssh")
	}
}

func TestSSHParserParse(t *testing.T) {
	content := `# SSH config
Host devbox
    HostName 192.168.1.100
    User deploy
    Port 2222

Host production
    HostName prod.example.com
    User admin
    IdentityFile ~/.ssh/prod_key

Host *
    ServerAliveInterval 60
`

	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("failed to create .ssh dir: %v", err)
	}
	confPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &SSHParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	// Expect 3 Host blocks: devbox, production, *
	if got := len(entries); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}

	// Verify first host: devbox
	first := entries[0]
	if first.RawBinding != "devbox" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "devbox")
	}
	if first.RawAction != "devbox -> 192.168.1.100 (deploy@192.168.1.100)" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "devbox -> 192.168.1.100 (deploy@192.168.1.100)")
	}
	if first.Type != EntryHost {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryHost)
	}
	if first.Tool != "ssh" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "ssh")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Verify second host: production
	second := entries[1]
	if second.RawBinding != "production" {
		t.Errorf("entries[1].RawBinding = %q, want %q", second.RawBinding, "production")
	}
	if second.RawAction != "production -> prod.example.com (admin@prod.example.com)" {
		t.Errorf("entries[1].RawAction = %q, want %q", second.RawAction, "production -> prod.example.com (admin@prod.example.com)")
	}

	// Verify third host: * (wildcard, no hostname/user)
	third := entries[2]
	if third.RawBinding != "*" {
		t.Errorf("entries[2].RawBinding = %q, want %q", third.RawBinding, "*")
	}
	if third.RawAction != "* -> *" {
		t.Errorf("entries[2].RawAction = %q, want %q", third.RawAction, "* -> *")
	}

	// All entries should be hosts with tool "ssh"
	for i, e := range entries {
		if e.Tool != "ssh" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "ssh")
		}
		if e.Type != EntryHost {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryHost)
		}
	}
}
