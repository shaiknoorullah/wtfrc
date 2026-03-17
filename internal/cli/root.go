package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "wtfrc",
	Short: "AI that reads your dotfiles so you don't have to",
	Long: `wtfrc indexes your dotfiles, configs, and keybindings into a local
semantic knowledge base, then answers natural-language questions about
your setup using a local LLM.

  wtfrc index      Index your config files
  wtfrc ask        Ask questions about your setup
  wtfrc search     Search the knowledge base directly`,
}

func Execute() error {
	return rootCmd.Execute()
}
