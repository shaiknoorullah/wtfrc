package coach

import (
	"os"
	"strings"
	"testing"
)

// TestNeovimPlugin verifies that the neovim coaching plugin contains required components
func TestNeovimPlugin(t *testing.T) {
	// Read the Lua script
	content, err := os.ReadFile("../../scripts/wtfrc-coach.lua")
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
