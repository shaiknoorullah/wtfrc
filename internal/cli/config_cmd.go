package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or initialize the wtfrc config file",
	Long: `Opens the config file in $EDITOR for editing.

Use --init to generate a default config.toml at ~/.config/wtfrc/config.toml.`,
	RunE: runConfig,
}

var configInit bool

func init() {
	configCmd.Flags().BoolVar(&configInit, "init", false, "Generate default config.toml")
	rootCmd.AddCommand(configCmd)
}

const defaultConfig = `# wtfrc configuration
# See: https://github.com/shaiknoorullah/wtfrc

[general]
assistant_name = "wtfrc"

[indexer]
auto_discover = true
watch = false
reindex_schedule = "daily"
# scan_paths = ["~/.config", "~/.bashrc", "~/.zshrc"]
# exclude_paths = []

[llm.fast]
provider = "ollama"
model = "gemma3:4b"
# base_url = "http://localhost:11434"

[llm.strong]
provider = "openai-compat"
model = "claude-sonnet-4-20250514"
base_url = "https://api.anthropic.com/v1"
api_key_env = "ANTHROPIC_API_KEY"

[popup]
frontend = "term"
max_context_entries = 10
max_history = 5

[session]
retain_days = 90
archive_format = "jsonl"

[supervisor]
enabled = true
schedule = "daily"
model_tier = "strong"
retain_reports = 30

[privacy]
redact_patterns = ["sk-", "xoxb-", "ghp_", "password", "secret", "token"]
never_index = ["~/.ssh/id_*", "~/.gnupg/", "~/.aws/credentials", "**/.env"]
`

func runConfig(cmd *cobra.Command, args []string) error {
	cfgFile, err := configPath()
	if err != nil {
		return err
	}

	if configInit {
		return initConfig(cfgFile)
	}

	return editConfig(cfgFile)
}

func initConfig(cfgFile string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))

	if _, err := os.Stat(cfgFile); err == nil {
		return fmt.Errorf("config file already exists at %s; remove it first to re-initialize", cfgFile)
	}

	// Ensure the directory exists.
	if _, err := configDir(); err != nil {
		return err
	}

	if err := os.WriteFile(cfgFile, []byte(defaultConfig), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc config --init"))
	fmt.Fprintln(os.Stdout, successStyle.Render(fmt.Sprintf("Default config written to %s", cfgFile)))
	return nil
}

func editConfig(cfgFile string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// If config doesn't exist yet, create a default one first.
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "No config found; creating default at %s\n", cfgFile)
		if initErr := initConfig(cfgFile); initErr != nil {
			return initErr
		}
	}

	c := exec.Command(editor, cfgFile)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
