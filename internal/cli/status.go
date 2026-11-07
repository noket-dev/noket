package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/state"
	"github.com/noket-dev/noket/internal/types"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the logged-in user, node, and connection status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := state.Read()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			renderStatus(out, buildStatus(in), interactiveOutput(out))
			return nil
		},
	}
}

func buildStatus(in state.View) []statusSection {
	return []statusSection{
		{title: "Config", rows: []statusRow{{label: "Directory", value: in.Dir}}},
		{title: "Account", rows: accountRows(in.Creds, in.CredsErr)},
		{title: "Node", rows: nodeRows(in.NodeID, in.CredsErr)},
		{title: "noketd", rows: daemonRows(in)},
	}
}

func accountRows(creds *types.Credentials, credsErr error) []statusRow {
	switch {
	case errors.Is(credsErr, config.ErrNoCredentials), creds == nil && credsErr == nil:
		return []statusRow{{label: "State", value: "not logged in (run `noket login`)", tone: toneWarn}}
	case credsErr != nil:
		return []statusRow{{label: "State", value: "failed to read: " + credsErr.Error(), tone: toneErr}}
	}

	user := creds.UserID
	if user == "" {
		user = "unknown user"
	}
	rows := []statusRow{{label: "User", value: user}}
	if !creds.SavedAt.IsZero() {
		rows = append(rows, statusRow{
			label: "Saved",
			value: creds.SavedAt.Local().Format("2006-01-02 15:04"),
			tone:  toneDim,
		})
	}
	return rows
}

func nodeRows(nodeID string, credsErr error) []statusRow {
	switch {
	case credsErr != nil:
		return []statusRow{{label: "ID", value: "available after login", tone: toneDim}}
	case nodeID == "":
		return []statusRow{{label: "ID", value: "not created yet (generated on first `noket up`)", tone: toneDim}}
	default:
		return []statusRow{{label: "ID", value: nodeID}}
	}
}

func daemonRows(in state.View) []statusRow {
	status := in.Daemon
	switch {
	case status == nil:
		return []statusRow{{label: "State", value: "not running", tone: toneDim}}
	case !in.Alive:
		rows := []statusRow{{
			label: "State",
			value: fmt.Sprintf("not running (stale status, pid %d)", status.PID),
			tone:  toneDim,
		}}
		return appendLastError(rows, status.LastError)
	case !in.DaemonProcess:
		rows := []statusRow{{
			label: "State",
			value: fmt.Sprintf("running in the foreground (pid %d); stop it with your supervisor", status.PID),
			tone:  toneWarn,
		}}
		return appendLastError(rows, status.LastError)
	}

	state := "disconnected"
	tone := toneWarn
	switch {
	case status.Registered:
		state, tone = "connected", toneOK
	case status.Connected:
		state = "connected, registering"
	}
	rows := []statusRow{{
		label: "State",
		value: fmt.Sprintf("%s (pid %d)", state, status.PID),
		tone:  tone,
	}}
	if status.NodeName != "" {
		rows = append(rows, statusRow{label: "Name", value: status.NodeName})
	}
	if !status.StartedAt.IsZero() {
		rows = append(rows, statusRow{
			label: "Started",
			value: status.StartedAt.Local().Format("2006-01-02 15:04"),
			tone:  toneDim,
		})
	}
	return appendLastError(rows, status.LastError)
}

func appendLastError(rows []statusRow, lastError string) []statusRow {
	if lastError == "" {
		return rows
	}
	return append(rows, statusRow{label: "Last error", value: lastError, tone: toneErr})
}
