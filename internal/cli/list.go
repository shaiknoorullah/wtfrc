package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all entries in the knowledge base",
	Long: `Query all entries from the knowledge base and display them in a
formatted table. Use --tool to filter by a specific tool name.`,
	RunE: runList,
}

var listTool string

func init() {
	listCmd.Flags().StringVar(&listTool, "tool", "", "Filter entries by tool name")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	query := `SELECT id, tool, type, raw_binding, description, source_file, source_line FROM entries`
	var queryArgs []any
	if listTool != "" {
		query += " WHERE tool = ?"
		queryArgs = append(queryArgs, listTool)
	}
	query += " ORDER BY tool, id"

	rows, err := d.DB.Conn().Query(query, queryArgs...)
	if err != nil {
		return fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	toolStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575")).Width(14)
	typeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Width(12)
	bindingColStyle := lipgloss.NewStyle().Width(24)
	descColStyle := lipgloss.NewStyle().Width(40)
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Italic(true)

	title := "Knowledge Base Entries"
	if listTool != "" {
		title = fmt.Sprintf("Knowledge Base Entries (tool: %s)", listTool)
	}
	fmt.Fprintln(os.Stdout, headerStyle.Render(title))
	fmt.Fprintln(os.Stdout)

	// Table header
	hdr := fmt.Sprintf("  %s %s %s %s %s",
		lipgloss.NewStyle().Bold(true).Width(14).Render("TOOL"),
		lipgloss.NewStyle().Bold(true).Width(12).Render("TYPE"),
		lipgloss.NewStyle().Bold(true).Width(24).Render("BINDING"),
		lipgloss.NewStyle().Bold(true).Width(40).Render("DESCRIPTION"),
		lipgloss.NewStyle().Bold(true).Render("SOURCE"),
	)
	fmt.Fprintln(os.Stdout, hdr)
	fmt.Fprintln(os.Stdout, "  "+strings.Repeat("-", 100))

	count := 0
	for rows.Next() {
		var id int64
		var tool, typ, description, sourceFile string
		var rawBinding *string
		var sourceLine int

		if err := rows.Scan(&id, &tool, &typ, &rawBinding, &description, &sourceFile, &sourceLine); err != nil {
			return fmt.Errorf("scan entry: %w", err)
		}

		binding := ""
		if rawBinding != nil {
			binding = *rawBinding
		}

		// Truncate long descriptions
		desc := description
		if len(desc) > 38 {
			desc = desc[:35] + "..."
		}

		fmt.Fprintf(os.Stdout, "  %s %s %s %s %s\n",
			toolStyle.Render(tool),
			typeStyle.Render(typ),
			bindingColStyle.Render(binding),
			descColStyle.Render(desc),
			sourceStyle.Render(fmt.Sprintf("%s:%d", sourceFile, sourceLine)),
		)
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate entries: %w", err)
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %d entry(ies)\n", count)
	return nil
}
