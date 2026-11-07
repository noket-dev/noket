package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/noketd"
	"github.com/noket-dev/noket/internal/types"
)

func newUpCmd(version string) *cobra.Command {
	var (
		foreground bool
		name       string
	)
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start noketd, connect to the tunnel, and register the node",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUp(cmd, foreground, name, version)
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in the foreground, write no pid file, leave supervision to systemd / Docker, and wait for `noket login` instead of exiting")
	cmd.Flags().StringVar(&name, "name", "", "override the node display name (does not create a second node)")
	return cmd
}

func runUp(cmd *cobra.Command, foreground bool, name, version string) (err error) {
	if pid, running := noketd.Running(); running {
		return fmt.Errorf("noketd is already running (pid %d); not starting a second instance", pid)
	}
	if _, err := config.LoadCredentials(); err != nil {
		if !errors.Is(err, config.ErrNoCredentials) {
			return err
		}
		// A supervised foreground process waits for the login inside noketd so
		// the supervisor does not restart it over and over; a detached daemon
		// fails now so `noket up` reports the missing login.
		if !foreground {
			return errors.New("not logged in, run noket login first")
		}
	}

	if foreground {
		return runNoketd(cmd, name, version, false)
	}

	progress := startProgress(cmd.ErrOrStderr(), "Starting noketd",
		"Starting the daemon", "Connecting to the tunnel", "Registering the node")
	defer func() { progress.Stop(err) }()

	detail := ""
	setDetail := func(text string) {
		if text == detail {
			return
		}
		detail = text
		progress.Detail(text)
	}

	proc, err := noketd.Start(noketd.StartOptions{Name: name, Verbose: verboseFlag(cmd)})
	if err != nil {
		return err
	}
	progress.Advance()

	connected := false
	status, err := noketd.AwaitRegistration(cmd.Context(), proc, noketd.StartupTimeout, func(status types.DaemonStatus) {
		setDetail(status.LastError)
		if status.Connected && !connected {
			connected = true
			progress.Advance()
		}
	})
	if err != nil {
		return err
	}

	progress.Stop(nil)
	printStarted(cmd, proc.PID, status.NodeID, status.NodeName, proc.LogPath)
	return nil
}

func verboseFlag(cmd *cobra.Command) bool {
	verbose, _ := cmd.Flags().GetBool("verbose")
	return verbose
}

func printStarted(cmd *cobra.Command, pid int, nodeID, name, logPath string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "noketd started (pid %d).\n", pid)
	if name != "" {
		fmt.Fprintf(out, "Node    %s (%s)\n", nodeID, name)
	} else {
		fmt.Fprintf(out, "Node    %s\n", nodeID)
	}
	fmt.Fprintf(out, "Log     %s\n", logPath)
}
