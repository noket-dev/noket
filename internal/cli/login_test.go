package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	noketv1 "github.com/noket-dev/noket/gen/noket/v1"
	"github.com/noket-dev/noket/gen/noket/v1/noketv1connect"
	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

type authServiceStub struct {
	noketv1connect.UnimplementedAuthServiceHandler
	exchange func(*connect.Request[noketv1.ExchangeCliAuthorizationCodeRequest]) (*connect.Response[noketv1.ExchangeCliAuthorizationCodeResponse], error)
}

func (s *authServiceStub) ExchangeCliAuthorizationCode(
	_ context.Context,
	request *connect.Request[noketv1.ExchangeCliAuthorizationCodeRequest],
) (*connect.Response[noketv1.ExchangeCliAuthorizationCodeResponse], error) {
	return s.exchange(request)
}

func newAuthServer(t *testing.T, stub *authServiceStub) *httptest.Server {
	t.Helper()

	_, handler := noketv1connect.NewAuthServiceHandler(stub)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestExchangeCode(t *testing.T) {
	var gotPath, gotContentType string
	var gotRequest *noketv1.ExchangeCliAuthorizationCodeRequest

	stub := &authServiceStub{
		exchange: func(request *connect.Request[noketv1.ExchangeCliAuthorizationCodeRequest]) (*connect.Response[noketv1.ExchangeCliAuthorizationCodeResponse], error) {
			gotRequest = request.Msg
			return connect.NewResponse(&noketv1.ExchangeCliAuthorizationCodeResponse{Token: "issued-token"}), nil
		},
	}

	_, connectHandler := noketv1connect.NewAuthServiceHandler(stub)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		connectHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	token, err := exchangeCode(context.Background(), server.URL, "code-1", "verifier-1", "http://127.0.0.1:1/callback")
	if err != nil {
		t.Fatal(err)
	}
	if token != "issued-token" {
		t.Fatalf("token = %q", token)
	}
	if gotPath != "/noket.v1.AuthService/ExchangeCliAuthorizationCode" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotContentType != "application/proto" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if gotRequest.GetCode() != "code-1" || gotRequest.GetCodeVerifier() != "verifier-1" {
		t.Fatalf("request = %v", gotRequest)
	}
	if gotRequest.GetRedirectUri() != "http://127.0.0.1:1/callback" {
		t.Fatalf("redirect_uri = %q", gotRequest.GetRedirectUri())
	}
}

func TestExchangeCodeWithoutToken(t *testing.T) {
	stub := &authServiceStub{
		exchange: func(_ *connect.Request[noketv1.ExchangeCliAuthorizationCodeRequest]) (*connect.Response[noketv1.ExchangeCliAuthorizationCodeResponse], error) {
			return connect.NewResponse(&noketv1.ExchangeCliAuthorizationCodeResponse{}), nil
		},
	}
	server := newAuthServer(t, stub)

	_, err := exchangeCode(context.Background(), server.URL, "code", "verifier", "http://127.0.0.1/callback")
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Fatalf("error = %v, want a missing token error", err)
	}
}

func TestExchangeCodeError(t *testing.T) {
	stub := &authServiceStub{
		exchange: func(_ *connect.Request[noketv1.ExchangeCliAuthorizationCodeRequest]) (*connect.Response[noketv1.ExchangeCliAuthorizationCodeResponse], error) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid code"))
		},
	}
	server := newAuthServer(t, stub)

	_, err := exchangeCode(context.Background(), server.URL, "code", "verifier", "http://127.0.0.1/callback")
	if err == nil || !strings.Contains(err.Error(), "invalid code") {
		t.Fatalf("error = %v, want the server message", err)
	}
}

func TestReportExistingLogin(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		t.Setenv(config.DirEnv, t.TempDir())

		var out bytes.Buffer
		reportExistingLogin(&out)
		if out.Len() != 0 {
			t.Fatalf("output = %q, want none", out.String())
		}
	})

	t.Run("saved session", func(t *testing.T) {
		t.Setenv(config.DirEnv, t.TempDir())
		savedAt := time.Date(2026, 11, 3, 9, 12, 0, 0, time.UTC)
		if err := config.SaveCredentials(&types.Credentials{
			Token:   "token",
			UserID:  "user-1",
			SavedAt: savedAt,
		}); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		reportExistingLogin(&out)
		text := out.String()
		if !strings.Contains(text, "Already logged in as user-1") {
			t.Fatalf("output = %q, want the saved user", text)
		}
		if !strings.Contains(text, "credentials saved "+savedAt.Local().Format("2006-01-02 15:04")) {
			t.Fatalf("output = %q, want the save time", text)
		}
		if !strings.Contains(text, "replaces the saved credentials") {
			t.Fatalf("output = %q, want the replacement notice", text)
		}
	})

	t.Run("falls back to the token subject", func(t *testing.T) {
		t.Setenv(config.DirEnv, t.TempDir())
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-2"}`))
		if err := config.SaveCredentials(&types.Credentials{Token: "header." + payload + ".signature"}); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		reportExistingLogin(&out)
		if text := out.String(); !strings.Contains(text, "Already logged in as user-2") {
			t.Fatalf("output = %q, want the subject from the token", text)
		}
	})

	t.Run("unreadable credentials", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(config.DirEnv, dir)
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		reportExistingLogin(&out)
		text := out.String()
		if !strings.Contains(text, "could not be read") || !strings.Contains(text, "will replace them") {
			t.Fatalf("output = %q, want a warning that login continues", text)
		}
	})
}

func TestRandomTokenHelpers(t *testing.T) {
	verifier, err := randomURLToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) != 43 || strings.Contains(verifier, "=") {
		t.Fatalf("verifier = %q, want 43 unpadded base64url characters", verifier)
	}

	state, err := randomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 32 {
		t.Fatalf("state length = %d, want 32", len(state))
	}
}
