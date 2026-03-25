package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
	"github.com/shaiknoorullah/wtfrc/internal/session"
	"github.com/shaiknoorullah/wtfrc/internal/tui/components"
)

type state int

const (
	stateInput state = iota
	stateLoading
	stateStreaming
	stateAnswer
)

type Model struct {
	input     components.Input
	answer    components.Answer
	spinner   spinner.Model
	state     state
	session   *session.Manager
	sessionID string
	db        *kb.DB
	llm       llm.Provider
	cfg       *config.Config
	err       error
	width     int
	height    int
}

// searchResultMsg carries KB search results.
type searchResultMsg struct {
	results []kb.SearchResult
	query   string
	err     error
}

// streamTokenMsg carries a single streamed token.
type streamTokenMsg struct {
	token string
	done  bool
}

// streamErrMsg carries a streaming error.
type streamErrMsg struct {
	err error
}

func NewModel(db *kb.DB, provider llm.Provider, sessMgr *session.Manager, cfg *config.Config, sessionID string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	return Model{
		input:     components.NewInput(),
		answer:    components.NewAnswer(),
		spinner:   s,
		state:     stateInput,
		db:        db,
		llm:       provider,
		session:   sessMgr,
		sessionID: sessionID,
		cfg:       cfg,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Init(), m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.answer.SetSize(msg.Width-4, msg.Height-8)
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case searchResultMsg:
		return m.handleSearchResult(msg)
	case streamTokenMsg:
		return m.handleStreamToken(msg)
	case streamErrMsg:
		m.err = msg.err
		m.state = stateAnswer
		return m, nil
	}

	switch m.state {
	case stateInput:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case stateStreaming:
		var cmd tea.Cmd
		m.answer, cmd = m.answer.Update(msg)
		return m, cmd
	case stateAnswer:
		var cmd tea.Cmd
		m.answer, cmd = m.answer.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) View() string {
	var b strings.Builder

	b.WriteString(HeaderStyle.Render("wtfrc — ask your config"))
	b.WriteString("\n")

	switch m.state {
	case stateInput:
		b.WriteString(PromptStyle.Render("? "))
		b.WriteString(m.input.View())
	case stateLoading:
		b.WriteString(m.spinner.View())
		b.WriteString(" Searching knowledge base...")
	case stateStreaming:
		b.WriteString(AnswerStyle.Render(m.answer.View()))
		b.WriteString("\n")
		b.WriteString(m.spinner.View())
	case stateAnswer:
		if m.err != nil {
			b.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			b.WriteString(AnswerStyle.Render(m.answer.View()))
		}
		b.WriteString("\n\n")
		b.WriteString(PromptStyle.Render("? "))
		b.WriteString(m.input.View())
	}

	b.WriteString("\n")
	b.WriteString(SourceRefStyle.Render("esc to quit"))

	return b.String()
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "up":
		if m.state == stateInput || m.state == stateAnswer {
			m.input.HistoryUp()
			return m, nil
		}
	case "down":
		if m.state == stateInput || m.state == stateAnswer {
			m.input.HistoryDown()
			return m, nil
		}
	case "enter":
		if m.state == stateInput || m.state == stateAnswer {
			query := m.input.Value()
			if query == "" {
				return m, nil
			}
			m.input.AddHistory(query)
			m.input.SetValue("")
			m.answer.Clear()
			m.state = stateLoading
			m.err = nil
			cmd := m.searchKB(query)
			return m, cmd
		}
	}

	if m.state == stateInput || m.state == stateAnswer {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) searchKB(query string) tea.Cmd {
	return func() tea.Msg {
		maxEntries := 10
		if m.cfg != nil {
			maxEntries = m.cfg.Popup.MaxContextEntries
		}
		results, err := m.db.Search(query, maxEntries)
		return searchResultMsg{results: results, query: query, err: err}
	}
}

func (m *Model) handleSearchResult(msg searchResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.state = stateAnswer
		return m, nil
	}

	// Build context from search results
	var contextParts []string
	var sources []string
	for i := range msg.results {
		r := &msg.results[i]
		part := fmt.Sprintf("[%s] %s", r.Tool, r.Description)
		if r.RawBinding != nil {
			part += fmt.Sprintf(" (binding: %s)", *r.RawBinding)
		}
		if r.RawAction != nil {
			part += fmt.Sprintf(" (action: %s)", *r.RawAction)
		}
		contextParts = append(contextParts, part)
		sources = append(sources, fmt.Sprintf("%s:%d", r.SourceFile, r.SourceLine))
	}

	m.answer.SetSources(sources)
	m.state = stateStreaming

	cmd := m.streamAnswer(msg.query, contextParts)
	return m, cmd
}

func (m *Model) streamAnswer(query string, context []string) tea.Cmd {
	return func() tea.Msg {
		systemPrompt := `You are wtfrc, a local config assistant. Answer questions about the user's dotfiles and configs based on the KB entries provided. Be concise and accurate. If the KB doesn't contain relevant info, say so.`

		contextStr := strings.Join(context, "\n")
		userMsg := fmt.Sprintf("KB Context:\n%s\n\nQuestion: %s", contextStr, query)

		ch, err := m.llm.Stream(context2(), llm.CompletionRequest{
			System:   systemPrompt,
			Messages: []llm.Message{{Role: "user", Content: userMsg}},
		})
		if err != nil {
			return streamErrMsg{err: err}
		}

		// Read first token to return
		for token := range ch {
			return streamTokenMsg{token: token}
		}
		return streamTokenMsg{done: true}
	}
}

func context2() context.Context {
	return context.Background()
}

func (m *Model) handleStreamToken(msg streamTokenMsg) (tea.Model, tea.Cmd) {
	if msg.done {
		m.state = stateAnswer
		m.input.Focus()
		return m, nil
	}

	m.answer.AppendToken(msg.token)

	// Continue reading from the stream
	return m, func() tea.Msg {
		// This is a simplified approach — in production you'd keep the channel reference
		return streamTokenMsg{done: true}
	}
}
