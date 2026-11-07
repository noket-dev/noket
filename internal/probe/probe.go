package probe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/rpc"
)

// Options describes one GET {base_url}/v1/models call. APIKey travels only in
// this request; it is never logged or persisted by the probe.
type Options struct {
	Protocol string
	BaseURL  string
	APIKey   string
	Headers  map[string]string
	Timeout  time.Duration
	MaxBytes int64
}

// Models discovers model ids from an OpenAI- or Anthropic-compatible local
// service. The returned order is the order reported by the service.
func Models(ctx context.Context, opts Options) ([]string, error) {
	base, err := netguard.ParseBaseURL(opts.BaseURL)
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeInvalidArgument, "%v", err)
	}

	target := *base
	target.Path = netguard.JoinPath(target.Path, "/v1/models")

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeInvalidArgument, "build probe request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	ApplyAuth(req.Header, opts.Protocol, opts.APIKey, opts.Headers)

	client := netguard.NewClient(netguard.ClientOptions{})
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, rpc.Errorf(rpc.CodeDeadlineExceeded, "probe timed out after %s", timeout)
		}
		return nil, rpc.Errorf(rpc.CodeUnavailable, "probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeUnavailable, "read probe response: %v", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, rpc.Errorf(rpc.CodeInvalidArgument, "probe response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, rpc.Errorf(rpc.CodeUnavailable, "upstream returned HTTP %d: %s", resp.StatusCode, snippet(body))
	}

	models, err := parseModels(body)
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeUnavailable, "parse probe response: %v", err)
	}
	if len(models) == 0 {
		return nil, rpc.Errorf(rpc.CodeNotFound, "upstream reported no models")
	}
	return models, nil
}

func parseModels(body []byte) ([]string, error) {
	openAI := struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}{}
	if err := json.Unmarshal(body, &openAI); err == nil {
		models := make([]string, 0, len(openAI.Data))
		for _, item := range openAI.Data {
			if item.ID != "" {
				models = append(models, item.ID)
			}
		}
		if len(models) > 0 {
			return dedupe(models), nil
		}
	}

	// Some local servers (notably Ollama's native API) use a different shape.
	legacy := struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}{}
	if err := json.Unmarshal(body, &legacy); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(legacy.Models))
	for _, item := range legacy.Models {
		id := item.Name
		if id == "" {
			id = item.Model
		}
		if id != "" {
			models = append(models, id)
		}
	}
	return dedupe(models), nil
}

func dedupe(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func snippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	if text == "" {
		return "(empty body)"
	}
	return text
}
