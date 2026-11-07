package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/ui"
)

func columns(width int) []table.Column {
	const (
		nameWidth     = 18
		protocolWidth = 10
		modelsWidth   = 6
		keyWidth      = 9
		testWidth     = 10
		frame         = 2 * 6
	)
	baseWidth := max(width-(nameWidth+protocolWidth+modelsWidth+keyWidth+testWidth+frame), 16)
	return []table.Column{
		{Title: "Name", Width: nameWidth},
		{Title: "Protocol", Width: protocolWidth},
		{Title: "Base URL", Width: baseWidth},
		{Title: "Models", Width: modelsWidth},
		{Title: "Key", Width: keyWidth},
		{Title: "Test", Width: testWidth},
	}
}

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Selected = ui.Accent.Bold(true)
	return styles
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.headerView(),
		"",
		m.bodyView(),
		"",
		m.toastView(),
		m.helpView(),
	)
}

func (m Model) headerView() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.titleView(),
		m.accountView(),
		m.daemonView(),
	)
}

func (m Model) titleView() string {
	version := m.opts.Version
	if version == "" {
		version = "dev"
	}
	return m.fit(ui.Bold.Render("noket "+version) + ui.Faint.Render("  ·  tui"))
}

func (m Model) accountView() string {
	if m.viewErr != nil {
		return m.fit(ui.Err.Render("config: " + m.viewErr.Error()))
	}
	if m.view.CfgErr != nil {
		return m.fit(ui.Err.Render("config: " + m.view.CfgErr.Error()))
	}
	if m.view.CredsErr != nil || m.view.Creds == nil {
		if m.view.CredsErr != nil && !errors.Is(m.view.CredsErr, config.ErrNoCredentials) {
			return m.fit(ui.Err.Render("account: " + m.view.CredsErr.Error()))
		}
		return m.fit(ui.Warn.Render("not logged in  ·  run `noket login`"))
	}

	user := m.view.Creds.UserID
	if user == "" {
		user = "unknown user"
	}
	node := m.view.NodeID
	if node == "" {
		node = "no node yet"
	}
	return m.fit(ui.Faint.Render("user ") + user + ui.Faint.Render("  ·  node ") + node)
}

func (m Model) daemonView() string {
	if m.viewErr != nil {
		return m.fit(ui.Faint.Render("noketd: unknown"))
	}

	status := m.view.Daemon
	line := ""
	switch {
	case status == nil:
		line = ui.Faint.Render("noketd: not running")
	case !m.view.Alive:
		line = ui.Faint.Render(fmt.Sprintf("noketd: not running (stale status for pid %d)", status.PID))
	default:
		state := ui.Warn.Render("starting")
		switch {
		case status.Connected:
			state = ui.OK.Render("connected")
		case status.Registered:
			state = ui.Warn.Render("registered")
		}
		line = "noketd: " + state + ui.Faint.Render(fmt.Sprintf(" (pid %d)", status.PID))
		if status.NodeName != "" {
			line += ui.Faint.Render("  ·  ") + status.NodeName
		}
	}
	if status != nil && status.LastError != "" {
		line += ui.Faint.Render("  ·  last error: ") + ui.Err.Render(status.LastError)
	}
	if m.logPath != "" {
		line += ui.Faint.Render("  ·  log ") + ui.Faint.Render(m.logPath)
	}
	return m.fit(line)
}

// bodyHeight is the room the table or a popup gets: the terminal minus the
// three header lines, two blank separators, the toast line and the help line.
func (m Model) bodyHeight() int { return max(3, m.height-7) }

func (m Model) bodyView() string {
	body := padLines(m.browseView(), m.bodyHeight())
	if m.modal.kind == modalNone {
		return body
	}
	if m.modal.form == nil && m.modal.kind != modalDetail {
		return body
	}
	return overlay(body, m.modalView(), m.width)
}

func (m Model) browseView() string {
	if len(m.rows) == 0 {
		if m.viewErr != nil || m.view.CfgErr != nil {
			return ui.Err.Render(m.configProblem())
		}
		return ui.Faint.Render("No upstreams yet. Press a to add one.")
	}
	return m.table.View()
}

// modalView frames the open form in a popup that sits in the middle of the
// body, over the table.
func (m Model) modalView() string {
	var content string
	switch {
	case m.modal.kind == modalDetail:
		content = m.detailContent()
	case m.busy.kind != busyNone:
		// A submitted huh form renders nothing once it is completed, so the
		// popup would flash empty while the probe or the write runs.
		content = m.spin.View() + " " + m.busy.detail + "…"
	default:
		content = m.modal.form.View()
	}
	title := ui.Accent.Bold(true).Render(m.modalTitle())
	return ui.Popup.Width(modalWidth(m.width) - 2).Render(title + "\n\n" + content)
}

