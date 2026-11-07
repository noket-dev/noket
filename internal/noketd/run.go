package noketd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

// credentialPollInterval is how often a supervised process without a login
// checks for one.
const credentialPollInterval = 5 * time.Second

// RunOptions configures a noketd process.
type RunOptions struct {
	// Name overrides the node name reported to the tunnel.
	Name string

	// Managed writes the pid file and refuses to start a second instance.
	// `noket up --foreground` leaves the process to an external supervisor
	// and passes false, which also makes it wait for a login instead of
	// exiting when none is saved.
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
		if !errors.Is(err, config.ErrNoCredentials) {
			return err
		}
		if opts.Managed {
			return errors.New("not logged in, run noket login first")
		}
		// A supervisor starts this process at boot and restarts it whenever it
		// exits, so returning the error here would add a log line on every
		// attempt until the user logs in. Wait for the login instead.
		logger.Warn().Msg("not logged in; waiting for `noket login`")
		creds, err = waitForCredentials(ctx, credentialPollInterval, config.LoadCredentials)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				logger.Info().Msg("noketd stopped")
				return nil
			}
			return err
		}
		if pid, running := Running(); running && pid != os.Getpid() {
			// `noket up` was run by hand while this process waited; let it serve.
			logger.Info().Int("pid", pid).Msg("noketd is already running; exiting")
			return nil
		}
		logger.Info().Msg("login found; starting")
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

// waitForCredentials polls for a saved login and returns it once one appears.
// A login that cannot be read for another reason ends the wait, since polling
// would not fix it.
func waitForCredentials(ctx context.Context, every time.Duration, load func() (*types.Credentials, error)) (*types.Credentials, error) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		creds, err := load()
		if err == nil {
			return creds, nil
		}
		if !errors.Is(err, config.ErrNoCredentials) {
			return nil, err
		}
	}
}
