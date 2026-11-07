package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

const (
	progressDoneMark = "✓"
	progressTodoMark = "○"
	progressFailMark = "✗"
)

// progressStepMsg marks the next step as the active one.
type progressStepMsg struct{}

type progressDetailMsg string

type progressDoneMsg struct{ err error }

type progressModel struct {
	spinner spinner.Model
	title   string
	steps   []string
	active  int
	detail  string
	start   time.Time
	err     error
	stopped bool
}

func newProgressModel(title string, steps []string) progressModel {
	return progressModel{
		spinner: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(styleAccent),
		),
		title: title,
		steps: steps,
		start: time.Now(),
	}
}

func (m progressModel) Init() tea.Cmd { return m.spinner.Tick }

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case progressStepMsg:
		if m.active < len(m.steps)-1 {
			m.active++
		}
		return m, nil
	case progressDetailMsg:
		m.detail = string(msg)
		return m, nil
	case progressDoneMsg:
		m.stopped = true
		m.err = msg.err
		return m, tea.Quit
	}
	return m, nil
}

// View is a header, the step list, and an optional detail line. It ends with a
// newline so the renderer's final line erase leaves the block in the terminal.
func (m progressModel) View() string {
	var b strings.Builder

	elapsed := styleFaint.Render(time.Since(m.start).Round(100 * time.Millisecond).String())
	fmt.Fprintf(&b, "%s  %s\n", m.header(), elapsed)
	for i, step := range m.steps {
		fmt.Fprintf(&b, "  %s\n", m.stepLine(i, step))
	}
	if m.detail != "" {
		fmt.Fprintf(&b, "  %s\n", styleFaint.Render(m.detail))
	}
	return b.String()
}

// header is the spinner or result mark and the title.
func (m progressModel) header() string {
	switch {
	case m.stopped && m.err != nil:
		return styleErr.Render(progressFailMark + " " + m.title)
	case m.stopped:
		return styleOK.Render(progressDoneMark + " " + m.title)
	default:
		return m.spinner.View() + " " + styleBold.Render(m.title)
	}
}

// stepLine is one step with the marker that matches its state.
func (m progressModel) stepLine(i int, step string) string {
	switch {
	case m.stopped && m.err == nil, i < m.active:
		return styleOK.Render(progressDoneMark + " " + step)
	case m.stopped && m.err != nil && i == m.active:
		return styleErr.Render(progressFailMark + " " + step)
	case i == m.active:
		return m.spinner.View() + " " + step
	default:
		return styleFaint.Render(progressTodoMark + " " + step)
	}
}

// progressUI shows a live progress block while a command waits. It renders only
// when out is a terminal, so piped and CI output keeps its plain form.
type progressUI struct {
	program *tea.Program
	stopped bool
}

func startProgress(out io.Writer, title string, steps ...string) *progressUI {
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return &progressUI{}
	}
	program := tea.NewProgram(
		newProgressModel(title, steps),
		tea.WithOutput(out),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)
	go func() { _, _ = program.Run() }()
	return &progressUI{program: program}
}

func (p *progressUI) enabled() bool { return p.program != nil }

// Advance makes the next step active. It is ignored on the last step.
func (p *progressUI) Advance() {
	if p.enabled() {
		p.program.Send(progressStepMsg{})
	}
}

// Detail replaces the line under the steps, for example the last reported error.
func (p *progressUI) Detail(text string) {
	if p.enabled() {
		p.program.Send(progressDetailMsg(text))
	}
}

// Stop renders the final frame and waits for the terminal to be restored. It is
// safe to call more than once.
func (p *progressUI) Stop(err error) {
	if !p.enabled() || p.stopped {
		return
	}
	p.stopped = true
	p.program.Send(progressDoneMsg{err: err})
	p.program.Wait()
}
