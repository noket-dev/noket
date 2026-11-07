package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/noketd"
)

// newNoketdCmd is the hidden daemon command. `noket up` starts it in the
// background when no standalone noketd binary sits next to noket.
func newNoketdCmd(version string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:    "noketd",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNoketd(cmd, name, version, true)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "")
	return cmd
}

// runNoketd serves until the process receives SIGINT or SIGTERM. managed is
// false for `noket up --foreground`, where an external supervisor owns the
// process and no pid file is written.
func runNoketd(cmd *cobra.Command, name, version string, managed bool) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return noketd.Run(ctx, noketd.RunOptions{Name: name, Managed: managed, Version: version, Logger: newLogger(cmd)})
}
