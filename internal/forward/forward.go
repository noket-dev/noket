package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lesismal/arpc"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/rpc"
)

const chunkSendTimeout = 30 * time.Second

type inflight struct {
	cancel context.CancelFunc
}

// Handler implements the noketd side of /relay/forward and /relay/cancel.
type Handler struct {
	store  *config.Store
	logger zerolog.Logger
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc
	active sync.Map // request id -> *inflight
}

type Options struct {
	ResponseHeaderTimeout time.Duration
}

func New(store *config.Store, logger zerolog.Logger, parent context.Context, opts Options) *Handler {
	ctx, cancel := context.WithCancel(parent)
	return &Handler{
		store:  store,
		logger: logger.With().Str("component", "forward").Logger(),
		client: netguard.NewClient(netguard.ClientOptions{ResponseHeaderTimeout: opts.ResponseHeaderTimeout}),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Shutdown cancels every in-flight local request. The connection that owned
// this handler is gone either way.
func (h *Handler) Shutdown() {
	h.cancel()
	h.active.Range(func(_, value any) bool {
		if item, ok := value.(*inflight); ok {
			item.cancel()
		}
		return true
	})
}

// HandleForward acknowledges the open immediately, then streams the response
// from a goroutine so the gateway can start its stream lease.
func (h *Handler) HandleForward(ctx *arpc.Context) {
	open := &tunnelv1.ForwardOpen{}
	if err := ctx.Bind(open); err != nil {
		_ = ctx.Error(err)
		return
	}
	if open.RequestId == "" {
		_ = ctx.Error(errors.New("request_id is required"))
		return
	}
	if open.Method == "" {
		_ = ctx.Error(errors.New("method is required"))
		return
	}
	if len(open.Body) > MaxBodyBytes {
		_ = ctx.Write(&emptypb.Empty{})
		go h.fail(ctx.Client, open.RequestId, http.StatusRequestEntityTooLarge, "request body exceeds 32 MiB")
		return
	}

	_ = ctx.Write(&emptypb.Empty{})
	go h.forward(ctx.Client, open)
}

func (h *Handler) HandleCancel(ctx *arpc.Context) {
	req := &tunnelv1.CancelForward{}
	if err := ctx.Bind(req); err != nil {
		return
	}
	h.Cancel(req.RequestId)
}

// Cancel stops one in-flight local request.
func (h *Handler) Cancel(requestID string) {
	if item, ok := h.active.Load(requestID); ok {
		if inflight, ok := item.(*inflight); ok {
			inflight.cancel()
		}
	}
}

func (h *Handler) forward(client *arpc.Client, open *tunnelv1.ForwardOpen) {
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()

	item := &inflight{cancel: cancel}
	h.active.Store(open.RequestId, item)
	defer h.active.Delete(open.RequestId)

	model, err := ExtractModel(open.Body)
	if err != nil {
		h.fail(client, open.RequestId, http.StatusBadRequest, err.Error())
		return
	}

	snapshot := h.store.Snapshot()
	upstream, ok := snapshot.UpstreamByModel(model)
	if !ok {
		h.fail(client, open.RequestId, http.StatusNotFound,
			"model_not_found: no upstream exposes model "+model)
		return
	}

	req, err := BuildRequest(open, upstream, open.Body)
	if err != nil {
		h.fail(client, open.RequestId, http.StatusBadRequest, err.Error())
		return
	}
	req = req.WithContext(ctx)

	started := time.Now()
	resp, err := h.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		h.logger.Warn().Err(err).
			Str("request_id", open.RequestId).
			Str("upstream", upstream.Name).
			Str("model", model).
			Msg("local request failed")
		_ = h.sendChunk(client, &tunnelv1.ForwardChunk{
			RequestId: open.RequestId,
			Status:    http.StatusBadGateway,
			Headers:   JSONHeaders(),
			Data:      JSONError("upstream request failed: " + err.Error()),
			Done:      true,
			Error:     rpc.Status(rpc.CodeUnavailable, "upstream request failed"),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if err := h.sendChunk(client, &tunnelv1.ForwardChunk{
		RequestId: open.RequestId,
		Status:    int32(resp.StatusCode),
		Headers:   FilterResponseHeaders(resp.Header),
	}); err != nil {
		return
	}

	buf := make([]byte, MaxChunkBytes)
	var total int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > MaxResponseBytes {
				cancel()
				_ = h.sendChunk(client, &tunnelv1.ForwardChunk{
					RequestId: open.RequestId,
					Done:      true,
					Error:     rpc.Status(rpc.CodeResourceExhausted, "response exceeds 64 MiB"),
				})
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			if err := h.sendChunk(client, &tunnelv1.ForwardChunk{
				RequestId: open.RequestId,
				Data:      data,
			}); err != nil {
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				_ = h.sendChunk(client, &tunnelv1.ForwardChunk{
					RequestId: open.RequestId,
					Done:      true,
				})
				h.logger.Info().
					Str("request_id", open.RequestId).
					Str("upstream", upstream.Name).
					Int("status", resp.StatusCode).
					Int64("bytes", total).
					Dur("duration", time.Since(started)).
					Msg("forward complete")
				return
			}
			if ctx.Err() != nil {
				return
			}
			h.logger.Warn().Err(readErr).Str("request_id", open.RequestId).Msg("read local response")
			_ = h.sendChunk(client, &tunnelv1.ForwardChunk{
				RequestId: open.RequestId,
				Done:      true,
				Error:     rpc.Status(rpc.CodeInternal, "read local response failed"),
			})
			return
		}
	}
}

// fail sends a complete error response when no response headers were written
// yet.
func (h *Handler) fail(client *arpc.Client, requestID string, status int, message string) {
	_ = h.sendChunk(client, &tunnelv1.ForwardChunk{
		RequestId: requestID,
		Status:    int32(status),
		Headers:   JSONHeaders(),
		Data:      JSONError(message),
		Done:      true,
	})
}

func (h *Handler) sendChunk(client *arpc.Client, chunk *tunnelv1.ForwardChunk) error {
	ctx, cancel := context.WithTimeout(h.ctx, chunkSendTimeout)
	defer cancel()
	return client.CallWith(ctx, "/relay/chunk", chunk, &emptypb.Empty{})
}
