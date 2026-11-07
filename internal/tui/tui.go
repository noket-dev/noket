// Package tui is the full-screen console behind `noket tui`. It edits the
// upstream list and drives the local noketd, using the same service packages
// as the subcommands so both surfaces apply the same rules.
package tui

import (
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// Options carries the command's streams and settings into the program.
type Options struct {
	In      io.Reader
	Out     io.Writer
	Version string
	Verbose bool
}

// Run starts the full-screen console and blocks until it exits.
func Run(opts Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	program := tea.NewProgram(
		newModel(ctx, cancel, opts),
		tea.WithInput(opts.In),
		tea.WithOutput(opts.Out),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}
