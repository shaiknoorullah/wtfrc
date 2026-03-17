package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

type Answer struct {
	viewport viewport.Model
	raw      strings.Builder
	sources  []string
	renderer *glamour.TermRenderer
	ready    bool
}

func NewAnswer() Answer {
	vp := viewport.New(80, 20)
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(76),
	)
	return Answer{
		viewport: vp,
		renderer: r,
	}
}

func (a Answer) Init() tea.Cmd {
	return nil
}

func (a Answer) Update(msg tea.Msg) (Answer, tea.Cmd) {
	var cmd tea.Cmd
	a.viewport, cmd = a.viewport.Update(msg)
	return a, cmd
}

func (a Answer) View() string {
	content := a.viewport.View()
	if len(a.sources) > 0 {
		content += "\n" + a.renderSources()
	}
	return content
}

func (a *Answer) AppendToken(token string) {
	a.raw.WriteString(token)
	a.render()
}

func (a *Answer) SetContent(content string) {
	a.raw.Reset()
	a.raw.WriteString(content)
	a.render()
}

func (a *Answer) SetSources(sources []string) {
	a.sources = sources
}

func (a *Answer) Clear() {
	a.raw.Reset()
	a.sources = nil
	a.viewport.SetContent("")
}

func (a *Answer) SetSize(width, height int) {
	a.viewport.Width = width
	a.viewport.Height = height
	a.ready = true
	a.render()
}

func (a *Answer) render() {
	if a.renderer == nil {
		a.viewport.SetContent(a.raw.String())
		return
	}
	rendered, err := a.renderer.Render(a.raw.String())
	if err != nil {
		a.viewport.SetContent(a.raw.String())
		return
	}
	a.viewport.SetContent(rendered)
	a.viewport.GotoBottom()
}

func (a *Answer) renderSources() string {
	var b strings.Builder
	b.WriteString("  Sources: ")
	for i, s := range a.sources {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s", s)
	}
	return b.String()
}
