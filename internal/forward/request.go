package forward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
)

const (
	// MaxBodyBytes matches the tunnel ingress default.
	MaxBodyBytes = 32 << 20
	// MaxChunkBytes is the noketd-side read size; ingress also enforces its
	// own max_chunk_bytes.
	MaxChunkBytes = 256 << 10
	// MaxResponseBytes bounds one forwarded local response.
	MaxResponseBytes = 64 << 20
)

var hopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

// ExtractModel reads the routing key from the JSON request body.
func ExtractModel(body []byte) (string, error) {
	if len(body) == 0 {
		return "", errors.New("request body is empty")
	}
	payload := struct {
		Model string `json:"model"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("request body is not a JSON object: %w", err)
	}
	if strings.TrimSpace(payload.Model) == "" {
		return "", errors.New("request body has no model field")
	}
	return payload.Model, nil
}

// FilterRequestHeaders keeps business headers and drops hop-by-hop headers,
// Host, Content-Length, client credentials and the tunnel's node token. Local
// credentials are injected afterwards.
func FilterRequestHeaders(in []*tunnelv1.Header) http.Header {
	out := make(http.Header, len(in))
	for _, header := range in {
		if header == nil {
			continue
		}
		name := header.Name
		lower := strings.ToLower(name)
		if _, ok := hopHeaders[lower]; ok {
			continue
		}
		switch lower {
		case "host", "content-length", "authorization", "x-api-key":
			continue
		}
		out.Add(name, header.Value)
	}
	return out
}

// FilterResponseHeaders drops hop-by-hop headers and Content-Length. Ingress
// writes the headers verbatim for the first chunk.
func FilterResponseHeaders(in http.Header) []*tunnelv1.Header {
	out := make([]*tunnelv1.Header, 0, len(in))
	for name, values := range in {
		lower := strings.ToLower(name)
		if _, ok := hopHeaders[lower]; ok {
			continue
		}
		if lower == "content-length" {
			continue
		}
		for _, value := range values {
			out = append(out, &tunnelv1.Header{Name: name, Value: value})
		}
	}
	return out
}

// BuildRequest assembles the local HTTP request. path comes from the cloud
// verbatim, including its query string.
func BuildRequest(open *tunnelv1.ForwardOpen, up *types.Upstream, body []byte) (*http.Request, error) {
	if !strings.HasPrefix(open.Path, "/") {
		return nil, errors.New("forward path must start with /")
	}
	target, err := url.Parse(up.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("build upstream URL: %w", err)
	}
	request, err := url.Parse(open.Path)
	if err != nil {
		return nil, fmt.Errorf("build upstream path: %w", err)
	}
	target.Path = netguard.JoinPath(target.Path, request.Path)
	target.RawPath = ""
	target.RawQuery = request.RawQuery

	req, err := http.NewRequest(open.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	req.Header = FilterRequestHeaders(open.Headers)
	applyAuth(req.Header, up)
	return req, nil
}

func applyAuth(h http.Header, up *types.Upstream) {
	// Kept here (rather than importing probe) so the forward path has no
	// dependency on the discovery code. Behaviour matches probe.ApplyAuth.
	switch up.Protocol {
	case types.ProtocolAnthropic:
		if up.APIKey != "" {
			h.Set("x-api-key", up.APIKey)
		}
		if h.Get("anthropic-version") == "" {
			h.Set("anthropic-version", "2023-06-01")
		}
	case types.ProtocolOpenAI:
		if up.APIKey != "" {
			h.Set("Authorization", "Bearer "+up.APIKey)
		}
	}
	for name, value := range up.Headers {
		h.Set(name, value)
	}
}

// JSONError renders an OpenAI-style error body for local failures.
func JSONError(message string) []byte {
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "tunnel_error",
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func JSONHeaders() []*tunnelv1.Header {
	return []*tunnelv1.Header{{Name: "Content-Type", Value: "application/json"}}
}
