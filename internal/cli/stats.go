package cli

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show knowledge base and usage statistics",
	Long: `Display entry counts by tool, session count, total queries,
and average response time.`,
	RunE: runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	labelStyle := lipgloss.NewStyle().Bold(true).Width(28)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD700")).PaddingTop(1)
	toolStyle := lipgloss.NewStyle().Width(20).PaddingLeft(4)

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc stats"))
	fmt.Fprintln(os.Stdout)

	// Entry counts by tool
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Entries by Tool"))
	rows, err := d.DB.Conn().Query("SELECT tool, COUNT(*) FROM entries GROUP BY tool ORDER BY COUNT(*) DESC")
	if err != nil {
		return fmt.Errorf("query entries by tool: %w", err)
	}
	defer rows.Close()

	totalEntries := 0
	for rows.Next() {
		var tool string
		var count int
		if err := rows.Scan(&tool, &count); err != nil {
			return fmt.Errorf("scan tool count: %w", err)
		}
		fmt.Fprintf(os.Stdout, "  %s %s\n", toolStyle.Render(tool), valueStyle.Render(fmt.Sprintf("%d", count)))
		totalEntries += count
	}
	rows.Close()
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Total entries:"), valueStyle.Render(fmt.Sprintf("%d", totalEntries)))

	// Session count
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Sessions"))
	var sessionCount int
	err = d.DB.Conn().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	if err != nil {
		return fmt.Errorf("query session count: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Total sessions:"), valueStyle.Render(fmt.Sprintf("%d", sessionCount)))

	// Total queries
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Queries"))
	var queryCount int
	err = d.DB.Conn().QueryRow("SELECT COUNT(*) FROM queries").Scan(&queryCount)
	if err != nil {
		return fmt.Errorf("query count: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Total queries:"), valueStyle.Render(fmt.Sprintf("%d", queryCount)))

	// Average response time
	var avgResponseTime sql.NullFloat64
	err = d.DB.Conn().QueryRow("SELECT AVG(response_time_ms) FROM queries WHERE response_time_ms > 0").Scan(&avgResponseTime)
	if err != nil {
		return fmt.Errorf("query avg response time: %w", err)
	}
	if avgResponseTime.Valid {
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Avg response time:"), valueStyle.Render(fmt.Sprintf("%.0f ms", avgResponseTime.Float64)))
	} else {
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Avg response time:"), valueStyle.Render("n/a"))
	}

	// Manifest (tracked files)
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Indexed Files"))
	var fileCount int
	err = d.DB.Conn().QueryRow("SELECT COUNT(*) FROM manifest").Scan(&fileCount)
	if err != nil {
		return fmt.Errorf("query manifest count: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Tracked files:"), valueStyle.Render(fmt.Sprintf("%d", fileCount)))

	return nil
}
