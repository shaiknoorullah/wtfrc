package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/shaiknoorullah/wtfrc/internal/session"
	"github.com/shaiknoorullah/wtfrc/internal/supervisor"
)

var superviseCmd = &cobra.Command{
	Use:   "supervise",
	Short: "Run supervisor review for hallucination detection",
	Long: `Run the two-tier hallucination-detection pipeline over recent ask
sessions. The supervisor checks answers against the knowledge base
entries and optionally cross-checks with a strong LLM.

Use --report to show the last saved supervisor report instead of
running a new review.`,
	RunE: runSupervise,
}

var superviseReport bool

func init() {
	superviseCmd.Flags().BoolVar(&superviseReport, "report", false, "Show last supervisor report instead of running a new review")
	rootCmd.AddCommand(superviseCmd)
}

func runSupervise(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	if superviseReport {
		return showLastReport(d)
	}

	sessMgr := session.NewManager(d.DB)
	sup := supervisor.New(d.DB, d.StrongLLM, sessMgr)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc supervise"))
	fmt.Fprintln(os.Stdout, "  Running supervisor review...")
	fmt.Fprintln(os.Stdout)

	ctx := context.Background()
	report, err := sup.Review(ctx)
	if err != nil {
		return fmt.Errorf("supervisor review: %w", err)
	}

	fmt.Println(supervisor.GenerateMarkdown(report))
	return nil
}

func showLastReport(d *deps) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

	var runAt, modelUsed string
	var sessionsReviewed, issuesFound int
	var optApplied *string

	err := d.DB.Conn().QueryRow(
		`SELECT run_at, sessions_reviewed, issues_found, optimizations_applied, model_used
		 FROM supervisor_runs ORDER BY run_at DESC LIMIT 1`,
	).Scan(&runAt, &sessionsReviewed, &issuesFound, &optApplied, &modelUsed)
	if err != nil {
		return fmt.Errorf("no supervisor reports found (run 'wtfrc supervise' first)")
	}

	fmt.Fprintln(os.Stdout, headerStyle.Render("Last Supervisor Report"))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  Run at:             %s\n", runAt)
	fmt.Fprintf(os.Stdout, "  Sessions reviewed:  %d\n", sessionsReviewed)
	fmt.Fprintf(os.Stdout, "  Issues found:       %d\n", issuesFound)
	fmt.Fprintf(os.Stdout, "  Model used:         %s\n", modelUsed)
	if optApplied != nil && *optApplied != "" {
		fmt.Fprintf(os.Stdout, "  Flagged queries:    %s\n", *optApplied)
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, dimStyle.Render("  Run 'wtfrc supervise' (without --report) for a fresh review."))
	return nil
}
