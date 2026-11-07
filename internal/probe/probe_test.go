package probe_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/noket-dev/noket/internal/probe"
	"github.com/noket-dev/noket/internal/rpc"
)

func TestModelsOpenAI(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"beta"},{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer server.Close()

	models, err := probe.Models(context.Background(), probe.Options{
		Protocol: "openai",
		BaseURL:  server.URL + "/",
		APIKey:   "sk-" + "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer "+"sk-"+"local" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	want := []string{"beta", "alpha"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestModelsAcceptsV1BaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"data":[{"id":"alpha"}]}`)
	}))
	defer server.Close()

	models, err := probe.Models(context.Background(), probe.Options{
		Protocol: "openai",
		BaseURL:  server.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if !reflect.DeepEqual(models, []string{"alpha"}) {
		t.Fatalf("models = %v", models)
	}
}

func TestModelsAnthropicHeaders(t *testing.T) {
	var key, version string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		_, _ = io.WriteString(w, `{"data":[{"id":"claude"}]}`)
	}))
	defer server.Close()

	if _, err := probe.Models(context.Background(), probe.Options{
		Protocol: "anthropic",
		BaseURL:  server.URL,
		APIKey:   "ak",
	}); err != nil {
		t.Fatal(err)
	}
	if key != "ak" {
		t.Fatalf("x-api-key = %q", key)
	}
	if version != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", version)
	}

	if _, err := probe.Models(context.Background(), probe.Options{
		Protocol: "anthropic",
		BaseURL:  server.URL,
		APIKey:   "ak",
		Headers:  map[string]string{"anthropic-version": "2025-01-01"},
	}); err != nil {
		t.Fatal(err)
	}
	if version != "2025-01-01" {
		t.Fatalf("configured anthropic-version was not applied: %q", version)
	}
}

func TestModelsLegacyShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"models":[{"name":"llama3.1:8b"},{"model":"qwen3:32b"}]}`)
	}))
	defer server.Close()

	models, err := probe.Models(context.Background(), probe.Options{
		Protocol: "openai",
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"llama3.1:8b", "qwen3:32b"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestModelsErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		maxBytes int64
		wantCode int32
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantCode: rpc.CodeUnavailable},
		{name: "no models", status: http.StatusOK, body: `{"data":[]}`, wantCode: rpc.CodeNotFound},
		{name: "invalid json", status: http.StatusOK, body: `not json`, wantCode: rpc.CodeUnavailable},
		{name: "too large", status: http.StatusOK, body: strings.Repeat("x", 2048), maxBytes: 1024, wantCode: rpc.CodeInvalidArgument},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			_, err := probe.Models(context.Background(), probe.Options{
				Protocol: "openai",
				BaseURL:  server.URL,
				MaxBytes: tc.maxBytes,
			})
			if err == nil {
				t.Fatal("probe succeeded, want error")
			}
			var rpcErr *rpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("error type = %T (%v), want *rpc.Error", err, err)
			}
			if rpcErr.Code != tc.wantCode {
				t.Fatalf("code = %d (%s), want %d", rpcErr.Code, rpcErr.Message, tc.wantCode)
			}
		})
	}
}
