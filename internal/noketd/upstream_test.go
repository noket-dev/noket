package noketd

import (
	"errors"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/rpc"
	"github.com/noket-dev/noket/internal/types"
)

func probeStore(t *testing.T) *config.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.Upstreams = []types.Upstream{{
		Name:     "local",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		APIKey:   "local" + "-" + "key",
		Headers:  map[string]string{"X-Tenant": "a"},
		Models:   []string{"m"},
	}}
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestProbeTargetReusesKeyForUnchangedAddress(t *testing.T) {
	daemon := &Daemon{}
	req := &tunnelv1.ProbeUpstreamRequest{
		Protocol: tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI,
		BaseUrl:  "http://127.0.0.1:11434/",
		Name:     "local",
	}
	up, err := daemon.probeTarget(req, probeStore(t).Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "local"+"-"+"key" {
		t.Fatalf("api key = %q, want the stored key", up.APIKey)
	}
	if up.Headers["X-Tenant"] != "a" {
		t.Fatalf("headers = %v", up.Headers)
	}
}

func TestProbeTargetRejectsKeyReuseAfterAddressChange(t *testing.T) {
	daemon := &Daemon{}
	req := &tunnelv1.ProbeUpstreamRequest{
		Protocol: tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI,
		BaseUrl:  "http://127.0.0.1:8000",
		Name:     "local",
	}
	_, err := daemon.probeTarget(req, probeStore(t).Snapshot())
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != rpc.CodeInvalidArgument {
		t.Fatalf("error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestProbeTargetExplicitKeyOnChangedAddress(t *testing.T) {
	daemon := &Daemon{}
	req := &tunnelv1.ProbeUpstreamRequest{
		Protocol: tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_ANTHROPIC,
		BaseUrl:  "http://127.0.0.1:8000",
		Name:     "local",
		ApiKey:   proto.String("new-key"),
	}
	up, err := daemon.probeTarget(req, probeStore(t).Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "new-key" {
		t.Fatalf("api key = %q", up.APIKey)
	}
	if len(up.Headers) != 0 {
		t.Fatalf("headers were carried to a new address: %v", up.Headers)
	}
}

func TestProbeTargetExplicitEmptyKey(t *testing.T) {
	daemon := &Daemon{}
	req := &tunnelv1.ProbeUpstreamRequest{
		Protocol: tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI,
		BaseUrl:  "http://127.0.0.1:11434",
		Name:     "local",
		ApiKey:   proto.String(""),
	}
	up, err := daemon.probeTarget(req, probeStore(t).Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "" {
		t.Fatalf("api key = %q, want empty", up.APIKey)
	}
}

func TestProbeTargetUnknownNameAndProtocol(t *testing.T) {
	daemon := &Daemon{}
	req := &tunnelv1.ProbeUpstreamRequest{
		Protocol: tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI,
		BaseUrl:  "http://127.0.0.1:11434",
		Name:     "missing",
	}
	var rpcErr *rpc.Error
	if _, err := daemon.probeTarget(req, probeStore(t).Snapshot()); !errors.As(err, &rpcErr) || rpcErr.Code != rpc.CodeNotFound {
		t.Fatalf("unknown name error = %v, want NOT_FOUND", err)
	}

	req.Name = ""
	req.Protocol = tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_UNSPECIFIED
	if _, err := daemon.probeTarget(req, probeStore(t).Snapshot()); !errors.As(err, &rpcErr) || rpcErr.Code != rpc.CodeInvalidArgument {
		t.Fatalf("unspecified protocol error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestToProtoUpstream(t *testing.T) {
	up := &types.Upstream{
		Name:     "local",
		Protocol: types.ProtocolAnthropic,
		BaseURL:  "http://127.0.0.1:11434",
		APIKey:   "key",
		Models:   []string{"a", "b"},
	}
	got := toProtoUpstream(up)
	if got.Protocol != tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_ANTHROPIC {
		t.Fatalf("protocol = %v", got.Protocol)
	}
	if !got.HasApiKey {
		t.Fatal("has_api_key = false")
	}
	if len(got.Models) != 2 || got.Models[0] != "a" {
		t.Fatalf("models = %v", got.Models)
	}

	got.Models[0] = "mutated"
	if up.Models[0] != "a" {
		t.Fatal("toProtoUpstream shares the model slice with the config")
	}
}
