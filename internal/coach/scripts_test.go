package coach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptContent(t *testing.T) {
	// Read the wtfrc-coach.zsh script
	scriptPath := filepath.Join("..", "..", "scripts", "wtfrc-coach.zsh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}

	scriptContent := string(content)

	tests := []struct {
		name     string
		required string
		what     string
	}{
		{
			name:     "contains _wtfrc_coach_preexec function",
			required: "_wtfrc_coach_preexec",
			what:     "preexec function",
		},
		{
			name:     "contains _wtfrc_coach_precmd function",
			required: "_wtfrc_coach_precmd",
			what:     "precmd function",
		},
		{
			name:     "contains add-zsh-hook preexec",
			required: "add-zsh-hook preexec",
			what:     "preexec hook registration",
		},
		{
			name:     "contains add-zsh-hook precmd",
			required: "add-zsh-hook precmd",
			what:     "precmd hook registration",
		},
		{
			name:     "contains coach.fifo reference",
			required: "coach.fifo",
			what:     "FIFO reference",
		},
		{
			name:     "contains coach-msg reference",
			required: "coach-msg",
			what:     "message file reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(scriptContent, tt.required) {
				t.Errorf("script missing %s: expected to find %q in script", tt.what, tt.required)
			}
		})
	}
}

func TestScriptHasStrictMode(t *testing.T) {
	// Read the wtfrc-coach.zsh script
	scriptPath := filepath.Join("..", "..", "scripts", "wtfrc-coach.zsh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}

	scriptContent := string(content)

	tests := []struct {
		name     string
		required string
		what     string
	}{
		{
			name:     "contains _wtfrc_strict_preexec function",
			required: "_wtfrc_strict_preexec",
			what:     "strict preexec function",
		},
		{
			name:     "contains coach-strict.sock reference",
			required: "coach-strict.sock",
			what:     "strict socket reference",
		},
		{
			name:     "contains WTFRC_STRICT reference",
			required: "WTFRC_STRICT",
			what:     "strict mode environment variable",
		},
		{
			name:     "contains socat reference",
			required: "socat",
			what:     "socat socket command",
		},
		{
			name:     "contains reject: message format",
			required: "reject:",
			what:     "rejection message format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(scriptContent, tt.required) {
				t.Errorf("script missing %s: expected to find %q in script", tt.what, tt.required)
			}
		})
	}
}

// TestNeovimPlugin verifies that the neovim coaching plugin contains required components
func TestNeovimPlugin(t *testing.T) {
	// Read the Lua script
	scriptPath := filepath.Join("..", "..", "scripts", "wtfrc-coach.lua")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read wtfrc-coach.lua: %v", err)
	}

	script := string(content)

	tests := []struct {
		name     string
		required string
	}{
		{
			name:     "vim.on_key registration",
			required: "vim.on_key",
		},
		{
			name:     "pcall for error handling",
			required: "pcall",
		},
		{
			name:     "VimLeavePre autocmd",
			required: "VimLeavePre",
		},
		{
			name:     "coach.fifo path reference",
			required: "coach.fifo",
		},
		{
			name:     "200ms throttle",
			required: "200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(script, tt.required) {
				t.Errorf("script missing required component: %q", tt.required)
			}
		})
	}
}
