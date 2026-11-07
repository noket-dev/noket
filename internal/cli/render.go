package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/noket-dev/noket/internal/ui"
)

// Shared styles for every command that prints to a terminal. The palette
// itself lives in internal/ui so the TUI renders the same colours.
var (
	styleBold   = ui.Bold
	styleOK     = ui.OK
	styleWarn   = ui.Warn
	styleErr    = ui.Err
	styleFaint  = ui.Faint
	styleAccent = ui.Accent
)

// interactiveOutput reports whether w can take styling at all: it has to be a
// terminal, and the user must not have switched colour off. Piped or redirected
// output always gets plain text so scripts keep working.
func interactiveOutput(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return false
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	return !strings.EqualFold(os.Getenv("TERM"), "dumb")
}

type statusTone int

const (
	tonePlain statusTone = iota
	toneOK
	toneWarn
	toneErr
	toneDim
)

type statusRow struct {
	label string
	value string
	tone  statusTone
}

type statusSection struct {
	title string
	rows  []statusRow
}

func styleForTone(tone statusTone) lipgloss.Style {
	switch tone {
	case toneOK:
		return styleOK
	case toneWarn:
		return styleWarn
	case toneErr:
		return styleErr
	case toneDim:
		return styleFaint
	default:
		return lipgloss.NewStyle()
	}
}

// renderStatus prints the sections with their labels aligned, styling them only
// when the writer is a terminal.
func renderStatus(out io.Writer, sections []statusSection, styled bool) {
	for i, section := range sections {
		if i > 0 {
			fmt.Fprintln(out)
		}
		title := section.title
		if styled {
			title = styleBold.Render(title)
		}
		fmt.Fprintln(out, title)

		width := 0
		for _, row := range section.rows {
			width = max(width, len(row.label))
		}
		for _, row := range section.rows {
			label := fmt.Sprintf("  %-*s", width, row.label)
			value := row.value
			if styled {
				label = styleFaint.Render(label)
				value = styleForTone(row.tone).Render(value)
			}
			fmt.Fprintf(out, "%s  %s\n", label, value)
		}
	}
}
