package config_test

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func sampleConfig() *types.Config {
	cfg := config.Default()
	cfg.Upstreams = []types.Upstream{{
		Name:     "ollama",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		APIKey:   "secret",
		Headers:  map[string]string{"x-tenant": "a"},
		Models:   []string{"qwen3:32b"},
	}}
	return cfg
}

// requirePrivateFile checks that path is readable only by its owner. Windows
// only tracks the read-only bit and reports 0666 for every new file, so the
// mode is only meaningful on Unix.
func requirePrivateFile(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("%s permission = %o, want 600", filepath.Base(path), perm)
	}
}

func TestEnsureNodePersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	first, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatalf("first EnsureNode: %v", err)
	}
	second, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatalf("second EnsureNode: %v", err)
	}
	if first.ID == "" || first.UserID != "user-a" || first.ID != second.ID {
		t.Fatalf("node id not stable: %+v then %+v", first, second)
	}

	requirePrivateFile(t, filepath.Join(dir, "node.json"))
}

func TestEnsureNodeSeparatesAccounts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	a, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := config.EnsureNode("user-b")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatalf("both accounts got the same node id %q", a.ID)
	}

	again, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID {
		t.Fatalf("switching back changed user-a's node id: %q then %q", a.ID, again.ID)
	}

	read, err := config.ReadNode("user-b")
	if err != nil {
		t.Fatal(err)
	}
	if read.ID != b.ID || read.UserID != "user-b" {
		t.Fatalf("ReadNode(user-b) = %+v, want %q", read, b.ID)
	}
	if _, err := config.ReadNode("user-c"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadNode(user-c) error = %v, want os.ErrNotExist", err)
	}
}

func TestRotateNode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	a, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := config.EnsureNode("user-b")
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := config.RotateNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID == a.ID {
		t.Fatal("RotateNode returned the old node id")
	}

	kept, err := config.ReadNode("user-b")
	if err != nil {
		t.Fatal(err)
	}
	if kept.ID != b.ID {
		t.Fatalf("rotating user-a changed user-b: %q then %q", b.ID, kept.ID)
	}

	again, err := config.EnsureNode("user-a")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != rotated.ID {
		t.Fatalf("EnsureNode after rotation = %q, want %q", again.ID, rotated.ID)
	}
}

func TestCredentialsRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	creds := &types.Credentials{Token: "jwt", UserID: "user-1"}
	if err := config.SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Token != "jwt" || loaded.UserID != "user-1" {
		t.Fatalf("unexpected credentials: %+v", loaded)
	}

	requirePrivateFile(t, filepath.Join(dir, "credentials.json"))

	if err := config.DeleteCredentials(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadCredentials(); !errors.Is(err, config.ErrNoCredentials) {
		t.Fatalf("LoadCredentials after delete = %v, want ErrNoCredentials", err)
	}
}

func TestSaveConfigPermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	if err := config.Save(sampleConfig()); err != nil {
		t.Fatal(err)
	}
	requirePrivateFile(t, filepath.Join(dir, "config.toml"))
}

func TestConfigTOMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg := sampleConfig()
	cfg.Noketd.ProbeTimeout = types.Duration(3 * time.Second)
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[upstreams]]") {
		t.Fatalf("saved config is not TOML:\n%s", data)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Noketd.ProbeTimeout.Std() != 3*time.Second {
		t.Fatalf("probe timeout = %v, want 3s", loaded.Noketd.ProbeTimeout.Std())
	}
	if len(loaded.Upstreams) != 1 {
		t.Fatalf("loaded %d upstreams, want 1", len(loaded.Upstreams))
	}
	up := loaded.Upstreams[0]
	if up.APIKey != "secret" || up.Headers["x-tenant"] != "a" {
		t.Fatalf("upstream = %+v", up)
	}
	if len(up.Models) != 1 || up.Models[0] != "qwen3:32b" {
		t.Fatalf("models = %v", up.Models)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[api]\nbase_urll = 'http://127.0.0.1:1'\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadFile with unknown field = %v", err)
	}
}

