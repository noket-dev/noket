package noketd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/lesismal/arpc"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

// fakeGateway speaks the same arpc + protobuf wire contract as noket-tunnel.
type fakeGateway struct {
	token     string
	ws        *wsListener
	peer      atomic.Pointer[arpc.Client]
	registers chan *tunnelv1.RegisterNodeRequest
	chunks    chan *tunnelv1.ForwardChunk

	mu          sync.Mutex
	registerErr error
	rejectFirst int
	attempts    int
}

// rejectRegister makes the next attempts register calls fail instead of
// registering the node.
func (g *fakeGateway) rejectRegister(err error, attempts int) {
	g.mu.Lock()
	g.registerErr = err
	g.rejectFirst = attempts
	g.mu.Unlock()
}

type wsListener struct {
	addr   net.Addr
	ch     chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *wsListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ch:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *wsListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *wsListener) Addr() net.Addr { return l.addr }

func startFakeGateway(t *testing.T, token string) *fakeGateway {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGateway{
		token:     token,
		ws:        &wsListener{addr: ln.Addr(), ch: make(chan net.Conn), closed: make(chan struct{})},
		registers: make(chan *tunnelv1.RegisterNodeRequest, 4),
		chunks:    make(chan *tunnelv1.ForwardChunk, 64),
	}

	handler := arpc.NewHandler()
	handler.Handle("/node/register", g.onRegister)
	handler.Handle("/node/ping", func(ctx *arpc.Context) {
		req := &emptypb.Empty{}
		if err := ctx.Bind(req); err != nil {
			_ = ctx.Error(err)
			return
		}
		_ = ctx.Write(timestamppb.Now())
	})
	handler.Handle("/relay/chunk", g.onChunk)

	server := arpc.NewServer()
	server.Handler = handler
	server.Codec = ProtoCodec{}
	go func() { _ = server.Serve(g.ws) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", g.handleWS)
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpServer.Serve(ln) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = g.ws.Close()
		_ = httpServer.Close()
		_ = ln.Close()
	})
	t.Cleanup(func() {
		select {
		case <-g.ws.closed:
		default:
			_ = g.ws.Close()
		}
	})
	return g
}

func (g *fakeGateway) url() string { return "ws://" + g.ws.addr.String() + "/ws" }

func (g *fakeGateway) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+g.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	select {
	case g.ws.ch <- &wsConn{Conn: conn}:
	case <-g.ws.closed:
		_ = conn.Close()
	}
}

func (g *fakeGateway) onRegister(ctx *arpc.Context) {
	req := &tunnelv1.RegisterNodeRequest{}
	if err := ctx.Bind(req); err != nil {
		_ = ctx.Error(err)
		return
	}
	g.peer.Store(ctx.Client)
	g.registers <- req

	g.mu.Lock()
	attempt := g.attempts
	g.attempts++
	reject := g.registerErr != nil && attempt < g.rejectFirst
	err := g.registerErr
	g.mu.Unlock()
	if reject {
		_ = ctx.Error(err)
		return
	}
	_ = ctx.Write(&tunnelv1.RegisterNodeResponse{})
}

func (g *fakeGateway) onChunk(ctx *arpc.Context) {
	chunk := &tunnelv1.ForwardChunk{}
	if err := ctx.Bind(chunk); err != nil {
		_ = ctx.Error(err)
		return
	}
	g.chunks <- chunk
	_ = ctx.Write(&emptypb.Empty{})
}

