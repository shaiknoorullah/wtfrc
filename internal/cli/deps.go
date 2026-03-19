package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
)

// deps holds shared runtime dependencies initialised once by newDeps.
type deps struct {
	Cfg      *config.Config
	DB       *kb.DB
	FastLLM  llm.Provider
	StrongLLM llm.Provider
}

// configDir returns ~/.config/wtfrc, creating it if necessary.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "wtfrc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// dataDir returns ~/.local/share/wtfrc, creating it if necessary.
func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".local", "share", "wtfrc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	return dir, nil
}

// configPath returns the default path to config.toml.
func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// dbPath returns the default path to kb.db.
func dbPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kb.db"), nil
}

// newProvider creates an LLM provider from a provider config entry.
func newProvider(pcfg config.LLMProviderConfig) llm.Provider {
	switch pcfg.Provider {
	case "ollama":
		baseURL := pcfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return llm.NewOllama(baseURL, pcfg.Model)
	case "openai-compat":
		return llm.NewOpenAICompat(pcfg.BaseURL, pcfg.Model, pcfg.APIKeyEnv)
	default:
		// Default to ollama for unrecognised providers.
		baseURL := pcfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return llm.NewOllama(baseURL, pcfg.Model)
	}
}

// newDeps loads config, opens the DB, and creates LLM providers.
func newDeps() (*deps, error) {
	cfgFile, err := configPath()
	if err != nil {
		return nil, err
	}

	// Load config; if file doesn't exist, viper returns defaults.
	var cfg *config.Config
	if _, statErr := os.Stat(cfgFile); statErr == nil {
		cfg, err = config.Load(cfgFile)
	} else {
		cfg, err = config.Load("")
	}
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	dbFile, err := dbPath()
	if err != nil {
		return nil, err
	}

	db, err := kb.Open(dbFile)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	fast := newProvider(cfg.LLM.Fast)
	strong := newProvider(cfg.LLM.Strong)

	return &deps{
		Cfg:       cfg,
		DB:        db,
		FastLLM:   &llm.FallbackProvider{Primary: fast, Fallback: strong},
		StrongLLM: &llm.FallbackProvider{Primary: strong, Fallback: fast},
	}, nil
}
