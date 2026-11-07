package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/noketd"
)

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the noketd started by noket up",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			pid, running := noketd.RunningPID()
			if !running {
				if status, ok := noketd.LiveStatus(); ok {
					fmt.Fprintf(out, "noketd is running in the foreground (pid %d); stop it with your supervisor. noket down only stops background mode.\n", status.PID)
					return nil
				}
				config.RemovePID()
				config.RemoveStatus()
				fmt.Fprintln(out, "noketd is not running.")
				return nil
			}

			progress := startProgress(cmd.ErrOrStderr(), fmt.Sprintf("Stopping noketd (pid %d)", pid),
				"Sending SIGTERM", "Waiting for the process to exit")

			if err := noketd.Terminate(pid); err != nil {
				err = fmt.Errorf("stop noketd (pid %d): %w", pid, err)
				progress.Stop(err)
				return err
			}
			progress.Advance()

			if err := noketd.WaitStopped(pid, noketd.ShutdownTimeout); err != nil {
				progress.Stop(err)
				return err
			}
			progress.Stop(nil)

			config.RemovePID()
			config.RemoveStatus()
			fmt.Fprintf(out, "noketd stopped (pid %d).\n", pid)
			return nil
		},
	}
}
