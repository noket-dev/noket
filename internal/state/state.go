// Package state reads everything the UI shows about the local install: the
// config, the login, the node id, and the daemon's published status.
package state

import (
	"errors"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/noketd"
	"github.com/noket-dev/noket/internal/types"
)

// View is one read of the local state. Config and credential problems are kept
// on the view so callers can render them; only a broken config directory stops
// the read.
type View struct {
	Dir string

	Cfg    *types.Config
	CfgErr error

	Creds    *types.Credentials
	CredsErr error

	NodeID string

	Daemon        *types.DaemonStatus
	Alive         bool
	DaemonProcess bool
}

// Read loads the config, credentials, node id, and daemon status.
func Read() (View, error) {
	var view View
	dir, err := config.Dir()
	if err != nil {
		return view, err
	}
	view.Dir = dir

	view.Cfg, view.CfgErr = config.Load()

	creds, credsErr := config.LoadCredentials()
	view.Creds, view.CredsErr = creds, credsErr
	if credsErr == nil {
		if node, err := config.ReadNode(config.Owner(creds)); err == nil {
			view.NodeID = node.ID
		}
	}

	status, err := config.ReadStatus()
	switch {
	case errors.Is(err, config.ErrNoStatus):
	case err != nil:
		return view, err
	default:
		view.Daemon = status
		view.Alive = noketd.ProcessAlive(status.PID)
		view.DaemonProcess = view.Alive && noketd.IsDaemonProcess(status.PID)
	}
	return view, nil
}
