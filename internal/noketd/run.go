package noketd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/noket-dev/noket/internal/config"
)

// RunOptions configures a noketd process.
type RunOptions struct {
	// Name overrides the node name reported to the tunnel.
	Name string

	// Managed writes the pid file and refuses to start a second instance.
	// `noket up --foreground` leaves the process to an external supervisor
	// and passes false.
	Managed bool

	// Version is the build version reported to the tunnel on registration.
	Version string

	Logger zerolog.Logger
}

// Run loads local credentials and configuration, then serves the tunnel until
// ctx is canceled.
func Run(ctx context.Context, opts RunOptions) error {
	logger := opts.Logger

	creds, err := config.LoadCredentials()
	if err != nil {
		if errors.Is(err, config.ErrNoCredentials) {
			return errors.New("not logged in, run noket login first")
		}
		return err
	}
	owner := config.Owner(creds)
	node, err := config.EnsureNode(owner)
	if err != nil {
		return err
	}
	configPath, err := config.ConfigPath()
	if err != nil {
		return err
	}
	store, err := config.NewStore(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if opts.Managed {
		if pid, running := RunningPID(); running && pid != os.Getpid() {
			return fmt.Errorf("noketd is already running (pid %d)", pid)
		}
		if err := config.WritePID(os.Getpid()); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer config.RemovePID()
	}

	logger.Info().
		Str("node_id", node.ID).
		Str("tunnel", store.Snapshot().Config.Tunnel.URL).
		Msg("noketd started")

	daemon := New(Options{
		Store:        store,
		Credentials:  creds,
		NodeID:       node.ID,
		NameOverride: opts.Name,
		Version:      opts.Version,
		OSVersion:    hostOSVersion(),
		Logger:       logger,
	})
	if err := daemon.Run(ctx); err != nil {
		var ownership *NodeOwnershipError
		if !errors.As(err, &ownership) {
			logger.Error().Err(err).Msg("noketd exited")
			return err
		}

		// The saved node id belongs to another account, which retrying cannot
		// fix. Give this account a fresh identity and try once more.
		logger.Warn().Err(err).Str("node_id", node.ID).Msg("node id rejected; rotating it")
		node, err = config.RotateNode(owner)
		if err != nil {
			return fmt.Errorf("rotate node id: %w", err)
		}
		daemon = New(Options{
			Store:        store,
			Credentials:  creds,
			NodeID:       node.ID,
			NameOverride: opts.Name,
			Version:      opts.Version,
			OSVersion:    hostOSVersion(),
			Logger:       logger,
		})
		if err := daemon.Run(ctx); err != nil {
			logger.Error().Err(err).Msg("noketd exited")
			return err
		}
	}

	config.RemoveStatus()
	logger.Info().Msg("noketd stopped")
	return nil
}
