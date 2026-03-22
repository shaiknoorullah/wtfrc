package coach

import (
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// helpers

func strPtr(s string) *string { return &s }

func makeAlias(aliasName, expansion, tool, sourceFile string, sourceLine int) kb.KBEntry {
	return kb.KBEntry{
		Tool:       tool,
		Type:       parsers.EntryAlias,
		RawBinding: strPtr(aliasName),
		RawAction:  strPtr(expansion),
		SourceFile: sourceFile,
		SourceLine: sourceLine,
	}
}

// ---------------------------------------------------------------------------
// TestNormalize
// ---------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  git  status  ", "git status"},
		{"sudo docker ps", "docker ps"},
		{"noglob git log", "git log"},
		{"git log | head", "git log"},
		{"git status", "git status"},
		{"nocorrect ls -la", "ls -la"},
		{"command ls", "ls"},
		// "sudo" alone must NOT be stripped
		{"sudo", "sudo"},
		// prefix only stripped when followed by space + more text
		{"noglob", "noglob"},
		// pipe at end — trailing pipe chain removed
		{"git log | head -n 10", "git log"},
		// multiple spaces around operators
		{"git status  &&  git diff", "git status && git diff"},
	}

	for _, tc := range tests {
		got := normalize(tc.input)
		if got != tc.expected {
			t.Errorf("normalize(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchExact
// ---------------------------------------------------------------------------

func TestMatchExact(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
		makeAlias("gp", "git push", "zsh", "/home/user/.zshrc", 11),
		makeAlias("ll", "ls -la", "zsh", "/home/user/.zshrc", 12),
	}
	m := NewMatcher(entries)

	tests := []struct {
		source      string
		action      string
		wantNil     bool
		wantOptimal string
	}{
		{SourceShell, "git status", false, "gs"},
		{SourceShell, "git push origin main", false, "gp origin main"},
		{SourceShell, "ls -la", false, "ll"},
		{SourceShell, "git commit", true, ""},
		// user already typed the alias — no suggestion
		{SourceShell, "gs", true, ""},
	}

	for _, tc := range tests {
		got := m.Match(tc.source, tc.action)
		if tc.wantNil {
			if got != nil {
				t.Errorf("Match(%q, %q) = %+v, want nil", tc.source, tc.action, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("Match(%q, %q) = nil, want Suggestion{Optimal: %q}", tc.source, tc.action, tc.wantOptimal)
			continue
		}
		if got.Optimal != tc.wantOptimal {
			t.Errorf("Match(%q, %q).Optimal = %q, want %q", tc.source, tc.action, got.Optimal, tc.wantOptimal)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchParameterized
// ---------------------------------------------------------------------------

func TestMatchParameterized(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("dclogs", "docker compose logs -f", "zsh", "/home/user/.zshrc", 20),
		makeAlias("dcup", "docker compose up -d", "zsh", "/home/user/.zshrc", 21),
	}
	m := NewMatcher(entries)

	tests := []struct {
		source      string
		action      string
		wantNil     bool
		wantOptimal string
	}{
		{SourceShell, "docker compose logs -f myservice", false, "dclogs myservice"},
		{SourceShell, "docker compose up -d", false, "dcup"},
		{SourceShell, "docker compose down", true, ""},
	}

	for _, tc := range tests {
		got := m.Match(tc.source, tc.action)
		if tc.wantNil {
			if got != nil {
				t.Errorf("Match(%q, %q) = %+v, want nil", tc.source, tc.action, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("Match(%q, %q) = nil, want Suggestion{Optimal: %q}", tc.source, tc.action, tc.wantOptimal)
			continue
		}
		if got.Optimal != tc.wantOptimal {
			t.Errorf("Match(%q, %q).Optimal = %q, want %q", tc.source, tc.action, got.Optimal, tc.wantOptimal)
		}
	}
}

// ---------------------------------------------------------------------------
// TestMatchKeySaved
// ---------------------------------------------------------------------------

func TestMatchKeySaved(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	want := len("git status") - len("gs") // 10 - 2 = 8
	if got.KeysSaved != want {
		t.Errorf("KeysSaved = %d, want %d", got.KeysSaved, want)
	}
}

// ---------------------------------------------------------------------------
// TestMatchNoShorter
// ---------------------------------------------------------------------------

func TestMatchNoShorter(t *testing.T) {
	// alias "gitstat" is longer than expansion "git st" — should NOT suggest
	entries := []kb.KBEntry{
		makeAlias("gitstat", "git st", "zsh", "/home/user/.zshrc", 30),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git st")
	if got != nil {
		t.Errorf("Match returned %+v, want nil (alias longer than expansion)", got)
	}
}

// ---------------------------------------------------------------------------
// TestActionID
// ---------------------------------------------------------------------------

func TestActionID(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 10),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	want := "zsh:gs"
	if got.ActionID != want {
		t.Errorf("ActionID = %q, want %q", got.ActionID, want)
	}
}

// ---------------------------------------------------------------------------
// TestMatchSourceFile
// ---------------------------------------------------------------------------

func TestMatchSourceFile(t *testing.T) {
	entries := []kb.KBEntry{
		makeAlias("gs", "git status", "zsh", "/home/user/.zshrc", 42),
	}
	m := NewMatcher(entries)

	got := m.Match(SourceShell, "git status")
	if got == nil {
		t.Fatal("expected non-nil Suggestion")
	}
	if got.SourceFile != "/home/user/.zshrc" {
		t.Errorf("SourceFile = %q, want %q", got.SourceFile, "/home/user/.zshrc")
	}
	if got.SourceLine != 42 {
		t.Errorf("SourceLine = %d, want %d", got.SourceLine, 42)
	}
}
