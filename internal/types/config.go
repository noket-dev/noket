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
	Web       WebConfig    `toml:"web"`
	Tunnel    Tunnel       `toml:"tunnel"`
	Noketd    NoketdConfig `toml:"noketd"`
	Upstreams []Upstream   `toml:"upstreams"`
}

// APIConfig points at the control-plane HTTP API used by `noket login` to
// exchange the authorization code for a token. The tunnel WebSocket URL is
// configured separately because it usually lives on another host.
type APIConfig struct {
	BaseURL string `toml:"base_url"`
}

// WebConfig points at the browser-facing web app that serves the login
// authorization page. It defaults to the production web host, independently of
// a custom API base.
type WebConfig struct {
	BaseURL string `toml:"base_url"`
}

type Tunnel struct {
	URL  string `toml:"url"`
	Name string `toml:"name"`
}

type NoketdConfig struct {
	ProbeTimeout         Duration `toml:"probe_timeout"`
	UpstreamWriteTimeout Duration `toml:"upstream_write_timeout"`
	ProbeMaxBytes        int64    `toml:"probe_max_bytes"`
}

// Upstream is one local LLM API. BaseURL may end with /v1: noketd joins the
// path it receives from the cloud without duplicating the version segment.
type Upstream struct {
	Name     string            `toml:"name"`
	Protocol string            `toml:"protocol"`
	BaseURL  string            `toml:"base_url"`
	APIKey   string            `toml:"api_key"`
	Headers  map[string]string `toml:"headers,omitempty"`

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