func TestValidateRejectsDuplicateModelsAndNames(t *testing.T) {
	cfg := config.Default()
	cfg.Upstreams = []types.Upstream{
		{Name: "one", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:1", Models: []string{"shared"}},
		{Name: "two", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:2", Models: []string{"shared"}},
	}
	if err := config.Validate(cfg); err == nil || !strings.Contains(err.Error(), "declared by both") {
		t.Fatalf("duplicate model error = %v", err)
	}

	cfg.Upstreams[1].Models = []string{"other"}
	cfg.Upstreams[1].Name = "one"
	if err := config.Validate(cfg); err == nil || !strings.Contains(err.Error(), "used twice") {
		t.Fatalf("duplicate name error = %v", err)
	}
}

func TestValidateName(t *testing.T) {
	for _, name := range []string{"ollama", "my-vllm_1.2", strings.Repeat("a", 64)} {
		if err := config.ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "has space", "ＡＢ", strings.Repeat("a", 65)} {
		if err := config.ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) succeeded, want error", name)
		}
	}
}

func TestSubject(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-1"}`))
	token := "header." + payload + ".signature"
	if got := config.Subject(token); got != "user-1" {
		t.Fatalf("Subject = %q, want user-1", got)
	}
	if got := config.Subject("not-a-jwt"); got != "" {
		t.Fatalf("Subject(invalid) = %q, want empty", got)
	}
}

func TestStoreReloadAndKeepOldSnapshotOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.SaveFile(path, sampleConfig()); err != nil {
		t.Fatal(err)
	}

	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.ReloadIfChanged(); err != nil || changed {
		t.Fatalf("unchanged reload = %v, %v", changed, err)
	}

	next := config.Default()
	next.Upstreams = []types.Upstream{{
		Name: "vllm", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:8000", Models: []string{"mistral"},
	}}
	if err := config.SaveFile(path, next); err != nil {
		t.Fatal(err)
	}
	changed, err := store.ReloadIfChanged()
	if err != nil || !changed {
		t.Fatalf("changed reload = %v, %v", changed, err)
	}
	if _, ok := store.Snapshot().UpstreamByModel("mistral"); !ok {
		t.Fatal("new snapshot is missing the new model")
	}

	bad := []byte("[[upstreams]]\nname = 'broken'\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReloadIfChanged(); err == nil {
		t.Fatal("invalid config reload succeeded, want error")
	}
	if _, ok := store.Snapshot().UpstreamByModel("mistral"); !ok {
		t.Fatal("old snapshot was discarded after a failed reload")
	}
}

func TestStoreMutate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.SaveFile(path, sampleConfig()); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	err = store.Mutate(func(cfg *types.Config) error {
		cfg.Upstreams = append(cfg.Upstreams, types.Upstream{
			Name: "vllm", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:8000", Models: []string{"mistral"},
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Upstreams) != 2 {
		t.Fatalf("saved %d upstreams, want 2", len(reloaded.Upstreams))
	}
	if _, ok := store.Snapshot().UpstreamByModel("mistral"); !ok {
		t.Fatal("snapshot was not refreshed after Mutate")
	}
}

func TestApplyDefaultsSplitsWebAndAPI(t *testing.T) {
	production := config.Default()
	if production.API.BaseURL != "https://api.noket.dev" || production.Web.BaseURL != "https://noket.dev" {
		t.Fatalf("production defaults = api %q, web %q", production.API.BaseURL, production.Web.BaseURL)
	}

	local := &types.Config{
		API: types.APIConfig{BaseURL: "http://127.0.0.1:3000"},
		Web: types.WebConfig{BaseURL: "http://127.0.0.1:3000"},
	}
	config.ApplyDefaults(local)
	if local.API.BaseURL != "http://127.0.0.1:3000" || local.Web.BaseURL != "http://127.0.0.1:3000" {
		t.Fatalf("local bases = api %q, web %q", local.API.BaseURL, local.Web.BaseURL)
	}

	split := &types.Config{
		API: types.APIConfig{BaseURL: "https://api.noket.dev"},
		Web: types.WebConfig{BaseURL: "https://noket.dev/"},
	}
	config.ApplyDefaults(split)
	config.Normalize(split)
	if split.Web.BaseURL != "https://noket.dev" {
		t.Fatalf("explicit web base = %q", split.Web.BaseURL)
	}
}

func TestValidateRejectsInvalidWebBase(t *testing.T) {
	cfg := sampleConfig()
	cfg.Web.BaseURL = "ftp://noket.dev"
	if err := config.Validate(cfg); err == nil || !strings.Contains(err.Error(), "web.base_url") {
		t.Fatalf("invalid web base error = %v", err)
	}
}
