package noketd

import (
	"strings"

	"github.com/lesismal/arpc"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/rpc"
	"github.com/noket-dev/noket/internal/types"
)

func (a *Daemon) handleUpstreamCreate(ctx *arpc.Context) {
	req := &tunnelv1.CreateUpstreamRequest{}
	if err := ctx.Bind(req); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "invalid request: %v", err)))
		return
	}
	if err := a.checkNode(req.NodeId); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	protocol, err := localProtocol(req.Protocol)
	if err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}
	base, err := netguard.ParseBaseURL(req.BaseUrl)
	if err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := config.ValidateName(name); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
		return
	}

	up := types.Upstream{
		Name:     name,
		Protocol: protocol,
		BaseURL:  base.String(),
		APIKey:   req.ApiKey,
		Models:   trimModels(req.Models),
	}

	snap := a.store.Snapshot()
	if _, exists := snap.UpstreamByName(name); exists {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeAlreadyExists, "upstream %q already exists", name)))
		return
	}
	if err := config.ValidateUpstream(&up, snap.Upstreams); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
		return
	}

	if err := a.store.Mutate(func(cfg *types.Config) error {
		for i := range cfg.Upstreams {
			if cfg.Upstreams[i].Name == up.Name {
				return rpc.Errorf(rpc.CodeAlreadyExists, "upstream %q already exists", up.Name)
			}
		}
		cfg.Upstreams = append(cfg.Upstreams, up)
		return nil
	}); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	stored, _ := a.store.Snapshot().UpstreamByName(up.Name)
	if stored == nil {
		stored = &up
	}
	a.logger.Info().Str("upstream", up.Name).Msg("upstream created")
	writeReply(ctx, rpc.Reply(&tunnelv1.CreateUpstreamResponse{Upstream: toProtoUpstream(stored)}))
}

func (a *Daemon) handleUpstreamUpdate(ctx *arpc.Context) {
	req := &tunnelv1.UpdateUpstreamRequest{}
	if err := ctx.Bind(req); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "invalid request: %v", err)))
		return
	}
	if err := a.checkNode(req.NodeId); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	oldName := strings.TrimSpace(req.Name)
	snap := a.store.Snapshot()
	existing, ok := snap.UpstreamByName(oldName)
	if !ok {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeNotFound, "upstream %q does not exist", oldName)))
		return
	}

	protocol, err := localProtocol(req.Protocol)
	if err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}
	base, err := netguard.ParseBaseURL(req.BaseUrl)
	if err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
		return
	}

	newName := oldName
	if req.NewName != nil {
		newName = strings.TrimSpace(*req.NewName)
		if err := config.ValidateName(newName); err != nil {
			writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
			return
		}
		if newName != oldName {
			if _, exists := snap.UpstreamByName(newName); exists {
				writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeAlreadyExists, "upstream %q already exists", newName)))
				return
			}
		}
	}

	// A key is never carried to a different address without the caller
	// explicitly supplying it.
	if existing.BaseURL != base.String() && req.ApiKey == nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "api_key is required when the base URL changes")))
		return
	}
	apiKey := existing.APIKey
	if req.ApiKey != nil {
		apiKey = *req.ApiKey
	}

	up := types.Upstream{
		Name:     newName,
		Protocol: protocol,
		BaseURL:  base.String(),
		APIKey:   apiKey,
		Headers:  cloneHeaders(existing.Headers),
		Models:   trimModels(req.Models),
	}

	others := make([]types.Upstream, 0, len(snap.Upstreams))
	for _, other := range snap.Upstreams {
		if other.Name != oldName {
			others = append(others, other)
		}
	}
	if err := config.ValidateUpstream(&up, others); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)))
		return
	}

	if err := a.store.Mutate(func(cfg *types.Config) error {
		index := -1
		for i := range cfg.Upstreams {
			if cfg.Upstreams[i].Name == oldName {
				index = i
				break
			}
		}
		if index < 0 {
			return rpc.Errorf(rpc.CodeNotFound, "upstream %q does not exist", oldName)
		}
		if up.Name != oldName {
			for i := range cfg.Upstreams {
				if i != index && cfg.Upstreams[i].Name == up.Name {
					return rpc.Errorf(rpc.CodeAlreadyExists, "upstream %q already exists", up.Name)
				}
			}
		}
		cfg.Upstreams[index] = up
		return nil
	}); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	stored, _ := a.store.Snapshot().UpstreamByName(up.Name)
	if stored == nil {
		stored = &up
	}
	a.logger.Info().Str("upstream", up.Name).Msg("upstream updated")
	writeReply(ctx, rpc.Reply(&tunnelv1.UpdateUpstreamResponse{Upstream: toProtoUpstream(stored)}))
}

func (a *Daemon) handleUpstreamDelete(ctx *arpc.Context) {
	req := &tunnelv1.DeleteUpstreamRequest{}
	if err := ctx.Bind(req); err != nil {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeInvalidArgument, "invalid request: %v", err)))
		return
	}
	if err := a.checkNode(req.NodeId); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	name := strings.TrimSpace(req.Name)
	if _, ok := a.store.Snapshot().UpstreamByName(name); !ok {
		writeReply(ctx, rpc.ErrorReply(rpc.Errorf(rpc.CodeNotFound, "upstream %q does not exist", name)))
		return
	}

	if err := a.store.Mutate(func(cfg *types.Config) error {
		for i := range cfg.Upstreams {
			if cfg.Upstreams[i].Name != name {
				continue
			}
			cfg.Upstreams = append(cfg.Upstreams[:i], cfg.Upstreams[i+1:]...)
			return nil
		}
		return rpc.Errorf(rpc.CodeNotFound, "upstream %q does not exist", name)
	}); err != nil {
		writeReply(ctx, rpc.ErrorReply(err))
		return
	}

	a.logger.Info().Str("upstream", name).Msg("upstream deleted")
	writeReply(ctx, rpc.Reply(&tunnelv1.DeleteUpstreamResponse{}))
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		out[name] = value
	}
	return out
}
