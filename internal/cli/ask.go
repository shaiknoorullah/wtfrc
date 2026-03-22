package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/shaiknoorullah/wtfrc/internal/llm"
	"github.com/shaiknoorullah/wtfrc/internal/session"
	"github.com/shaiknoorullah/wtfrc/internal/tui"
)

var askCmd = &cobra.Command{
	Use:   "ask [query]",
	Short: "Ask questions about your config setup",
	Long: `Ask a natural-language question about your dotfiles and configs.

If a query is provided as a positional argument, runs in one-shot mode:
searches the KB, queries the LLM, prints the answer, and exits.

If no argument is given, launches an interactive Bubble Tea REPL.`,
	RunE: runAsk,
}

func init() {
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	sessMgr := session.NewManager(d.DB)
	sess, err := sessMgr.StartSession(d.FastLLM.Name())
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer func() { _ = sessMgr.EndSession(sess.ID) }()

	if len(args) > 0 {
		query := strings.Join(args, " ")
		return askOneShot(d, sessMgr, sess.ID, query)
	}

	// Interactive REPL mode
	model := tui.NewModel(d.DB, d.FastLLM, sessMgr, d.Cfg, sess.ID)
	p := tea.NewProgram(&model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func askOneShot(d *deps, sessMgr *session.Manager, sessionID, query string) error {
	maxEntries := 10
	if d.Cfg != nil {
		maxEntries = d.Cfg.Popup.MaxContextEntries
	}

	results, err := d.DB.Search(query, maxEntries)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	var contextParts []string
	for i := range results {
		r := &results[i]
		part := fmt.Sprintf("[%s] %s", r.Tool, r.Description)
		if r.RawBinding != nil {
			part += fmt.Sprintf(" (binding: %s)", *r.RawBinding)
		}
		if r.RawAction != nil {
			part += fmt.Sprintf(" (action: %s)", *r.RawAction)
		}
		contextParts = append(contextParts, part)
	}

	systemPrompt := `You are wtfrc, a local config assistant. Answer questions about the user's dotfiles and configs based on the KB entries provided. Be concise and accurate. If the KB doesn't contain relevant info, say so.`

	contextStr := strings.Join(contextParts, "\n")
	userMsg := fmt.Sprintf("KB Context:\n%s\n\nQuestion: %s", contextStr, query)

	resp, err := d.FastLLM.Complete(context.Background(), llm.CompletionRequest{
		System:   systemPrompt,
		Messages: []llm.Message{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4"))
	answerStyle := lipgloss.NewStyle().PaddingLeft(2)
	sourceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		Italic(true)

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc"))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, answerStyle.Render(resp.Content))
	fmt.Fprintln(os.Stdout)

	if len(results) > 0 {
		var sources []string
		for i := range results {
			sources = append(sources, fmt.Sprintf("  %s:%d", results[i].SourceFile, results[i].SourceLine))
		}
		fmt.Fprintln(os.Stdout, sourceStyle.Render("Sources:"))
		for _, s := range sources {
			fmt.Fprintln(os.Stdout, sourceStyle.Render(s))
		}
	}

	return nil
}
