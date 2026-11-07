package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"

	"github.com/noket-dev/noket/internal/types"
)

const (
	defaultAPIBaseURL     = "https://api.noket.dev"
	defaultWebBaseURL     = "https://noket.dev"
	defaultTunnelURL      = "wss://tunnel.noket.dev/ws"
	defaultProbeTimeout   = 10 * time.Second
	defaultWriteTimeout   = 10 * time.Second
	defaultProbeMaxBytes  = 1 << 20
	credentialsFileMode   = 0o600
	configurationFileMode = 0o600
	statusFileMode        = 0o600
)

// Default returns the config used when config.toml does not exist yet.
func Default() *types.Config {
	cfg := &types.Config{}
	ApplyDefaults(cfg)
	return cfg
}

// ApplyDefaults fills unset fields without touching explicit values.
func ApplyDefaults(cfg *types.Config) {
	if cfg.API.BaseURL == "" {
		cfg.API.BaseURL = defaultAPIBaseURL
	}
	if strings.TrimSpace(cfg.Web.BaseURL) == "" {
		cfg.Web.BaseURL = defaultWebBaseURL
	}
	if cfg.Tunnel.URL == "" {
		cfg.Tunnel.URL = defaultTunnelURL
	}
	if cfg.Noketd.ProbeTimeout == 0 {
		cfg.Noketd.ProbeTimeout = types.Duration(defaultProbeTimeout)
	}
	if cfg.Noketd.UpstreamWriteTimeout == 0 {
		cfg.Noketd.UpstreamWriteTimeout = types.Duration(defaultWriteTimeout)
	}
	if cfg.Noketd.ProbeMaxBytes == 0 {
		cfg.Noketd.ProbeMaxBytes = defaultProbeMaxBytes
	}
}

// Load reads config.toml. A missing file yields the default config, so a
// daemon with no upstreams can still start and register.
func Load() (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

func LoadFile(path string) (*types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	cfg := &types.Config{}
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		// StrictMissingError's message names every unknown key with the
		// offending line, which %v alone does not show.
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			err = errors.New(strict.String())
		}
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	ApplyDefaults(cfg)
	Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Save validates the config and atomically replaces config.toml.
func Save(cfg *types.Config) error {
	ApplyDefaults(cfg)
	Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return SaveFile(path, cfg)
}

func SaveFile(path string, cfg *types.Config) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return atomicWrite(path, data, configurationFileMode)
}

// Normalize trims incidental whitespace and trailing slashes so repeated
// edits do not accumulate cosmetic differences.
func Normalize(cfg *types.Config) {
	cfg.API.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.API.BaseURL), "/")
	cfg.Web.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Web.BaseURL), "/")
	cfg.Tunnel.URL = strings.TrimSpace(cfg.Tunnel.URL)
	cfg.Tunnel.Name = strings.TrimSpace(cfg.Tunnel.Name)

	for i := range cfg.Upstreams {
		up := &cfg.Upstreams[i]
		up.Name = strings.TrimSpace(up.Name)
		up.Protocol = strings.ToLower(strings.TrimSpace(up.Protocol))
		up.BaseURL = strings.TrimRight(strings.TrimSpace(up.BaseURL), "/")
		up.APIKey = strings.TrimSpace(up.APIKey)
		for j := range up.Models {
			up.Models[j] = strings.TrimSpace(up.Models[j])
		}
		if up.Headers == nil {
			up.Headers = map[string]string{}
		}
	}
}

// LoadCredentials reads the local login state.
func LoadCredentials() (*types.Credentials, error) {
	path, err := Path(credsFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoCredentials
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	creds := &types.Credentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if creds.Token == "" {
		return nil, ErrNoCredentials
	}
	return creds, nil
}

func SaveCredentials(creds *types.Credentials) error {
	if creds.Token == "" {
		return errors.New("credentials token is empty")
	}
	if creds.SavedAt.IsZero() {
		creds.SavedAt = time.Now().UTC()
	}
	path, err := Path(credsFile)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data, credentialsFileMode)
}

func DeleteCredentials() error {
	path, err := Path(credsFile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete credentials: %w", err)
	}
	return nil
}

const nodeFileVersion = 1

