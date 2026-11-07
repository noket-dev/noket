package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/types"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidateName checks upstream names, which also appear in admin URLs.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("upstream name is required")
	}
	if name == "." || name == ".." {
		return errors.New(`upstream name must not be "." or ".."`)
	}
	if !namePattern.MatchString(name) {
		return errors.New("upstream name may contain only letters, digits, '.', '_' and '-', up to 64 characters")
	}
	return nil
}

func validateProtocol(protocol string) error {
	switch protocol {
	case types.ProtocolOpenAI, types.ProtocolAnthropic:
		return nil
	default:
		return fmt.Errorf("protocol must be %q or %q", types.ProtocolOpenAI, types.ProtocolAnthropic)
	}
}

// Validate checks the whole config: tunnel URL, upstream names, protocols,
// addresses and global model uniqueness.
func Validate(cfg *types.Config) error {
	if strings.TrimSpace(cfg.Tunnel.URL) == "" {
		return errors.New("tunnel.url is required")
	}
	u, err := url.Parse(cfg.Tunnel.URL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return errors.New("tunnel.url must be a ws:// or wss:// URL")
	}
	if cfg.API.BaseURL != "" {
		parsed, err := url.Parse(cfg.API.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("api.base_url must be an http:// or https:// URL")
		}
	}
	if len(cfg.Tunnel.Name) > 128 {
		return errors.New("tunnel.name must be at most 128 characters")
	}
	if cfg.Noketd.ProbeTimeout <= 0 {
		return errors.New("noketd.probe_timeout must be positive")
	}
	if cfg.Noketd.UpstreamWriteTimeout <= 0 {
		return errors.New("noketd.upstream_write_timeout must be positive")
	}
	if cfg.Noketd.ProbeMaxBytes <= 0 {
		return errors.New("noketd.probe_max_bytes must be positive")
	}

	names := make(map[string]struct{}, len(cfg.Upstreams))
	models := make(map[string]string)
	for i := range cfg.Upstreams {
		up := &cfg.Upstreams[i]
		if err := validateUpstream(up); err != nil {
			return fmt.Errorf("upstream %q: %w", up.Name, err)
		}
		if _, ok := names[up.Name]; ok {
			return fmt.Errorf("upstream name %q is used twice", up.Name)
		}
		names[up.Name] = struct{}{}
		for _, model := range up.Models {
			if owner, ok := models[model]; ok {
				return fmt.Errorf("model id %q is declared by both %q and %q", model, owner, up.Name)
			}
			models[model] = up.Name
		}
	}
	return nil
}

// ValidateUpstream checks one upstream against the others in the file.
func ValidateUpstream(up *types.Upstream, others []types.Upstream) error {
	if err := validateUpstream(up); err != nil {
		return err
	}
	for _, other := range others {
		if other.Name == up.Name {
			continue
		}
		for _, model := range up.Models {
			for _, otherModel := range other.Models {
				if model == otherModel {
					return fmt.Errorf("model id %q is already declared by upstream %q", model, other.Name)
				}
			}
		}
	}
	return nil
}

func validateUpstream(up *types.Upstream) error {
	if err := ValidateName(up.Name); err != nil {
		return err
	}
	if err := validateProtocol(up.Protocol); err != nil {
		return err
	}
	if _, err := netguard.ParseBaseURL(up.BaseURL); err != nil {
		return err
	}
	if strings.ContainsAny(up.APIKey, "\r\n") {
		return errors.New("api_key must not contain newlines")
	}
	for name, value := range up.Headers {
		if err := validateHeader(name, value); err != nil {
			return err
		}
	}

	seen := make(map[string]struct{}, len(up.Models))
	for _, model := range up.Models {
		if strings.TrimSpace(model) == "" {
			return errors.New("model ids must not be empty")
		}
		if len(model) > 256 {
			return fmt.Errorf("model id %q is too long", model)
		}
		if strings.ContainsAny(model, "\r\n") {
			return fmt.Errorf("model id %q contains a newline", model)
		}
		if _, ok := seen[model]; ok {
			return fmt.Errorf("model id %q is listed twice", model)
		}
		seen[model] = struct{}{}
	}
	return nil
}

func validateHeader(name, value string) error {
	if name == "" {
		return errors.New("header name must not be empty")
	}
	if strings.ContainsAny(name, " \t\r\n:") {
		return fmt.Errorf("header name %q is invalid", name)
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return fmt.Errorf("header name %q must be ASCII", name)
		}
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("header %q must not contain a newline", name)
	}
	return nil
}

func decodeJWTPart(part string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(part); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(part)
}

// DecodeJWTSubject is used by the CLI and by status output. The signature is
// never checked locally; the server authenticates the token.
func DecodeJWTSubject(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("token is not a JWT")
	}
	payload, err := decodeJWTPart(parts[1])
	if err != nil {
		return "", err
	}
	claims := struct {
		Sub string `json:"sub"`
	}{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return claims.Sub, nil
}