// detailContent is the read-only body of the details popup: the upstream's
// fields, its models (a few, then a count), and this session's test result.
// Header values stay hidden; only their names are shown.
func (m Model) detailContent() string {
	up := m.modal.expected
	if up == nil {
		return ""
	}

	const labelWidth = 10
	valueWidth := max(formWidth(m.width)-labelWidth, 10)
	value := func(text string) string { return ansi.Truncate(text, valueWidth, "…") }
	row := func(name, text string) string {
		return ui.Faint.Render(fmt.Sprintf("%-*s", labelWidth, name)) + text
	}
	indent := strings.Repeat(" ", labelWidth)

	key := ui.Faint.Render("not set")
	if up.HasAPIKey() {
		key = ui.OK.Render("set")
	}

	lines := []string{
		row("Name", value(up.Name)),
		row("Protocol", up.Protocol),
		row("Base URL", value(up.BaseURL)),
		row("API key", key),
		row("Models", strconv.Itoa(len(up.Models))),
	}

	const maxShown = 8
	shown := up.Models
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}
	for _, model := range shown {
		lines = append(lines, indent+value(model))
	}
	if rest := len(up.Models) - len(shown); rest > 0 {
		lines = append(lines, indent+ui.Faint.Render(fmt.Sprintf("… and %d more", rest)))
	}

	if len(up.Headers) > 0 {
		names := make([]string, 0, len(up.Headers))
		for name := range up.Headers {
			names = append(names, name)
		}
		sort.Strings(names)
		lines = append(lines, row("Headers", value(strings.Join(names, ", "))))
	}

	switch result, tested := m.results[resultKey(up.Name, up.BaseURL)]; {
	case !tested:
		lines = append(lines, row("Test", "-"))
	case result.err != nil:
		lines = append(lines, row("Test", ui.Err.Render("failed")))
		lines = append(lines, indent+ui.Err.Render(value(result.err.Error())))
	default:
		lines = append(lines, row("Test", ui.OK.Render(result.cell())))
	}
	return strings.Join(lines, "\n")
}

func (m Model) modalTitle() string {
	switch m.modal.kind {
	case modalDetail:
		return "Upstream"
	case modalFields:
		if m.modal.editing {
			return "Edit upstream"
		}
		return "New upstream"
	case modalModels, modalManualModels:
		if m.modal.up.Name != "" {
			return fmt.Sprintf("Models for %q", m.modal.up.Name)
		}
		return "Models to expose"
	case modalRecovery:
		return "Model list unavailable"
	case modalConfirmRemove:
		return "Remove upstream"
	case modalConfirmStop:
		return "Stop noketd"
	}
	return ""
}

// padLines makes text occupy exactly height lines so the body keeps a fixed
// size and anything drawn over it can be centred.
func padLines(text string, height int) string {
	lines := strings.Split(text, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// overlay replaces the middle of body with box, centred horizontally and
// vertically. The rows the box covers are rebuilt in full, so nothing of the
// table shows through the frame.
func overlay(body, box string, width int) string {
	bodyLines := strings.Split(body, "\n")
	boxLines := strings.Split(box, "\n")

	boxWidth := 0
	for _, line := range boxLines {
		boxWidth = max(boxWidth, lipgloss.Width(line))
	}
	boxWidth = min(boxWidth, width)
	left := max((width-boxWidth)/2, 0)
	top := max((len(bodyLines)-len(boxLines))/2, 0)

	for len(bodyLines) < top+len(boxLines) {
		bodyLines = append(bodyLines, "")
	}
	for i, line := range boxLines {
		line = ansi.Truncate(line, boxWidth, "")
		padding := strings.Repeat(" ", max(width-left-lipgloss.Width(line), 0))
		bodyLines[top+i] = strings.Repeat(" ", left) + line + padding
	}
	return strings.Join(bodyLines, "\n")
}

func (m Model) toastView() string {
	if m.busy.kind != busyNone {
		return m.spin.View() + " " + ui.Faint.Render(m.busy.detail)
	}
	if m.toast.text == "" || time.Now().After(m.toast.until) {
		return ""
	}
	return m.fit(toastStyle(m.toast.tone).Render(m.toast.text))
}

func (m Model) helpView() string {
	if m.modal.kind == modalNone {
		return m.help.View(m.keys)
	}
	if m.modal.kind == modalDetail {
		return m.help.View(helpBindings(detailBindings()))
	}
	if m.modal.kind == modalModels {
		return m.help.View(helpBindings(modelBindings()))
	}
	action := m.keys.Next
	switch m.modal.kind {
	case modalConfirmRemove, modalConfirmStop, modalRecovery:
		action = m.keys.Confirm
	}
	return m.help.View(helpBindings{action, m.keys.Cancel})
}

func (m Model) fit(line string) string {
	if m.width <= 0 {
		return line
	}
	return ansi.Truncate(line, m.width, "…")
}

func toastStyle(tone toastTone) lipgloss.Style {
	switch tone {
	case toneOK:
		return ui.OK
	case toneWarn:
		return ui.Warn
	case toneErr:
		return ui.Err
	default:
		return ui.Faint
	}
}
