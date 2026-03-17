package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the knowledge base (FTS only, no LLM)",
	Long: `Full-text search across the knowledge base without invoking the LLM.
Prints matching entries with tool, binding, description, and source location.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	query := strings.Join(args, " ")

	results, err := d.DB.Search(query, 20)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	toolStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	bindingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
	descStyle := lipgloss.NewStyle().PaddingLeft(4)
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Italic(true).PaddingLeft(4)

	fmt.Fprintln(os.Stdout, headerStyle.Render(fmt.Sprintf("Search results for: %s", query)))
	fmt.Fprintln(os.Stdout)

	if len(results) == 0 {
		fmt.Fprintln(os.Stdout, "  No results found.")
		return nil
	}

	for i, r := range results {
		binding := ""
		if r.RawBinding != nil {
			binding = *r.RawBinding
		}

		fmt.Fprintf(os.Stdout, "  %d. %s", i+1, toolStyle.Render(r.Tool))
		if binding != "" {
			fmt.Fprintf(os.Stdout, "  %s", bindingStyle.Render(binding))
		}
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, descStyle.Render(r.Description))
		fmt.Fprintln(os.Stdout, sourceStyle.Render(fmt.Sprintf("%s:%d", r.SourceFile, r.SourceLine)))
		fmt.Fprintln(os.Stdout)
	}

	fmt.Fprintf(os.Stdout, "  %d result(s)\n", len(results))
	return nil
}
