package noketd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lesismal/arpc"
	arpccodec "github.com/lesismal/arpc/codec"
	arpclog "github.com/lesismal/arpc/log"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/forward"
	"github.com/noket-dev/noket/internal/types"
)

const (
	heartbeatInterval = 30 * time.Second
	registerTimeout   = 15 * time.Second
	heartbeatTimeout  = 10 * time.Second
)

// arpcLogTarget is swapped atomically by New. arpc reads its logger from
// several goroutines, so SetLogger must happen only in init.
var arpcLogTarget atomic.Pointer[zerolog.Logger]

func init() {
	// The tunnel server pins arpc's codec to protobuf. Noketd is a
	// single-purpose binary, so setting the process-wide default is simpler
	// and race-free compared with swapping Client.Codec after it starts.
	arpccodec.SetCodec(ProtoCodec{})
	arpclog.SetLogger(arpcLogger{})
}

// Options configures the daemon.
type Options struct {
	Store                 *config.Store
	Credentials           *types.Credentials
	NodeID                string
	NameOverride          string
	Version               string
	OSVersion             string
	Logger                zerolog.Logger
	ResponseHeaderTimeout time.Duration
}

// Daemon owns the WebSocket connection, registration, heartbeats and the
// reverse methods exposed to the tunnel.
type Daemon struct {
	store         *config.Store
	nodeID        string
	nameOverride  string
	version       string
	osVersion     string
	tunnelURL     string
	token         string
	logger        zerolog.Logger
	headerTimeout time.Duration

	ready     chan struct{}
	readyOnce sync.Once

	// epoch identifies the newest registered session, so a late disconnect
	// callback from an old connection cannot mark the status offline.
	epoch atomic.Int64

	statusMu   sync.Mutex
	startedAt  time.Time
	activeName string
	lastErr    string

	// writeMu orders status writes against shutdown. arpc's connection
	// callbacks can fire after Run has returned, and a status file written
	// then would outlive the daemon.
	writeMu sync.Mutex
	stopped bool
}

func New(opts Options) *Daemon {
	logger := opts.Logger.With().Str("component", "noketd").Logger()
	arpcLogTarget.Store(&logger)

	return &Daemon{
		store:         opts.Store,
		nodeID:        opts.NodeID,
		nameOverride:  opts.NameOverride,
		version:       opts.Version,
		osVersion:     opts.OSVersion,
		tunnelURL:     opts.Store.Snapshot().Config.Tunnel.URL,
		token:         opts.Credentials.Token,
		logger:        logger,
		headerTimeout: opts.ResponseHeaderTimeout,
		ready:         make(chan struct{}),
		startedAt:     time.Now().UTC(),
	}
}

// Ready is closed after the first successful /node/register.
func (a *Daemon) Ready() <-chan struct{} { return a.ready }

// Run connects, registers and reconnects until ctx is canceled.
func (a *Daemon) Run(ctx context.Context) error {
	// Runs after every return below, so no status file appears once Run has
	// handed control back and the caller removes the status.
	defer a.stopStatus()
	a.writeStatus(false, false)

	var bo backoff
	for {
		if ctx.Err() != nil {
			a.writeStatus(false, false)
			return nil
		}

		sess, err := a.newSession(ctx)
		if err != nil {
			if isUnauthorizedError(err) {
				return a.failAuthentication(err)
			}
			a.recordError(err)
			a.logger.Warn().Err(err).Msg("connect to tunnel")
			if !bo.wait(ctx) {
				a.writeStatus(false, false)
				return nil
			}
			continue
		}

		select {
		case <-sess.registered:
			bo.reset()
			sess.startHeartbeat()
		case <-sess.disconnected:
			sess.release()
			if fatal := sess.fatalError(); fatal != nil {
				if isUnauthorizedError(fatal) {
					return a.failAuthentication(fatal)
				}
				if isNodeOwnershipError(fatal) {
					return a.failNodeOwnership(fatal)
				}
				return fatal
			}
			if ctx.Err() != nil {
				return nil
			}
			a.recordError(errors.New("connection closed before registration"))
			a.logger.Warn().Msg("tunnel closed the connection before registration")
			if !bo.wait(ctx) {
				return nil
			}
			continue
		case <-ctx.Done():
			sess.release()
			return nil
		}

		select {
		case <-sess.disconnected:
			sess.release()
			if fatal := sess.fatalError(); fatal != nil {
				if isUnauthorizedError(fatal) {
					return a.failAuthentication(fatal)
				}
				if isNodeOwnershipError(fatal) {
					return a.failNodeOwnership(fatal)
				}
				return fatal
			}
			if ctx.Err() != nil {
				return nil
			}
			a.recordError(errors.New("connection lost"))
			a.writeStatus(false, false)
			a.logger.Warn().Msg("tunnel connection lost; reconnecting")
			if !bo.wait(ctx) {
				return nil
			}
		case <-ctx.Done():
			sess.release()
			return nil
		}
	}
}

