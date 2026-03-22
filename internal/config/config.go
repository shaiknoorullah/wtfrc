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
	Coach      CoachConfig      `mapstructure:"coach"`
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

type CoachConfig struct {
	Enabled          bool                   `mapstructure:"enabled"`
	Mode             string                 `mapstructure:"mode"`
	BudgetPerHour    int                    `mapstructure:"budget_per_hour"`
	CooldownSeconds  int                    `mapstructure:"cooldown_seconds"`
	QuietHours       string                 `mapstructure:"quiet_hours"`
	FocusCategory    string                 `mapstructure:"focus_category"`
	GraduationStreak int                    `mapstructure:"graduation_streak"`
	Layer4           CoachLayer4Config      `mapstructure:"layer4"`
	Delivery         CoachDeliveryConfig    `mapstructure:"delivery"`
	Personality      CoachPersonalityConfig `mapstructure:"personality"`
}

type CoachLayer4Config struct {
	Enabled        bool `mapstructure:"enabled"`
	EmitShiftChars bool `mapstructure:"emit_shift_chars"`
}

type CoachDeliveryConfig struct {
	Shell        string `mapstructure:"shell"`
	Hyprland     string `mapstructure:"hyprland"`
	Tmux         string `mapstructure:"tmux"`
	Neovim       string `mapstructure:"neovim"`
	Default      string `mapstructure:"default"`
	WaybarSignal int    `mapstructure:"waybar_signal"`
}

type CoachPersonalityConfig struct {
	CustomPrompt string `mapstructure:"custom_prompt"`
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("general.assistant_name", "wtfrc")

	v.SetDefault("indexer.auto_discover", true)
	v.SetDefault("indexer.watch", false)
	v.SetDefault("indexer.reindex_schedule", "daily")

	v.SetDefault("llm.fast.provider", "ollama")
	v.SetDefault("llm.fast.model", "gemma3:4b")

	v.SetDefault("llm.strong.provider", "ollama")
	v.SetDefault("llm.strong.model", "qwen2.5-coder:7b")

	v.SetDefault("popup.frontend", "term")
	v.SetDefault("popup.max_context_entries", 10)
	v.SetDefault("popup.max_history", 5)

	v.SetDefault("session.retain_days", 90)
	v.SetDefault("session.archive_format", "jsonl")

	v.SetDefault("supervisor.enabled", true)
	v.SetDefault("supervisor.schedule", "daily")
	v.SetDefault("supervisor.model_tier", "strong")
	v.SetDefault("supervisor.retain_reports", 30)

	v.SetDefault("coach.enabled", true)
	v.SetDefault("coach.mode", "chill")
	v.SetDefault("coach.budget_per_hour", 5)
	v.SetDefault("coach.cooldown_seconds", 120)
	v.SetDefault("coach.quiet_hours", "22:00-08:00")
	v.SetDefault("coach.focus_category", "")
	v.SetDefault("coach.graduation_streak", 7)
	v.SetDefault("coach.layer4.enabled", false)
	v.SetDefault("coach.layer4.emit_shift_chars", false)
	v.SetDefault("coach.delivery.shell", "inline")
	v.SetDefault("coach.delivery.hyprland", "dunst")
	v.SetDefault("coach.delivery.tmux", "status")
	v.SetDefault("coach.delivery.neovim", "notify")
	v.SetDefault("coach.delivery.default", "dunst")
	v.SetDefault("coach.delivery.waybar_signal", 8)
	v.SetDefault("coach.personality.custom_prompt", "")

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
