// Package upstream holds the rules for building, validating, saving, probing,
// and removing local upstreams. The CLI and the TUI both call it, so neither
// grows a second copy of the key semantics.
package upstream

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
)

// ErrStale means config.toml changed while an edit was open, so saving now
// would overwrite a change made by noketd or another command.
var ErrStale = errors.New("the upstream list changed on disk")

// Draft is the editable part of an upstream. A nil Key keeps the existing key,
// but never across a base URL change; a non-nil empty Key means "no key".
type Draft struct {
	Name     string
	Protocol string
	BaseURL  string
	Key      *string
	Models   []string
}

// Build applies the key rules and normalizes the address and protocol.
func Build(existing *types.Upstream, d Draft) (types.Upstream, error) {
	base, err := netguard.ParseBaseURL(d.BaseURL)
	if err != nil {
		return types.Upstream{}, err
	}
	protocol := strings.ToLower(strings.TrimSpace(d.Protocol))
	if protocol != types.ProtocolOpenAI && protocol != types.ProtocolAnthropic {
		return types.Upstream{}, fmt.Errorf("protocol must be %q or %q", types.ProtocolOpenAI, types.ProtocolAnthropic)
	}

	up := types.Upstream{
		Name:     strings.TrimSpace(d.Name),
		Protocol: protocol,
		BaseURL:  base.String(),
		Models:   Trim(d.Models),
	}
	if existing != nil {
		up.Headers = cloneStringMap(existing.Headers)
	}

	switch {
	case d.Key != nil:
		up.APIKey = *d.Key
	case existing != nil && existing.BaseURL == up.BaseURL:
		up.APIKey = existing.APIKey
	case existing != nil:
		return types.Upstream{}, errors.New("the Base URL changed; provide a new API key, or use --clear-api-key to mark this URL as needing no key")
	}

	return up, nil
}

// KeyPointer turns the answers of a key field into a Draft key: an entered key
// wins; otherwise an edited upstream keeps its key only while the URL is
// unchanged.
func KeyPointer(existing *types.Upstream, base, entered string) *string {
	switch {
	case entered != "":
		return &entered
	case existing != nil && existing.BaseURL == base:
		value := existing.APIKey
		return &value
	default:
		empty := ""
		return &empty
	}
}

// Save writes up into cfg and stores the config atomically. existing is the
// entry being replaced, or nil when adding.
func Save(cfg *types.Config, up types.Upstream, existing *types.Upstream) error {
	others := make([]types.Upstream, 0, len(cfg.Upstreams))
	for _, other := range cfg.Upstreams {
		if existing != nil && other.Name == existing.Name {
			continue
		}
		others = append(others, other)
	}
	if err := config.ValidateUpstream(&up, others); err != nil {
		return err
	}
	for _, other := range others {
		if other.Name == up.Name {
			return fmt.Errorf("upstream name %q is already in use", up.Name)
		}
	}

	if existing == nil {
		cfg.Upstreams = append(cfg.Upstreams, up)
		return config.Save(cfg)
	}
	index := -1
	for i := range cfg.Upstreams {
		if cfg.Upstreams[i].Name == existing.Name {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("upstream %q does not exist", existing.Name)
	}
	cfg.Upstreams[index] = up
	return config.Save(cfg)
}

// SaveFresh reloads config.toml, verifies the entry being edited is unchanged
// since expected was taken, and then saves. It returns the freshly loaded
// config even on failure so a caller can redraw from it. noketd writes this
// file in response to cloud commands, so an edit must never be applied blind.
func SaveFresh(up types.Upstream, expected *types.Upstream) (*types.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if expected != nil {
		current, ok := Find(cfg, expected.Name)
		if !ok || !reflect.DeepEqual(*current, *expected) {
			return cfg, ErrStale
		}
	}
	if err := Save(cfg, up, expected); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// RemoveFresh reloads config.toml, verifies the entry is unchanged since
// expected was taken, and then removes it.
func RemoveFresh(name string, expected *types.Upstream) (*types.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	current, ok := Find(cfg, name)
	if !ok || (expected != nil && !reflect.DeepEqual(*current, *expected)) {
		return cfg, ErrStale
	}
	if !Remove(cfg, name) {
		return cfg, fmt.Errorf("upstream %q does not exist", name)
	}
	if err := config.Save(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Find returns the named upstream, which points into cfg.
func Find(cfg *types.Config, name string) (*types.Upstream, bool) {
	for i := range cfg.Upstreams {
		if cfg.Upstreams[i].Name == name {
			return &cfg.Upstreams[i], true
		}
	}
	return nil, false
}

// Remove deletes the named upstream from cfg in memory and reports whether it
// was there.
func Remove(cfg *types.Config, name string) bool {
	for i := range cfg.Upstreams {
		if cfg.Upstreams[i].Name == name {
			cfg.Upstreams = append(cfg.Upstreams[:i], cfg.Upstreams[i+1:]...)
			return true
		}
	}
	return false
}

// Clone returns a deep copy, suitable as the expected snapshot for SaveFresh.
func Clone(up *types.Upstream) *types.Upstream {
	if up == nil {
		return nil
	}
	out := *up
	out.Models = append([]string(nil), up.Models...)
	out.Headers = cloneStringMap(up.Headers)
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
