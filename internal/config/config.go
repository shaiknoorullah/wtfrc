package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	General    GeneralConfig    `mapstructure:"general"`
	Indexer    IndexerConfig    `mapstructure:"indexer"`
	LLM        LLMConfig        `mapstructure:"llm"`
	Popup      PopupConfig      `mapstructure:"popup"`
	Session    SessionConfig    `mapstructure:"session"`
	Supervisor SupervisorConfig `mapstructure:"supervisor"`
	Privacy    PrivacyConfig    `mapstructure:"privacy"`
}

type GeneralConfig struct {
	AssistantName string `mapstructure:"assistant_name"`
}

type IndexerConfig struct {
	ScanPaths       []string `mapstructure:"scan_paths"`
	AutoDiscover    bool     `mapstructure:"auto_discover"`
	ExcludePaths    []string `mapstructure:"exclude_paths"`
	Watch           bool     `mapstructure:"watch"`
	ReindexSchedule string   `mapstructure:"reindex_schedule"`
}

type LLMConfig struct {
	Fast   LLMProviderConfig `mapstructure:"fast"`
	Strong LLMProviderConfig `mapstructure:"strong"`
}

type LLMProviderConfig struct {
	Provider  string `mapstructure:"provider"`
	Model     string `mapstructure:"model"`
	BaseURL   string `mapstructure:"base_url"`
	APIKeyEnv string `mapstructure:"api_key_env"`
}

type PopupConfig struct {
	Frontend          string `mapstructure:"frontend"`
	MaxContextEntries int    `mapstructure:"max_context_entries"`
	MaxHistory        int    `mapstructure:"max_history"`
}

type SessionConfig struct {
	RetainDays    int    `mapstructure:"retain_days"`
	ArchiveFormat string `mapstructure:"archive_format"`
}

type SupervisorConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Schedule      string `mapstructure:"schedule"`
	ModelTier     string `mapstructure:"model_tier"`
	RetainReports int    `mapstructure:"retain_reports"`
}

type PrivacyConfig struct {
	RedactPatterns []string `mapstructure:"redact_patterns"`
	NeverIndex     []string `mapstructure:"never_index"`
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("general.assistant_name", "wtfrc")

	v.SetDefault("indexer.auto_discover", true)
	v.SetDefault("indexer.watch", false)
	v.SetDefault("indexer.reindex_schedule", "daily")

	v.SetDefault("llm.fast.provider", "ollama")
	v.SetDefault("llm.fast.model", "gemma3:4b")

	v.SetDefault("llm.strong.provider", "openai-compat")
	v.SetDefault("llm.strong.model", "claude-sonnet-4-20250514")
	v.SetDefault("llm.strong.base_url", "https://api.anthropic.com/v1")
	v.SetDefault("llm.strong.api_key_env", "ANTHROPIC_API_KEY")

	v.SetDefault("popup.frontend", "term")
	v.SetDefault("popup.max_context_entries", 10)
	v.SetDefault("popup.max_history", 5)

	v.SetDefault("session.retain_days", 90)
	v.SetDefault("session.archive_format", "jsonl")

	v.SetDefault("supervisor.enabled", true)
	v.SetDefault("supervisor.schedule", "daily")
	v.SetDefault("supervisor.model_tier", "strong")
	v.SetDefault("supervisor.retain_reports", 30)

	v.SetDefault("privacy.redact_patterns", []string{
		"sk-", "xoxb-", "ghp_", "password", "secret", "token",
	})
	v.SetDefault("privacy.never_index", []string{
		"~/.ssh/id_*", "~/.gnupg/", "~/.aws/credentials", "**/.env",
	})
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")
	setDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
