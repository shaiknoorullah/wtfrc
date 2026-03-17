package indexer

import (
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	r := NewRedactor([]string{"sk-", "xoxb-", "ghp_", "password", "secret", "token"})

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "sk- prefixed API key",
			input:    `export OPENAI_API_KEY=sk-abc123def456ghi789`,
			expected: `export OPENAI_API_KEY=[REDACTED]`,
		},
		{
			name:     "SSH IdentityFile",
			input:    `IdentityFile ~/.ssh/id_ed25519`,
			expected: `IdentityFile [REDACTED_KEY_PATH]`,
		},
		{
			name:     "PostgreSQL URL with password",
			input:    `DATABASE_URL=postgres://admin:s3cretPass@localhost:5432/mydb`,
			expected: `DATABASE_URL=postgres://admin:[REDACTED]@localhost:5432/mydb`,
		},
		{
			name:     "Slack token",
			input:    `SLACK_TOKEN=xoxb-1234-5678-abcdef`,
			expected: `SLACK_TOKEN=[REDACTED]`,
		},
		{
			name:     "normal config line",
			input:    `set -g prefix C-a`,
			expected: `set -g prefix C-a`,
		},
		{
			name:     "GitHub PAT",
			input:    `GITHUB_TOKEN=ghp_abc123def456`,
			expected: `GITHUB_TOKEN=[REDACTED]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Redact(tt.input)
			if got != tt.expected {
				t.Errorf("Redact(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}
