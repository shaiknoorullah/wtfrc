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

		// Step 2: Write default config if not exists
		cfgFile := filepath.Join(cfgDir, "config.toml")
		if _, statErr := os.Stat(cfgFile); statErr != nil {
			defaultCfg, err := os.ReadFile("configs/default.toml")
			if err != nil {
				defaultCfg = []byte("# wtfrc configuration\n# Run 'wtfrc config' to edit\n")
			}
			if err := os.WriteFile(cfgFile, defaultCfg, 0o644); err != nil {
				return fmt.Errorf("write default config: %w", err)
			}
			logger.Info("Default config written", "path", cfgFile)
		} else {
			logger.Info("Config already exists", "path", cfgFile)
		}

		// Step 3: Scan for known configs
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

		// Step 4: Check Ollama
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

		// Step 5: Run initial index if we have configs
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
