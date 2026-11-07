package netguard_test

import (
	"context"
	"testing"
	"time"

	"github.com/noket-dev/noket/internal/netguard"
)

func TestParseBaseURL(t *testing.T) {
	userinfo := "http://" + "user" + ":" + "pass" + "@127.0.0.1"

	cases := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "http://127.0.0.1:11434", want: "http://127.0.0.1:11434"},
		{raw: " http://localhost:8000/ ", want: "http://localhost:8000"},
		{raw: "https://10.0.0.2/llm/", want: "https://10.0.0.2/llm"},
		{raw: "http://127.0.0.1:11434/v1", want: "http://127.0.0.1:11434/v1"},
		{raw: "http://127.0.0.1:11434/api/v1/", want: "http://127.0.0.1:11434/api/v1"},
		{raw: "ftp://127.0.0.1", wantErr: true},
		{raw: userinfo, wantErr: true},
		{raw: "http://127.0.0.1?x=1", wantErr: true},
		{raw: "http://127.0.0.1/#frag", wantErr: true},
		{raw: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := netguard.ParseBaseURL(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseBaseURL(%q) = %q, want error", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBaseURL(%q) error: %v", tc.raw, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("ParseBaseURL(%q) = %q, want %q", tc.raw, got.String(), tc.want)
		}
	}
}

func TestJoinPath(t *testing.T) {
	cases := []struct {
		base, request, want string
	}{
		{base: "", request: "/v1/models", want: "/v1/models"},
		{base: "/", request: "/v1/models", want: "/v1/models"},
		{base: "/openai", request: "/v1/chat/completions", want: "/openai/v1/chat/completions"},
		{base: "/v1", request: "/v1/models", want: "/v1/models"},
		{base: "/v1/", request: "/v1/chat/completions", want: "/v1/chat/completions"},
		{base: "/openai/v1", request: "/v1/chat/completions", want: "/openai/v1/chat/completions"},
		{base: "/v1", request: "/v1", want: "/v1"},
		{base: "/v1", request: "/health", want: "/v1/health"},
	}
	for _, tc := range cases {
		if got := netguard.JoinPath(tc.base, tc.request); got != tc.want {
			t.Errorf("JoinPath(%q, %q) = %q, want %q", tc.base, tc.request, got, tc.want)
		}
	}
}

func TestResolveAcceptsAnyAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, host := range []string{"127.0.0.1", "8.8.8.8", "169.254.169.254", "::1"} {
		ips, err := netguard.Resolve(ctx, host)
		if err != nil || len(ips) != 1 {
			t.Errorf("Resolve(%s) = %v, %v", host, ips, err)
		}
	}
}