func TestRunRegistersAndForwards(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	const nodeID = "0199b2a0-0000-7000-8000-000000000001"
	token := "test" + "-" + "token"
	gateway := startFakeGateway(t, token)

	var upstreamMu sync.Mutex
	var upstreamAuth, upstreamFixed, upstreamPath, upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamMu.Lock()
		upstreamAuth = r.Header.Get("Authorization")
		upstreamFixed = r.Header.Get("X-Fixed")
		upstreamPath = r.URL.RequestURI()
		upstreamBody = string(body)
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := &types.Config{
		Tunnel: types.Tunnel{URL: gateway.url(), Name: "test-node"},
		Upstreams: []types.Upstream{{
			Name:     "local",
			Protocol: types.ProtocolOpenAI,
			BaseURL:  upstream.URL,
			APIKey:   "upstream" + "-" + "key",
			Headers:  map[string]string{"X-Fixed": "1"},
			Models:   []string{"test-model"},
		}},
	}
	config.ApplyDefaults(cfg)
	config.Normalize(cfg)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ag := New(Options{
		Store:                 store,
		Credentials:           &types.Credentials{Token: token},
		NodeID:                nodeID,
		Version:               "v1.2.3",
		OSVersion:             "Ubuntu 24.04.1 LTS",
		Logger:                zerolog.Nop(),
		ResponseHeaderTimeout: 5 * time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- ag.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-runErr:
		case <-time.After(5 * time.Second):
			t.Error("noketd did not stop")
		}
	}()

	select {
	case <-ag.Ready():
	case err := <-runErr:
		t.Fatalf("noketd stopped before registering: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for registration")
	}

	var reg *tunnelv1.RegisterNodeRequest
	select {
	case reg = <-gateway.registers:
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not receive /node/register")
	}
	if reg.NodeId != nodeID || reg.Name != "test-node" {
		t.Fatalf("register = %+v", reg)
	}
	if reported := reg.Config; reported == nil {
		t.Fatal("register reported no node config")
	} else if reported.Platform != runtime.GOOS || reported.Arch != runtime.GOARCH ||
		reported.Version != "v1.2.3" || reported.OsVersion != "Ubuntu 24.04.1 LTS" {
		t.Fatalf("register config = %+v", reported)
	}
	for label, value := range map[string]string{"node": reg.NodeId, "connection": reg.ConnectionId, "gateway": reg.GatewayId} {
		if _, err := uuid.Parse(value); err != nil {
			t.Fatalf("%s id %q is not a UUID: %v", label, value, err)
		}
	}

	peer := gateway.peer.Load()
	if peer == nil {
		t.Fatal("gateway has no peer connection")
	}

	callCtx, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()

	reply := &tunnelv1.UpstreamReply{}
	if err := peer.CallWith(callCtx, "/node/upstream/list", &tunnelv1.GetNodeUpstreamsRequest{NodeId: nodeID}, reply); err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if reply.Status.GetCode() != 0 {
		t.Fatalf("list upstreams status = %v", reply.Status)
	}
	list := &tunnelv1.GetNodeUpstreamsResponse{}
	if err := reply.Data.UnmarshalTo(list); err != nil {
		t.Fatal(err)
	}
	if len(list.Upstreams) != 1 {
		t.Fatalf("upstreams = %+v", list.Upstreams)
	}
	seen := list.Upstreams[0]
	if seen.Name != "local" || seen.BaseUrl != upstream.URL || !seen.HasApiKey || len(seen.Models) != 1 || seen.Models[0] != "test-model" {
		t.Fatalf("upstream = %+v", seen)
	}
	if list.CapturedAt == nil {
		t.Fatal("captured_at is missing")
	}

	open := &tunnelv1.ForwardOpen{
		RequestId: "req-1",
		Method:    http.MethodPost,
		Path:      "/v1/chat/completions?stream=1",
		Headers: []*tunnelv1.Header{
			{Name: "Authorization", Value: "cloud-token"},
			{Name: "Content-Type", Value: "application/json"},
		},
		Body: []byte(`{"model":"test-model","messages":[]}`),
	}
	if err := peer.CallWith(callCtx, "/relay/forward", open, &emptypb.Empty{}); err != nil {
		t.Fatalf("forward: %v", err)
	}

	var (
		status   int32
		headers  = map[string]string{}
		body     []byte
		chunkErr error
	)
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk := <-gateway.chunks:
			if chunk.RequestId != "req-1" {
				t.Fatalf("chunk request id = %q", chunk.RequestId)
			}
			if chunk.Status != 0 {
				status = chunk.Status
				for _, header := range chunk.Headers {
					headers[http.CanonicalHeaderKey(header.Name)] = header.Value
				}
			}
			body = append(body, chunk.Data...)
			if chunk.Done {
				if chunk.Error != nil {
					chunkErr = errors.New(chunk.Error.Message)
				}
				goto collected
			}
		case <-timeout:
			t.Fatal("timed out waiting for response chunks")
		}
	}

collected:
	if chunkErr != nil {
		t.Fatalf("forward returned an error: %v", chunkErr)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if headers["Content-Type"] != "application/json" || headers["X-Upstream"] != "yes" {
		t.Fatalf("response headers = %v", headers)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}

	upstreamMu.Lock()
	gotAuth, gotFixed, gotPath, gotBody := upstreamAuth, upstreamFixed, upstreamPath, upstreamBody
	upstreamMu.Unlock()
	if gotAuth != "Bearer "+"upstream"+"-"+"key" {
		t.Fatalf("upstream Authorization = %q, want the local key", gotAuth)
	}
	if gotFixed != "1" {
		t.Fatalf("upstream X-Fixed = %q", gotFixed)
	}
	if gotPath != "/v1/chat/completions?stream=1" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotBody != string(open.Body) {
		t.Fatalf("upstream body = %q", gotBody)
	}
}

