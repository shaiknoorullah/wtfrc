package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type Input struct {
	textInput textinput.Model
	history   []string
	histIdx   int // -1 means not browsing history
	saved     string
}

func NewInput() Input {
	ti := textinput.New()
	ti.Placeholder = "Ask about your config..."
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 60

	return Input{
		textInput: ti,
		histIdx:   -1,
	}
}

func (i *Input) Init() tea.Cmd {
	return textinput.Blink
}

func (i *Input) Update(msg tea.Msg) (Input, tea.Cmd) {
	var cmd tea.Cmd
	i.textInput, cmd = i.textInput.Update(msg)
	return *i, cmd
}

func (i *Input) View() string {
	return i.textInput.View()
}

func (i *Input) Value() string {
	return i.textInput.Value()
}

func (i *Input) SetValue(s string) {
	i.textInput.SetValue(s)
}

func (i *Input) Focus() tea.Cmd {
	return i.textInput.Focus()
}

func (i *Input) Blur() {
	i.textInput.Blur()
}

func (i *Input) AddHistory(s string) {
	if s == "" {
		return
	}
	i.history = append(i.history, s)
	i.histIdx = -1
}

func (i *Input) HistoryUp() {
	if len(i.history) == 0 {
		return
	}
	if i.histIdx == -1 {
		i.saved = i.textInput.Value()
		i.histIdx = len(i.history) - 1
	} else if i.histIdx > 0 {
		i.histIdx--
	}
	i.textInput.SetValue(i.history[i.histIdx])
}

func (i *Input) HistoryDown() {
	if i.histIdx == -1 {
		return
	}
	if i.histIdx < len(i.history)-1 {
		i.histIdx++
		i.textInput.SetValue(i.history[i.histIdx])
	} else {
		i.histIdx = -1
		i.textInput.SetValue(i.saved)
	}
}