func (a *Daemon) newSession(ctx context.Context) (*session, error) {
	sctx, cancel := context.WithCancel(ctx)
	sess := &session{
		daemon:       a,
		ctx:          sctx,
		cancel:       cancel,
		registered:   make(chan struct{}),
		disconnected: make(chan struct{}),
	}
	sess.forward = forward.New(a.store, a.logger, sctx, forward.Options{
		ResponseHeaderTimeout: a.headerTimeout,
	})

	handler := arpc.NewHandler()
	handler.SetLogTag("noket")
	// Noketd implements its own supervisor and backoff. Let arpc make one
	// quick reconnect attempt for transient blips, then hand control back.
	handler.SetMaxReconnectTimes(1)
	handler.HandleConnected(sess.onConnected)
	handler.HandleDisconnected(sess.onDisconnected)
	handler.HandleReconnect(sess.onReconnect)
	handler.Handle("/relay/forward", sess.forward.HandleForward)
	handler.Handle("/relay/cancel", sess.forward.HandleCancel)
	handler.Handle("/node/upstream/list", a.handleUpstreamList)
	handler.Handle("/node/upstream/probe", a.handleUpstreamProbe)
	handler.Handle("/node/upstream/apply", a.handleUpstreamCreate)
	handler.Handle("/node/upstream/update", a.handleUpstreamUpdate)
	handler.Handle("/node/upstream/delete", a.handleUpstreamDelete)

	client, err := arpc.NewClient(func() (net.Conn, error) {
		return dialWebSocket(sctx, a.tunnelURL, a.token)
	}, handler)
	if err != nil {
		cancel()
		return nil, err
	}
	sess.client = client
	return sess, nil
}

func (a *Daemon) nodeName() string {
	if a.nameOverride != "" {
		return a.nameOverride
	}
	if name := strings.TrimSpace(a.store.Snapshot().Config.Tunnel.Name); name != "" {
		return name
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "noket"
	}
	return hostname
}

func (a *Daemon) registeredName() string {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	return a.activeName
}

func (a *Daemon) setRegisteredName(name string) {
	a.statusMu.Lock()
	a.activeName = name
	a.statusMu.Unlock()
}

func (a *Daemon) recordError(err error) {
	a.statusMu.Lock()
	if err == nil {
		a.lastErr = ""
	} else {
		a.lastErr = err.Error()
	}
	a.statusMu.Unlock()
}

func (a *Daemon) writeStatus(connected, registered bool) {
	// Held across the write so stopStatus waits for it instead of letting a
	// temp file appear while the caller is tearing the config dir down.
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.stopped {
		return
	}

	a.statusMu.Lock()
	status := &types.DaemonStatus{
		PID:        os.Getpid(),
		NodeID:     a.nodeID,
		NodeName:   a.nodeName(),
		Registered: registered,
		Connected:  connected,
		StartedAt:  a.startedAt,
		LastError:  a.lastErr,
	}
	a.statusMu.Unlock()

	if err := config.WriteStatus(status); err != nil {
		a.logger.Warn().Err(err).Msg("write noketd status")
	}
}

