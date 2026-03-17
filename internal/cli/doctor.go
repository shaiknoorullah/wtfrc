package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks on your wtfrc setup",
	Long: `Verify that all wtfrc components are working correctly:
  - Config file is valid
  - Database is accessible
  - Ollama is running (fast LLM)
  - Strong LLM is reachable`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	passStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	labelStyle := lipgloss.NewStyle().Width(30)

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc doctor"))
	fmt.Fprintln(os.Stdout)

	allOK := true

	check := func(name string, fn func() error) {
		label := labelStyle.Render(name)
		if err := fn(); err != nil {
			fmt.Fprintf(os.Stdout, "  %s %s  %v\n", label, failStyle.Render("FAIL"), err)
			allOK = false
		} else {
			fmt.Fprintf(os.Stdout, "  %s %s\n", label, passStyle.Render("OK"))
		}
	}

	// Check 1: Config valid
	check("Config valid", func() error {
		cfgFile, err := configPath()
		if err != nil {
			return err
		}
		if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
			return fmt.Errorf("config file not found at %s (using defaults)", cfgFile)
		}
		return nil
	})

	// Check 2: DB accessible
	check("Database accessible", func() error {
		dbFile, err := dbPath()
		if err != nil {
			return err
		}
		d, err := newDeps()
		if err != nil {
			return err
		}
		defer d.DB.Close()
		_ = dbFile
		// Verify we can query
		var count int
		return d.DB.Conn().QueryRow("SELECT COUNT(*) FROM entries").Scan(&count)
	})

	// Check 3: Fast LLM (Ollama) reachable
	check("Fast LLM reachable", func() error {
		d, err := newDeps()
		if err != nil {
			return err
		}
		defer d.DB.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return newProvider(d.Cfg.LLM.Fast).HealthCheck(ctx)
	})

	// Check 4: Strong LLM reachable
	check("Strong LLM reachable", func() error {
		d, err := newDeps()
		if err != nil {
			return err
		}
		defer d.DB.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return newProvider(d.Cfg.LLM.Strong).HealthCheck(ctx)
	})

	fmt.Fprintln(os.Stdout)
	if allOK {
		fmt.Fprintln(os.Stdout, passStyle.Render("All checks passed."))
	} else {
		fmt.Fprintln(os.Stdout, failStyle.Render("Some checks failed. See above for details."))
	}

	return nil
}
