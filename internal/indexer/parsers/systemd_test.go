package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdParserCanParse(t *testing.T) {
	p := &SystemdParser{}

	shouldMatch := []string{
		"/etc/systemd/system/nginx.service",
		"/home/user/.config/systemd/user/backup.service",
		"/usr/lib/systemd/system/docker.service",
		"/etc/systemd/system/multi-user.target.wants/sshd.service",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.bashrc",
		"/etc/systemd/system/nginx.timer",
		"/etc/systemd/system/network.socket",
		"/home/user/myapp.service.bak",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestSystemdParserName(t *testing.T) {
	p := &SystemdParser{}
	if got := p.Name(); got != "systemd" {
		t.Errorf("Name() = %q, want %q", got, "systemd")
	}
}

func TestSystemdParserParse(t *testing.T) {
	content := `[Unit]
Description=My Custom Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/myapp --config /etc/myapp.conf
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "myapp.service")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &SystemdParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 1 {
		t.Fatalf("got %d entries, want 1", got)
	}

	entry := entries[0]
	if entry.RawBinding != "My Custom Service" {
		t.Errorf("RawBinding = %q, want %q", entry.RawBinding, "My Custom Service")
	}
	if entry.RawAction != "/usr/bin/myapp --config /etc/myapp.conf" {
		t.Errorf("RawAction = %q, want %q", entry.RawAction, "/usr/bin/myapp --config /etc/myapp.conf")
	}
	if entry.Type != EntryService {
		t.Errorf("Type = %q, want %q", entry.Type, EntryService)
	}
	if entry.Tool != "systemd" {
		t.Errorf("Tool = %q, want %q", entry.Tool, "systemd")
	}
	if entry.SourceFile != confPath {
		t.Errorf("SourceFile = %q, want %q", entry.SourceFile, confPath)
	}
}

func TestSystemdParserParseNoDescription(t *testing.T) {
	content := `[Unit]
After=network.target

[Service]
ExecStart=/usr/bin/another-app
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "another.service")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &SystemdParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 1 {
		t.Fatalf("got %d entries, want 1", got)
	}

	// Without Description, RawBinding should be the filename
	entry := entries[0]
	if entry.RawBinding != "another.service" {
		t.Errorf("RawBinding = %q, want %q", entry.RawBinding, "another.service")
	}
	if entry.RawAction != "/usr/bin/another-app" {
		t.Errorf("RawAction = %q, want %q", entry.RawAction, "/usr/bin/another-app")
	}
}

func TestSystemdParserParseNoExecStart(t *testing.T) {
	content := `[Unit]
Description=No exec start service

[Service]
Type=oneshot
RemainAfterExit=yes
`

	dir := t.TempDir()
	confPath := filepath.Join(dir, "noexec.service")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &SystemdParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	// No ExecStart means no entry
	if got := len(entries); got != 0 {
		t.Fatalf("got %d entries, want 0", got)
	}
}