func newUnauthorizedDaemon(t *testing.T, dir, token string) *Daemon {
	t.Helper()

	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(unauthorized.Close)

	cfg := &types.Config{Tunnel: types.Tunnel{URL: "ws" + strings.TrimPrefix(unauthorized.URL, "http") + "/ws"}}
	config.ApplyDefaults(cfg)
	config.Normalize(cfg)
	configPath := filepath.Join(dir, "config.toml")
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return New(Options{
		Store:       store,
		Credentials: &types.Credentials{Token: token},
		NodeID:      "0199b2a0-0000-7000-8000-000000000002",
		Logger:      zerolog.Nop(),
	})
}

func TestRunClearsCredentialsOnUnauthorized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	if err := config.SaveCredentials(&types.Credentials{Token: "bad" + "-" + "token"}); err != nil {
		t.Fatal(err)
	}
	ag := newUnauthorizedDaemon(t, dir, "bad"+"-"+"token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ag.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "credentials were cleared") {
		t.Fatalf("Run error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credentials file still exists: %v", err)
	}
}

func TestRunKeepsNewerCredentialsOnUnauthorized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	// The daemon started with the stale token; another terminal has since
	// logged in and saved a newer one.
	if err := config.SaveCredentials(&types.Credentials{Token: "newer" + "-" + "token"}); err != nil {
		t.Fatal(err)
	}
	ag := newUnauthorizedDaemon(t, dir, "stale"+"-"+"token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ag.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "a newer login is saved") {
		t.Fatalf("Run error = %v", err)
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials after the failure: %v", err)
	}
	if creds.Token != "newer"+"-"+"token" {
		t.Fatalf("saved token = %q, want the newer login to survive", creds.Token)
	}
}

func TestRunStopsOnNodeOwnershipError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	token := "test" + "-" + "token"
	gateway := startFakeGateway(t, token)
	gateway.rejectRegister(errors.New("permission_denied: Node belongs to another account"), 10)

	cfg := &types.Config{Tunnel: types.Tunnel{URL: gateway.url()}}
	config.ApplyDefaults(cfg)
	config.Normalize(cfg)
	configPath := filepath.Join(dir, "config.toml")
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ag := New(Options{
		Store:       store,
		Credentials: &types.Credentials{Token: token},
		NodeID:      "0199b2a0-0000-7000-8000-000000000003",
		Logger:      zerolog.Nop(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = ag.Run(ctx)
	var ownership *NodeOwnershipError
	if !errors.As(err, &ownership) {
		t.Fatalf("Run error = %v, want a NodeOwnershipError", err)
	}
}

func TestRunRotatesNodeIDOnOwnershipError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	token := "test" + "-" + "token"
	gateway := startFakeGateway(t, token)
	gateway.rejectRegister(errors.New("permission_denied: Node belongs to another account"), 1)

	cfg := &types.Config{Tunnel: types.Tunnel{URL: gateway.url()}}
	config.ApplyDefaults(cfg)
	config.Normalize(cfg)
	configPath := filepath.Join(dir, "config.toml")
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCredentials(&types.Credentials{Token: token, UserID: "user-a"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, RunOptions{Logger: zerolog.Nop()}) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("noketd did not stop")
		}
	}()

	var registers []*tunnelv1.RegisterNodeRequest
	for len(registers) < 2 {
		select {
		case req := <-gateway.registers:
			registers = append(registers, req)
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out after %d registrations", len(registers))
		}
	}

	if registers[0].NodeId == registers[1].NodeId {
		t.Fatalf("node id was not rotated: %q", registers[0].NodeId)
	}
	node, err := config.ReadNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.ID != registers[1].NodeId {
		t.Fatalf("saved node id = %q, want the rotated id %q", node.ID, registers[1].NodeId)
	}
}

func TestRunReportsMissingCredentialsOnUnauthorized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	ag := newUnauthorizedDaemon(t, dir, "stale"+"-"+"token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ag.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "no longer usable") {
		t.Fatalf("Run error = %v", err)
	}
}
