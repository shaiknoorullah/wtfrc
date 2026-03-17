package cli

import (
	"context"
	"fmt"

	"github.com/shaiknoorullah/wtfrc/internal/session"
	"github.com/shaiknoorullah/wtfrc/internal/supervisor"
	"github.com/spf13/cobra"
)

var superviseReport bool

var superviseCmd = &cobra.Command{
	Use:   "supervise",
	Short: "Run supervisor review for hallucination detection",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := newDeps()
		if err != nil {
			return err
		}
		defer d.DB.Close()

		sessMgr := session.NewManager(d.DB)
		sup := supervisor.New(d.DB, d.StrongLLM, sessMgr)

		ctx := context.Background()
		report, err := sup.Review(ctx)
		if err != nil {
			return fmt.Errorf("supervisor review: %w", err)
		}

		fmt.Println(supervisor.GenerateMarkdown(report))

		return nil
	},
}

func init() {
	superviseCmd.Flags().BoolVar(&superviseReport, "report", false, "Show last report")
	rootCmd.AddCommand(superviseCmd)
}
