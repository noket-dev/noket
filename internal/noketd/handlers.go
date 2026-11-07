package noketd

import (
	"strings"

	"github.com/lesismal/arpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/probe"
	"github.com/noket-dev/noket/internal/rpc"
	"github.com/noket-dev/noket/internal/types"
)

func (a *Daemon) handleUpstreamList(ctx *arpc.Context) {
	req := &tunnelv1.GetNodeUpstreamsRequest{}
	if err := ctx.Bind(req); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "invalid request: %v", err)))
		return
	}
	if err := a.checkNode(req.NodeId); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	// The config file is the source of truth. Pick up external edits before
	// answering so the control plane sees what the router would use.
	if _, err := a.store.ReloadIfChanged(); err != nil {
		a.logger.Warn().Err(err).Msg("reload config; keeping the previous snapshot")
	}

	snap := a.store.Snapshot()
	rsp := &tunnelv1.GetNodeUpstreamsResponse{
		Upstreams:  make([]*tunnelv1.Upstream, 0, len(snap.Upstreams)),
		CapturedAt: timestamppb.New(snap.CapturedAt),
	}
	for i := range snap.Upstreams {
		rsp.Upstreams = append(rsp.Upstreams, toProtoUpstream(&snap.Upstreams[i]))
	}
	writeReply(ctx, rpc.Reply(rsp))
}

func (a *Daemon) handleUpstreamProbe(ctx *arpc.Context) {
	req := &tunnelv1.ProbeUpstreamRequest{}
	if err := ctx.Bind(req); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "invalid request: %v", err)))
		return
	}
	if err := a.checkNode(req.NodeId); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	snap := a.store.Snapshot()
	up, err := a.probeTarget(req, snap)
	if err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	models, err := probe.Models(ctx, probe.Options{
		Protocol: up.Protocol,
		BaseURL:  up.BaseURL,
		APIKey:   up.APIKey,
		Headers:  up.Headers,
		Timeout:  snap.Config.Noketd.ProbeTimeout.Std(),
		MaxBytes: snap.Config.Noketd.ProbeMaxBytes,
	})
	if err != nil {
		a.logger.Warn().Err(err).Str("base_url", up.BaseURL).Msg("probe upstream")
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}
	writeReply(ctx, rpc.Reply(&tunnelv1.ProbeUpstreamResponse{Models: models}))
}

// probeTarget applies the key-reuse rules: an omitted key on a known name is
// only safe to reuse while the address is unchanged, so a stale key is never
// sent to a different host.
func (a *Daemon) probeTarget(req *tunnelv1.ProbeUpstreamRequest, snap *config.Snapshot) (*types.Upstream, error) {
	protocol, err := localProtocol(req.Protocol)
	if err != nil {
		return nil, err
	}
	base, err := netguard.ParseBaseURL(req.BaseUrl)
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)
	}

	target := &types.Upstream{Protocol: protocol, BaseURL: base.String()}
	var existing *types.Upstream
	if req.Name != "" {
		found, ok := snap.UpstreamByName(req.Name)
		if !ok {
			return nil, rpc.Errorf(rpc.CodeNotFound, "upstream %q does not exist", req.Name)
		}
		existing = found
	}

	switch {
	case req.ApiKey != nil:
		target.APIKey = *req.ApiKey
	case existing != nil && existing.BaseURL == target.BaseURL:
		target.APIKey = existing.APIKey
		if existing.Protocol == target.Protocol {
			target.Headers = existing.Headers
		}
	case existing != nil:
		return nil, rpc.Errorf(rpc.CodeInvalidArgument, "api_key is required when the base URL changes")
	}

	return target, nil
}

func (a *Daemon) checkNode(nodeID string) error {
	if nodeID != "" && nodeID != a.nodeID {
		return rpc.Errorf(rpc.CodePermissionDenied, "node_id does not belong to this node")
	}
	return nil
}

func toProtoUpstream(up *types.Upstream) *tunnelv1.Upstream {
	return &tunnelv1.Upstream{
		Name:      up.Name,
		Protocol:  protoProtocol(up.Protocol),
		BaseUrl:   up.BaseURL,
		Models:    append([]string(nil), up.Models...),
		HasApiKey: up.HasAPIKey(),
	}
}

func protoProtocol(protocol string) tunnelv1.UpstreamProtocol {
	switch protocol {
	case types.ProtocolOpenAI:
		return tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI
	case types.ProtocolAnthropic:
		return tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_ANTHROPIC
	default:
		return tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_UNSPECIFIED
	}
}

func localProtocol(protocol tunnelv1.UpstreamProtocol) (string, error) {
	switch protocol {
	case tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_OPENAI:
		return types.ProtocolOpenAI, nil
	case tunnelv1.UpstreamProtocol_UPSTREAM_PROTOCOL_ANTHROPIC:
		return types.ProtocolAnthropic, nil
	default:
		return "", rpc.Errorf(rpc.CodeInvalidArgument, "protocol must be openai or anthropic")
	}
}

func writeReply(ctx *arpc.Context, reply *tunnelv1.UpstreamReply) {
	_ = ctx.Write(reply)
}

func trimModels(models []string) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		if trimmed := strings.TrimSpace(model); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
