package forward_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"

	"github.com/noket-dev/noket/internal/forward"
	"github.com/noket-dev/noket/internal/types"
)

func TestExtractModel(t *testing.T) {
	cases := []struct {
		body    string
		want    string
		wantErr bool
	}{
		{body: `{"model":"qwen3:32b","messages":[]}`, want: "qwen3:32b"},
		{body: `{"model":"  "}`, wantErr: true},
		{body: `{"messages":[]}`, wantErr: true},
		{body: ``, wantErr: true},
		{body: `not json`, wantErr: true},
	}
	for _, tc := range cases {
		got, err := forward.ExtractModel([]byte(tc.body))
		if tc.wantErr {
			if err == nil {
				t.Errorf("ExtractModel(%q) = %q, want error", tc.body, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ExtractModel(%q) error: %v", tc.body, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ExtractModel(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

func TestFilterRequestHeaders(t *testing.T) {
	in := []*tunnelv1.Header{
		{Name: "Authorization", Value: "cloud-token"},
		{Name: "Host", Value: "example.com"},
		{Name: "Content-Length", Value: "12"},
		{Name: "X-Api-Key", Value: "cloud-key"},
		{Name: "X-Bf-Trace", Value: "abc"},
		{Name: "X-Bifrost-Request", Value: "1"},
		{Name: "Connection", Value: "keep-alive"},
		{Name: "Content-Type", Value: "application/json"},
		{Name: "X-Custom", Value: "kept"},
	}
	got := forward.FilterRequestHeaders(in)

	for _, dropped := range []string{"Authorization", "Host", "Content-Length", "X-Api-Key", "X-Bf-Trace", "X-Bifrost-Request", "Connection"} {
		if values := got.Values(dropped); len(values) != 0 {
			t.Errorf("%s survived filtering: %v", dropped, values)
		}
	}
	if got.Get("Content-Type") != "application/json" || got.Get("X-Custom") != "kept" {
		t.Fatalf("business headers were not kept: %v", got)
	}
}

func TestBuildRequestInjectsLocalAuth(t *testing.T) {
	open := &tunnelv1.ForwardOpen{
		RequestId: "req",
		Method:    http.MethodPost,
		Path:      "/v1/messages?beta=1",
		Headers: []*tunnelv1.Header{
			{Name: "Authorization", Value: "cloud"},
			{Name: "Content-Type", Value: "application/json"},
			{Name: "X-Custom", Value: "kept"},
		},
	}
	up := &types.Upstream{
		Name:     "claude-local",
		Protocol: types.ProtocolAnthropic,
		BaseURL:  "http://127.0.0.1:11434",
		APIKey:   "local-key",
		Headers:  map[string]string{"X-Fixed": "1"},
	}
	body := []byte(`{"model":"m"}`)

	req, err := forward.BuildRequest(open, up, body)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "http://127.0.0.1:11434/v1/messages?beta=1" {
		t.Fatalf("url = %q", got)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("cloud Authorization header leaked to the local upstream")
	}
	if req.Header.Get("x-api-key") != "local-key" {
		t.Fatalf("x-api-key = %q", req.Header.Get("x-api-key"))
	}
	if req.Header.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", req.Header.Get("anthropic-version"))
	}
	if req.Header.Get("X-Fixed") != "1" || req.Header.Get("X-Custom") != "kept" {
		t.Fatalf("headers = %v", req.Header)
	}
	sent, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(sent) != string(body) {
		t.Fatalf("body = %q, want %q", sent, body)
	}
}

func TestBuildRequestOpenAIAndInvalidPath(t *testing.T) {
	up := &types.Upstream{Protocol: types.ProtocolOpenAI, BaseURL: "http://127.0.0.1:11434", APIKey: "key"}
	req, err := forward.BuildRequest(&tunnelv1.ForwardOpen{Method: http.MethodGet, Path: "/v1/models"}, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer key" {
		t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
	}

	if _, err := forward.BuildRequest(&tunnelv1.ForwardOpen{Method: http.MethodGet, Path: "v1/models"}, up, nil); err == nil {
		t.Fatal("path without a leading slash was accepted")
	}
}

func TestBuildRequestAvoidsDuplicateV1(t *testing.T) {
	up := &types.Upstream{Protocol: types.ProtocolOpenAI, BaseURL: "https://api.example.com/v1"}
	req, err := forward.BuildRequest(&tunnelv1.ForwardOpen{Method: http.MethodPost, Path: "/v1/chat/completions"}, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("url = %q", got)
	}

	up.BaseURL = "https://api.example.com/openai/v1/"
	req, err = forward.BuildRequest(&tunnelv1.ForwardOpen{Method: http.MethodPost, Path: "/v1/chat/completions"}, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://api.example.com/openai/v1/chat/completions" {
		t.Fatalf("url = %q", got)
	}
}

func TestFilterResponseHeaders(t *testing.T) {
	in := http.Header{
		"Content-Type":      {"text/event-stream"},
		"Content-Length":    {"100"},
		"Transfer-Encoding": {"chunked"},
		"Connection":        {"keep-alive"},
	}
	got := forward.FilterResponseHeaders(in)

	names := map[string]bool{}
	for _, header := range got {
		names[strings.ToLower(header.Name)] = true
	}
	if !names["content-type"] {
		t.Fatal("Content-Type was dropped")
	}
	for _, dropped := range []string{"content-length", "transfer-encoding", "connection"} {
		if names[dropped] {
			t.Errorf("%s survived filtering", dropped)
		}
	}
}

func TestJSONError(t *testing.T) {
	body := string(forward.JSONError("model_not_found"))
	if !strings.Contains(body, "model_not_found") || !strings.Contains(body, "tunnel_error") {
		t.Fatalf("JSONError body = %s", body)
	}
}