// nodeIndex keeps one node identity per account. The server binds a node id to
// the account that first registered it, so two accounts sharing one node.json
// cannot both register; each account gets its own entry instead.
type nodeIndex struct {
	Version int               `json:"version"`
	Nodes   map[string]string `json:"nodes"`
}

var errNodeIndexCorrupt = errors.New("node identity file is corrupt")

func readNodeIndex() (*nodeIndex, error) {
	path, err := Path(nodeFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &nodeIndex{Version: nodeFileVersion, Nodes: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("read node identity: %w", err)
	}

	file := &nodeIndex{}
	if err := json.Unmarshal(data, file); err != nil {
		return nil, errNodeIndexCorrupt
	}
	if file.Nodes == nil {
		file.Nodes = map[string]string{}
	}
	return file, nil
}

func saveNodeIndex(file *nodeIndex) error {
	path, err := Path(nodeFile)
	if err != nil {
		return err
	}
	file.Version = nodeFileVersion
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return atomicWrite(path, encoded, 0o600)
}

// EnsureNode returns the persisted node identity for owner, generating and
// saving a new UUID v7 when that account has none yet.
func EnsureNode(owner string) (*types.Node, error) {
	file, err := readNodeIndex()
	if err != nil {
		if !errors.Is(err, errNodeIndexCorrupt) {
			return nil, err
		}
		file = &nodeIndex{Nodes: map[string]string{}}
	}

	if id := file.Nodes[owner]; id != "" {
		if _, err := uuid.Parse(id); err == nil {
			return &types.Node{ID: id, UserID: owner}, nil
		}
	}

	node, err := newNode(owner)
	if err != nil {
		return nil, err
	}
	file.Nodes[owner] = node.ID
	if err := saveNodeIndex(file); err != nil {
		return nil, err
	}
	return node, nil
}

// RotateNode replaces owner's identity with a new one, keeping the entries of
// other accounts. It is used when the server reports that the saved node id is
// registered to a different account and cannot be reused.
func RotateNode(owner string) (*types.Node, error) {
	file, err := readNodeIndex()
	if err != nil {
		if !errors.Is(err, errNodeIndexCorrupt) {
			return nil, err
		}
		file = &nodeIndex{Nodes: map[string]string{}}
	}

	node, err := newNode(owner)
	if err != nil {
		return nil, err
	}
	file.Nodes[owner] = node.ID
	if err := saveNodeIndex(file); err != nil {
		return nil, err
	}
	return node, nil
}

func newNode(owner string) (*types.Node, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate node id: %w", err)
	}
	return &types.Node{ID: id.String(), UserID: owner}, nil
}

// ReadNode returns the saved identity for owner without creating one.
func ReadNode(owner string) (*types.Node, error) {
	file, err := readNodeIndex()
	if err != nil {
		return nil, err
	}
	id, ok := file.Nodes[owner]
	if !ok {
		return nil, os.ErrNotExist
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid node id: %w", err)
	}
	return &types.Node{ID: id, UserID: owner}, nil
}

// WriteStatus records the noketd state for `noket status` and for `noket up`
// while it waits for the first registration.
func WriteStatus(status *types.DaemonStatus) error {
	path, err := Path(statusFile)
	if err != nil {
		return err
	}
	status.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, statusFileMode)
}

func ReadStatus() (*types.DaemonStatus, error) {
	path, err := Path(statusFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoStatus
		}
		return nil, err
	}
	status := &types.DaemonStatus{}
	if err := json.Unmarshal(data, status); err != nil {
		return nil, fmt.Errorf("parse noketd status: %w", err)
	}
	return status, nil
}

func RemoveStatus() {
	if path, err := Path(statusFile); err == nil {
		_ = os.Remove(path)
	}
}

// Owner returns the account a credential set belongs to. Older credentials may
// not have user_id saved, so the subject claim is the fallback.
func Owner(creds *types.Credentials) string {
	if creds == nil {
		return ""
	}
	if creds.UserID != "" {
		return creds.UserID
	}
	return Subject(creds.Token)
}

// Subject extracts `sub` from an unverified JWT payload for display only.
// Authentication always happens on the server.
func Subject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := decodeJWTPart(parts[1])
	if err != nil {
		return ""
	}
	claims := struct {
		Sub string `json:"sub"`
	}{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}
