package types

import (
	"fmt"
	"time"
)

const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
)

// Config is noketd's local config file. It is the source of truth for the
// upstream list; the server never keeps a copy.
type Config struct {
	API       APIConfig    `toml:"api"`
	Tunnel    Tunnel       `toml:"tunnel"`
	Noketd    NoketdConfig `toml:"noketd"`
	Upstreams []Upstream   `toml:"upstreams"`
}

// APIConfig points at the control-plane HTTP API used by `noket login` and
// the account commands. The tunnel WebSocket URL is configured separately
// because it usually lives on another host.
type APIConfig struct {
	// Base URL of the Noket control-plane API.
	BaseURL string `toml:"base_url"`
}

// Tunnel configures the WebSocket connection noketd keeps open to Noket.
type Tunnel struct {
	// WebSocket URL of the Noket tunnel endpoint.
	URL string `toml:"url"`
	// Name of this machine in the console. Empty uses the host name.
	Name string `toml:"name"`
}

// NoketdConfig tunes the daemon's probing of upstream APIs.
type NoketdConfig struct {
	// Timeout for each upstream probe, including model-list discovery.
	ProbeTimeout Duration `toml:"probe_timeout"`
	// Timeout for writing a request to an upstream API. Accepted for
	// compatibility; the current forwarder does not apply it.
	UpstreamWriteTimeout Duration `toml:"upstream_write_timeout"`
	// Largest response body the probe reads, in bytes.
	ProbeMaxBytes int64 `toml:"probe_max_bytes"`
}

// Upstream is one local LLM API. BaseURL may end with /v1: noketd joins the
// path it receives from the cloud without duplicating the version segment.
type Upstream struct {
	// Local name for this upstream, unique in the file.
	Name string `toml:"name"`
	// API dialect: "openai" or "anthropic".
	Protocol string `toml:"protocol"`
	// Address of the API. May end in /v1.
	BaseURL string `toml:"base_url"`
	// Key sent as a bearer token or x-api-key header. Stored in config.toml,
	// which is created with mode 0600.
	APIKey string `toml:"api_key"`
	// Extra request headers, for APIs that need more than a key.
	Headers map[string]string `toml:"headers,omitempty"`

	// Models are exposed to the cloud and act as routing keys. A model id may
	// appear in only one upstream.
	Models []string `toml:"models"`
}

func (u Upstream) HasAPIKey() bool { return u.APIKey != "" }

// Duration reads and writes Go duration strings ("10s") in TOML.
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

func (d *Duration) UnmarshalText(text []byte) error {
	raw := string(text)
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("duration must be a string such as \"10s\": %w", err)
	}
	*d = Duration(parsed)
	return nil
}
