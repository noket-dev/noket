package probe

import (
	"net/http"

	"github.com/noket-dev/noket/internal/types"
)

// AnthropicVersion is sent by default for anthropic upstreams unless the
// config or the incoming request already sets it.
const AnthropicVersion = "2023-06-01"

// ApplyAuth injects the local upstream credentials. The tunnel's node token
// is never reused here: the caller strips it before calling this function.
// Config headers are applied last, so they win.
func ApplyAuth(h http.Header, protocol, apiKey string, headers map[string]string) {
	switch protocol {
	case types.ProtocolAnthropic:
		if apiKey != "" {
			h.Set("x-api-key", apiKey)
		}
		if h.Get("anthropic-version") == "" {
			h.Set("anthropic-version", AnthropicVersion)
		}
	case types.ProtocolOpenAI:
		if apiKey != "" {
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}

	for name, value := range headers {
		h.Set(name, value)
	}
}