// stopStatus drops the status writes that connection callbacks still attempt
// after Run returns.
func (a *Daemon) stopStatus() {
	a.writeMu.Lock()
	a.stopped = true
	a.writeMu.Unlock()
}

func (a *Daemon) signalReady() {
	a.readyOnce.Do(func() { close(a.ready) })
}

// failAuthentication clears the saved session only when it is still the one
// this daemon started with. A login in another terminal may already have saved
// a new token, and that newer session must survive this failure.
func (a *Daemon) failAuthentication(err error) error {
	creds, loadErr := config.LoadCredentials()
	saved := loadErr == nil
	if saved && creds.Token == a.token {
		_ = config.DeleteCredentials()
	}

	a.recordError(err)
	a.writeStatus(false, false)

	switch {
	case !saved:
		return fmt.Errorf("%v: the saved login is no longer usable, run `noket login` and start noketd again", err)
	case creds.Token == a.token:
		return fmt.Errorf("%v: local credentials were cleared, run `noket login` and start noketd again", err)
	default:
		return fmt.Errorf("%v: a newer login is saved; restart noketd to use it", err)
	}
}

// NodeOwnershipError reports that the tunnel rejected the node id because it is
// registered to a different account. Retrying with the same identity cannot
// succeed.
type NodeOwnershipError struct{ Err error }

func (e *NodeOwnershipError) Error() string { return e.Err.Error() }
func (e *NodeOwnershipError) Unwrap() error { return e.Err }

func isNodeOwnershipError(err error) bool {
	if err == nil {
		return false
	}
	var ownership *NodeOwnershipError
	if errors.As(err, &ownership) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "belongs to another account")
}

// failNodeOwnership stops the daemon. The caller may rotate the local node id
// once and try again; the same id will keep being rejected.
func (a *Daemon) failNodeOwnership(err error) error {
	a.recordError(err)
	a.writeStatus(false, false)
	return &NodeOwnershipError{Err: err}
}

// session is one WebSocket connection and its arpc client.
type session struct {
	daemon  *Daemon
	ctx     context.Context
	cancel  context.CancelFunc
	client  *arpc.Client
	forward *forward.Handler

	registered   chan struct{}
	regOnce      sync.Once
	disconnected chan struct{}
	discOnce     sync.Once
	hbOnce       sync.Once

	idsMu        sync.Mutex
	connectionID string
	gatewayID    string
	isRegistered bool
	epoch        int64

	fatalMu  sync.Mutex
	fatalErr error
}

func (s *session) onConnected(client *arpc.Client) {
	connectionID := newUUID()
	gatewayID := newUUID()
	s.idsMu.Lock()
	s.connectionID = connectionID
	s.gatewayID = gatewayID
	s.idsMu.Unlock()

	name := s.daemon.nodeName()
	if err := s.register(client, connectionID, gatewayID, name); err != nil {
		if isUnauthorizedError(err) || isNodeOwnershipError(err) {
			s.setFatal(err)
		}
		s.daemon.recordError(err)
		s.daemon.logger.Warn().Err(err).Msg("register node")
		s.daemon.writeStatus(false, false)
		go client.Stop()
		return
	}

	s.idsMu.Lock()
	s.isRegistered = true
	s.epoch = s.daemon.epoch.Add(1)
	s.idsMu.Unlock()

	s.daemon.setRegisteredName(name)
	s.daemon.recordError(nil)
	s.regOnce.Do(func() { close(s.registered) })
	s.daemon.signalReady()
	s.daemon.writeStatus(true, true)
	s.daemon.logger.Info().
		Str("node_id", s.daemon.nodeID).
		Str("name", name).
		Msg("node registered with tunnel")
}

func (s *session) onDisconnected(*arpc.Client) {
	s.idsMu.Lock()
	current := s.epoch != 0 && s.daemon.epoch.Load() == s.epoch
	s.idsMu.Unlock()
	// release closes s.ctx before stopping the client and writes the offline
	// status itself. A late callback from that shutdown must not write again
	// after noketd has already returned.
	if current && s.ctx.Err() == nil {
		s.daemon.writeStatus(false, false)
	}
	s.discOnce.Do(func() { close(s.disconnected) })
}

