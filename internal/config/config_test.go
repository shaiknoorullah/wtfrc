package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load('') returned error: %v", err)
	}

	if cfg.LLM.Fast.Provider != "ollama" {
		t.Errorf("expected fast provider 'ollama', got %q", cfg.LLM.Fast.Provider)
	}
	if cfg.LLM.Fast.Model != "gemma3:4b" {
		t.Errorf("expected fast model 'gemma3:4b', got %q", cfg.LLM.Fast.Model)
	}
	if cfg.Popup.MaxContextEntries != 10 {
		t.Errorf("expected max_context_entries 10, got %d", cfg.Popup.MaxContextEntries)
	}
	if cfg.Session.RetainDays != 90 {
		t.Errorf("expected retain_days 90, got %d", cfg.Session.RetainDays)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[llm.fast]
provider = "openai-compat"
model = "llama-3.1-8b-instant"
base_url = "https://api.groq.com/openai/v1"
api_key_env = "GROQ_API_KEY"

[popup]
max_context_entries = 5
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", cfgPath, err)
	}

	if cfg.LLM.Fast.Provider != "openai-compat" {
		t.Errorf("expected fast provider 'openai-compat', got %q", cfg.LLM.Fast.Provider)
	}
	if cfg.LLM.Fast.Model != "llama-3.1-8b-instant" {
		t.Errorf("expected fast model 'llama-3.1-8b-instant', got %q", cfg.LLM.Fast.Model)
	}
	if cfg.LLM.Fast.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("expected fast base_url 'https://api.groq.com/openai/v1', got %q", cfg.LLM.Fast.BaseURL)
	}
	if cfg.LLM.Fast.APIKeyEnv != "GROQ_API_KEY" {
		t.Errorf("expected fast api_key_env 'GROQ_API_KEY', got %q", cfg.LLM.Fast.APIKeyEnv)
	}
	if cfg.Popup.MaxContextEntries != 5 {
		t.Errorf("expected max_context_entries 5, got %d", cfg.Popup.MaxContextEntries)
	}
	// Unset values should still have defaults
	if cfg.Session.RetainDays != 90 {
		t.Errorf("expected retain_days 90 (default), got %d", cfg.Session.RetainDays)
	}
}
