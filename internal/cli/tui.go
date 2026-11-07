package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/tui"
)

func newTuiCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Manage upstreams and noketd in a full-screen terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !canUseForm(cmd) {
				return errors.New("noket tui needs an interactive terminal; use the noket subcommands instead")
			}
			return tui.Run(tui.Options{
				In:      cmd.InOrStdin(),
				Out:     cmd.OutOrStdout(),
				Version: version,
				Verbose: verboseFlag(cmd),
			})
		},
	}
}
