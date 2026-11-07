package upstream

import (
	"errors"
	"reflect"
	"testing"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func TestBuildKeyRules(t *testing.T) {
	existing := &types.Upstream{
		Name:     "local",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		APIKey:   "old" + "-" + "key",
		Headers:  map[string]string{"X-Tenant": "a"},
	}

	up, err := Build(existing, Draft{
		Name: "local", Protocol: types.ProtocolOpenAI,
		BaseURL: "http://127.0.0.1:11434/", Models: []string{"m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "old"+"-"+"key" {
		t.Fatalf("unchanged address: key = %q, want the stored key", up.APIKey)
	}
	if up.Headers["X-Tenant"] != "a" {
		t.Fatalf("headers were not preserved: %v", up.Headers)
	}

	if _, err := Build(existing, Draft{
		Name: "local", Protocol: types.ProtocolOpenAI,
		BaseURL: "http://127.0.0.1:8000", Models: []string{"m"},
	}); err == nil {
		t.Fatal("changing the address without a key was accepted")
	}

	empty := ""
	up, err = Build(existing, Draft{
		Name: "local", Protocol: types.ProtocolAnthropic,
		BaseURL: "http://127.0.0.1:8000", Key: &empty, Models: []string{"m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "" {
		t.Fatalf("explicit empty key = %q", up.APIKey)
	}

	up, err = Build(nil, Draft{
		Name: "fresh", Protocol: types.ProtocolOpenAI,
		BaseURL: "http://127.0.0.1:11434", Models: []string{"m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.APIKey != "" {
		t.Fatalf("new upstream key = %q, want empty", up.APIKey)
	}
}

func TestBuildRejectsInvalidInput(t *testing.T) {
	if _, err := Build(nil, Draft{Name: "fresh", Protocol: "grpc", BaseURL: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("invalid protocol was accepted")
	}
	up, err := Build(nil, Draft{Name: "fresh", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatalf("base URL with /v1 was rejected: %v", err)
	}
	if up.BaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("base URL = %q, want it kept as entered", up.BaseURL)
	}
}

func TestKeyPointer(t *testing.T) {
	existing := &types.Upstream{BaseURL: "https://a.example/v1", APIKey: "old"}

	tests := []struct {
		name     string
		existing *types.Upstream
		base     string
		entered  string
		want     string
	}{
		{"entered wins", existing, "https://b.example/v1", "new", "new"},
		{"same URL keeps the key", existing, "https://a.example/v1", "", "old"},
		{"changed URL clears the key", existing, "https://b.example/v1", "", ""},
		{"add starts without a key", nil, "http://127.0.0.1:11434", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := KeyPointer(test.existing, test.base, test.entered)
			if got == nil || *got != test.want {
				t.Fatalf("KeyPointer = %v, want %q", got, test.want)
			}
		})
	}
}

func TestSave(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	first := types.Upstream{
		Name:     "ollama",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		Models:   []string{"qwen3:32b"},
	}
	if err := Save(cfg, first, nil); err != nil {
		t.Fatal(err)
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Upstreams) != 1 || reloaded.Upstreams[0].Name != "ollama" {
		t.Fatalf("saved upstreams = %+v", reloaded.Upstreams)
	}

	second := types.Upstream{
		Name:     "vllm",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:8000",
		Models:   []string{"mistral"},
	}
	if err := Save(reloaded, second, nil); err != nil {
		t.Fatal(err)
	}

	renamed := first
	renamed.Name = "vllm"
	if err := Save(reloaded, renamed, &reloaded.Upstreams[0]); err == nil {
		t.Fatal("renaming onto an existing name was accepted")
	}
}

func TestSaveFresh(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	original := types.Upstream{
		Name:     "ollama",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		Models:   []string{"qwen3:32b"},
	}
	if err := Save(cfg, original, nil); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := Clone(&loaded.Upstreams[0])

	// Another writer (noketd acting on a cloud command) changes the entry.
	loaded.Upstreams[0].Models = []string{"cloud-model"}
	if err := config.Save(loaded); err != nil {
		t.Fatal(err)
	}

	edited := *expected
	edited.Models = []string{"mine"}
	fresh, err := SaveFresh(edited, expected)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("SaveFresh over a changed entry = %v, want ErrStale", err)
	}
	if fresh == nil || len(fresh.Upstreams) != 1 || !reflect.DeepEqual(fresh.Upstreams[0].Models, []string{"cloud-model"}) {
		t.Fatalf("SaveFresh did not return the fresh config: %+v", fresh)
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(onDisk.Upstreams[0].Models, []string{"cloud-model"}) {
		t.Fatalf("disk models = %v, want noketd's write kept", onDisk.Upstreams[0].Models)
	}
}

func TestSaveFreshAppliesAnUnchangedEdit(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	original := types.Upstream{
		Name:     "ollama",
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		Models:   []string{"a"},
	}
	if err := Save(cfg, original, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := Clone(&loaded.Upstreams[0])

	edited := *expected
	edited.Models = []string{"a", "b"}
	if _, err := SaveFresh(edited, expected); err != nil {
		t.Fatalf("SaveFresh: %v", err)
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(onDisk.Upstreams[0].Models, []string{"a", "b"}) {
		t.Fatalf("disk models = %v", onDisk.Upstreams[0].Models)
	}
}

func TestRemoveFresh(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	up := types.Upstream{Name: "ollama", Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:11434", Models: []string{"a"}}
	if err := Save(cfg, up, nil); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := Clone(&loaded.Upstreams[0])
	if _, err := RemoveFresh("ollama", expected); err != nil {
		t.Fatalf("RemoveFresh: %v", err)
	}
	after, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Upstreams) != 0 {
		t.Fatalf("upstreams after remove = %+v", after.Upstreams)
	}

	// The entry is gone, so a second removal from the stale snapshot fails.
	if _, err := RemoveFresh("ollama", expected); !errors.Is(err, ErrStale) {
		t.Fatalf("second RemoveFresh = %v, want ErrStale", err)
	}
}

func TestRemove(t *testing.T) {
	cfg := &types.Config{Upstreams: []types.Upstream{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	if !Remove(cfg, "b") {
		t.Fatal("Remove reported a missing upstream")
	}
	got := make([]string, len(cfg.Upstreams))
	for i, up := range cfg.Upstreams {
		got[i] = up.Name
	}
	if !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("remaining upstreams = %v", got)
	}
	if Remove(cfg, "b") {
		t.Fatal("removing a missing upstream returned true")
	}
}

func TestParseList(t *testing.T) {
	got := ParseList(" a, b ,a,, c ")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseList = %v, want %v", got, want)
	}
}

func TestUnion(t *testing.T) {
	got := Union([]string{"probed", "shared"}, []string{"shared", "manual"})
	want := []string{"probed", "shared", "manual"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Union = %v, want %v", got, want)
	}
}

func TestChoicesPreselectsEverythingOnAdd(t *testing.T) {
	items, selected := Choices([]string{"a", "b"}, nil, false)
	if !reflect.DeepEqual(items, []string{"a", "b"}) {
		t.Fatalf("items = %v", items)
	}
	if !reflect.DeepEqual(selected, []string{"a", "b"}) {
		t.Fatalf("selected = %v, want every probed model", selected)
	}
}

func TestChoicesKeepsThePreviousSelectionOnEdit(t *testing.T) {
	existing := &types.Upstream{Models: []string{"b", "manual"}}
	items, selected := Choices([]string{"a", "b"}, existing, true)

	wantItems := []string{"b", "manual", "a"}
	if !reflect.DeepEqual(items, wantItems) {
		t.Fatalf("items = %v, want %v", items, wantItems)
	}
	if !reflect.DeepEqual(selected, []string{"b", "manual"}) {
		t.Fatalf("selected = %v", selected)
	}
}

// huh scrolls a multi-select to its first selected option, so the first entry
// must be one of the preselected ones or the list opens mid-way.
func TestChoicesStartsWithASelectedEntryOnEdit(t *testing.T) {
	items, selected := Choices([]string{"a", "b", "c"}, &types.Upstream{Models: []string{"c"}}, true)
	if len(selected) == 0 {
		t.Fatal("nothing is preselected")
	}
	if items[0] != selected[0] {
		t.Fatalf("first item %q is not selected (%v)", items[0], selected)
	}
}
