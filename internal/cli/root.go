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

  wtfrc ask        Ask questions about your setup (one-shot or REPL)
  wtfrc index      Index your config files
  wtfrc search     Search the knowledge base (FTS, no LLM)
  wtfrc list       List all entries in the knowledge base
  wtfrc stats      Show knowledge base and usage statistics
  wtfrc doctor     Run health checks on your setup
  wtfrc config     View or initialize the config file
  wtfrc supervise  Run supervisor hallucination review`,
}

func Execute() error {
	return rootCmd.Execute()
}