func (s *session) onReconnect(client *arpc.Client, info *arpc.ReconnectInfo) {
	if info.Err != nil {
		if isUnauthorizedError(info.Err) || isNodeOwnershipError(info.Err) {
			s.setFatal(info.Err)
			go client.Stop()
			return
		}
		s.daemon.logger.Debug().Err(info.Err).Int("attempt", info.Times).Msg("reconnect attempt failed")
	}
}

func (s *session) register(client *arpc.Client, connectionID, gatewayID, name string) error {
	ctx, cancel := context.WithTimeout(s.ctx, registerTimeout)
	defer cancel()

	return client.CallWith(ctx, "/node/register", &tunnelv1.RegisterNodeRequest{
		NodeId:       s.daemon.nodeID,
		Name:         name,
		ConnectionId: connectionID,
		GatewayId:    gatewayID,
		Config: &tunnelv1.NodeConfig{
			Platform:  runtime.GOOS,
			Arch:      runtime.GOARCH,
			Version:   s.daemon.version,
			OsVersion: s.daemon.osVersion,
		},
	}, &tunnelv1.RegisterNodeResponse{})
}

func (s *session) startHeartbeat() {
	s.hbOnce.Do(func() { go s.heartbeat() })
}

func (s *session) heartbeat() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.ctx, heartbeatTimeout)
			err := s.client.CallWith(ctx, "/node/ping", &emptypb.Empty{}, &timestamppb.Timestamp{})
			cancel()
			if err != nil {
				if errors.Is(err, arpc.ErrClientStopped) {
					return
				}
				if isUnauthorizedError(err) {
					s.setFatal(err)
					go s.client.Stop()
					return
				}
				s.daemon.logger.Warn().Err(err).Msg("heartbeat")
				continue
			}

			changed, err := s.daemon.store.ReloadIfChanged()
			if err != nil {
				s.daemon.logger.Warn().Err(err).Msg("reload config; keeping the previous snapshot")
				continue
			}
			if !changed {
				continue
			}
			name := s.daemon.nodeName()
			if name == s.daemon.registeredName() {
				continue
			}

			s.idsMu.Lock()
			connectionID, gatewayID := s.connectionID, s.gatewayID
			s.idsMu.Unlock()
			if err := s.register(s.client, connectionID, gatewayID, name); err != nil {
				s.daemon.recordError(err)
				s.daemon.logger.Warn().Err(err).Msg("update node name")
				continue
			}
			s.daemon.setRegisteredName(name)
			s.daemon.recordError(nil)
			s.daemon.writeStatus(true, true)
			s.daemon.logger.Info().Str("name", name).Msg("node name updated")
		}
	}
}

func (s *session) release() {
	s.forward.Shutdown()
	s.cancel()
	if s.client != nil {
		s.client.Stop()
	}
	s.daemon.writeStatus(false, false)
}

func (s *session) setFatal(err error) {
	s.fatalMu.Lock()
	if s.fatalErr == nil {
		s.fatalErr = err
	}
	s.fatalMu.Unlock()
}

func (s *session) fatalError() error {
	s.fatalMu.Lock()
	defer s.fatalMu.Unlock()
	return s.fatalErr
}

func newUUID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// arpcLogger routes arpc's connection and reconnect messages through zerolog.
type arpcLogger struct{}

func (arpcLogger) SetLevel(int) {}

func (arpcLogger) Debug(format string, v ...any) { arpcLogf(zerolog.DebugLevel, format, v...) }
func (arpcLogger) Info(format string, v ...any)  { arpcLogf(zerolog.InfoLevel, format, v...) }
func (arpcLogger) Warn(format string, v ...any)  { arpcLogf(zerolog.WarnLevel, format, v...) }
func (arpcLogger) Error(format string, v ...any) { arpcLogf(zerolog.ErrorLevel, format, v...) }

func arpcLogf(level zerolog.Level, format string, v ...any) {
	logger := arpcLogTarget.Load()
	if logger == nil {
		return
	}
	logger.WithLevel(level).Msgf(format, v...)
}
