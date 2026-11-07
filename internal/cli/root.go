package cli

import (
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// New builds the noket command tree.
func New(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "noket",
		Short:         "Connect local LLM APIs to the Noket tunnel",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.PersistentFlags().BoolP("verbose", "v", false, "enable debug logging")

	root.AddCommand(
		newLoginCmd(),
		newLogoutCmd(),
		newStatusCmd(),
		newUpCmd(version),
		newDownCmd(),
		newUpstreamCmd(),
		newNoketdCmd(version),
		newTuiCmd(version),
	)
	return root
}

func newLogger(cmd *cobra.Command) zerolog.Logger {
	level := zerolog.InfoLevel
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		level = zerolog.DebugLevel
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).
		Level(level).
		With().
		Timestamp().
		Logger()
}
