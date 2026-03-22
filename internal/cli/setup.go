package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/shaiknoorullah/wtfrc/internal/indexer"
	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "First-run setup: detect configs, generate config, run initial index",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.Default()

		// Step 1: Create config directory
		cfgDir, err := configDir()
		if err != nil {
			return err
		}
		logger.Info("Config directory", "path", cfgDir)

		// Step 2: Detect VRAM and recommend model
		vramMB := detectVRAM()
		recommendedOllamaModel := "qwen2.5-coder:7b" // default recommendation
		if vramMB > 0 {
			model, reason := recommendedModel(vramMB)
			recommendedOllamaModel = model
			fmt.Printf("Your GPU has ~%dGB VRAM. We recommend %s for enrichment.\n",
				vramMB/1024, model)
			_ = reason
		} else {
			fmt.Println("No NVIDIA GPU detected. Defaulting to qwen2.5-coder:7b for enrichment.")
		}

		// Step 3: Write default config if not exists
		cfgFile := filepath.Join(cfgDir, "config.toml")
		if _, statErr := os.Stat(cfgFile); statErr != nil {
			defaultCfg, err := os.ReadFile("configs/default.toml")
			if err != nil {
				defaultCfg = []byte("# wtfrc configuration\n# Run 'wtfrc config' to edit\n")
			}
			// Replace the strong LLM section with the recommended Ollama model
			cfgStr := string(defaultCfg)
			if !strings.Contains(cfgStr, "[llm.strong]") {
				// Append an Ollama-based strong LLM section
				cfgStr += fmt.Sprintf("\n[llm.strong]\nprovider = \"ollama\"\nmodel = %q\n", recommendedOllamaModel)
			} else {
				// Replace existing strong section model with recommended
				cfgStr = replaceStrongModel(cfgStr, recommendedOllamaModel)
			}
			if err := os.WriteFile(cfgFile, []byte(cfgStr), 0o644); err != nil {
				return fmt.Errorf("write default config: %w", err)
			}
			logger.Info("Default config written", "path", cfgFile)
		} else {
			logger.Info("Config already exists", "path", cfgFile)
		}

		// Step 4: Scan for known configs
		home, _ := os.UserHomeDir()
		knownConfigs := discoverConfigs(home)
		if len(knownConfigs) > 0 {
			logger.Info("Discovered config files", "count", len(knownConfigs))
			for _, c := range knownConfigs {
				fmt.Printf("  %s\n", c)
			}
		} else {
			logger.Warn("No config files discovered")
		}

		// Step 5: Check Ollama
		d, err := newDeps()
		if err != nil {
			logger.Warn("Could not initialize dependencies", "error", err)
			fmt.Println("\nTo use wtfrc, you need Ollama running:")
			fmt.Println("  curl -fsSL https://ollama.com/install.sh | sh")
			fmt.Println("  ollama pull gemma3:4b")
			return nil
		}
		defer d.DB.Close()

		ctx := context.Background()
		if err := d.FastLLM.HealthCheck(ctx); err != nil {
			logger.Warn("Ollama not reachable", "error", err)
			fmt.Println("\nStart Ollama and pull the default model:")
			fmt.Println("  ollama serve &")
			fmt.Println("  ollama pull gemma3:4b")
		} else {
			logger.Info("Ollama is running")
		}

		// Step 6: Run initial index if we have configs
		if len(knownConfigs) > 0 {
			logger.Info("Running initial index...")
			enricher := indexer.NewLLMEnricher(d.StrongLLM)
			redactor := indexer.NewRedactor(d.Cfg.Privacy.RedactPatterns)
			idx := indexer.New(d.DB, enricher, redactor)

			if err := idx.Index(ctx, knownConfigs); err != nil {
				logger.Warn("Index had errors", "error", err)
			} else {
				logger.Info("Initial index complete")
			}
		}

		fmt.Println()
		fmt.Println("Setup complete! Try:")
		fmt.Println("  wtfrc ask 'how do I close a window'")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

// replaceStrongModel replaces the [llm.strong] section's provider and model
// with the recommended Ollama model.
func replaceStrongModel(cfg, model string) string {
	lines := strings.Split(cfg, "\n")
	var result []string
	inStrong := false
	replaced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[llm.strong]" {
			inStrong = true
			result = append(result, line, `provider = "ollama"`, fmt.Sprintf("model = %q", model))
			replaced = true
			continue
		}
		if inStrong {
			// Skip old provider/model/base_url/api_key_env lines in [llm.strong]
			if strings.HasPrefix(trimmed, "provider") ||
				strings.HasPrefix(trimmed, "model") ||
				strings.HasPrefix(trimmed, "base_url") ||
				strings.HasPrefix(trimmed, "api_key_env") {
				continue
			}
			// Any new section header ends [llm.strong]
			if strings.HasPrefix(trimmed, "[") {
				inStrong = false
			}
		}
		result = append(result, line)
	}
	_ = replaced
	return strings.Join(result, "\n")
}

func discoverConfigs(home string) []string {
	candidates := []string{
		filepath.Join(home, ".config", "i3", "config"),
		filepath.Join(home, ".config", "sway", "config"),
		filepath.Join(home, ".config", "hypr", "hyprland.conf"),
		filepath.Join(home, ".config", "kitty", "kitty.conf"),
		filepath.Join(home, ".tmux.conf"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".gitconfig"),
		filepath.Join(home, ".ssh", "config"),
	}

	var found []string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			// Verify we have a parser for it
			if parsers.ForFile(c) != nil || strings.HasSuffix(c, ".gitconfig") || strings.HasSuffix(c, "ssh/config") {
				found = append(found, c)
			}
		}
	}
	return found
}
