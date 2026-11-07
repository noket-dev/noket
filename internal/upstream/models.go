package upstream

import (
	"context"
	"strings"

	"github.com/noket-dev/noket/internal/probe"
	"github.com/noket-dev/noket/internal/types"
)

// Probe requests the upstream's model list with the timeouts from cfg.
func Probe(ctx context.Context, cfg *types.Config, up types.Upstream) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return probe.Models(ctx, probe.Options{
		Protocol: up.Protocol,
		BaseURL:  up.BaseURL,
		APIKey:   up.APIKey,
		Headers:  up.Headers,
		Timeout:  cfg.Noketd.ProbeTimeout.Std(),
		MaxBytes: cfg.Noketd.ProbeMaxBytes,
	})
}

// Existing returns the models already stored on an upstream, both as the list
// to show and as the preselected set.
func Existing(up *types.Upstream) (items, selected []string) {
	if up == nil {
		return nil, nil
	}
	return append([]string(nil), up.Models...), append([]string(nil), up.Models...)
}

// Union keeps the probed order and appends models that exist only in the local
// config, so an edit can keep a manually added model.
func Union(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	out := make([]string, 0, len(primary)+len(secondary))
	for _, list := range [][]string{primary, secondary} {
		for _, model := range list {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			out = append(out, model)
		}
	}
	return out
}

// ParseList splits a comma separated model list and drops blanks and repeats.
func ParseList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

// Trim strips surrounding whitespace and drops blank model ids.
func Trim(models []string) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			out = append(out, model)
		}
	}
	return out
}

// Choices preselects everything on add or the previous selection on edit. On
// edit the previous models come first: huh scrolls a multi-select to its first
// selected option, so a selection further down would open the list scrolled to
// that row instead of at the top.
func Choices(found []string, existing *types.Upstream, editing bool) (items, selected []string) {
	previous, selected := Existing(existing)
	if !editing {
		items := Union(found, previous)
		return items, items
	}
	return Union(previous, found), selected
}
