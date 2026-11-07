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

const testAuthorizationID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var testAuthorizationSecret = strings.Repeat("b", 43)

type authServiceStub struct {
	noketv1connect.UnimplementedAuthServiceHandler
	create func(*connect.Request[noketv1.CreateCliAuthorizationRequest]) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error)
	poll   func(*connect.Request[noketv1.PollCliAuthorizationRequest]) (*connect.Response[noketv1.PollCliAuthorizationResponse], error)
}

func (s *authServiceStub) CreateCliAuthorization(
	_ context.Context,
	request *connect.Request[noketv1.CreateCliAuthorizationRequest],
) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error) {
	return s.create(request)
}

func (s *authServiceStub) PollCliAuthorization(
	_ context.Context,
	request *connect.Request[noketv1.PollCliAuthorizationRequest],
) (*connect.Response[noketv1.PollCliAuthorizationResponse], error) {
	return s.poll(request)
}

func newAuthServer(t *testing.T, stub *authServiceStub) *httptest.Server {
	t.Helper()

	_, handler := noketv1connect.NewAuthServiceHandler(stub)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// newTestSession returns a session pointed at stub with short timings, so the
// polling tests do not wait on the server's real interval.
func newTestSession(t *testing.T, stub *authServiceStub) *loginSession {
	t.Helper()

	server := newAuthServer(t, stub)
	return &loginSession{
		client:    noketv1connect.NewAuthServiceClient(server.Client(), server.URL),
		id:        testAuthorizationID,
		secret:    testAuthorizationSecret,
		interval:  time.Millisecond,
		expiresIn: time.Minute,
	}
}

func TestCreateLoginSession(t *testing.T) {
	var gotPath, gotContentType string
	const authorizeURL = "http://127.0.0.1:9527/cli/login?authorization_id=" + testAuthorizationID

	stub := &authServiceStub{
		create: func(*connect.Request[noketv1.CreateCliAuthorizationRequest]) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error) {
			return connect.NewResponse(&noketv1.CreateCliAuthorizationResponse{
				AuthorizationId:     testAuthorizationID,
				AuthorizationSecret: testAuthorizationSecret,
				ExpiresIn:           600,
				Interval:            2,
				AuthorizationUrl:    authorizeURL,
			}), nil
		},
	}

	_, connectHandler := noketv1connect.NewAuthServiceHandler(stub)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		connectHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	session, err := createLoginSession(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/noket.v1.AuthService/CreateCliAuthorization" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotContentType != "application/proto" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if session.id != testAuthorizationID || session.secret != testAuthorizationSecret {
		t.Fatalf("session = %+v, want the server's id and secret", session)
	}
	if session.authorizeURL != authorizeURL {
		t.Fatalf("authorization URL = %q, want the server's URL", session.authorizeURL)
	}
	if session.interval != 2*time.Second || session.expiresIn != 10*time.Minute {
		t.Fatalf("timings = %s, %s", session.interval, session.expiresIn)
	}
}

func TestCreateLoginSessionWithoutCredentials(t *testing.T) {
	stub := &authServiceStub{
		create: func(*connect.Request[noketv1.CreateCliAuthorizationRequest]) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error) {
			return connect.NewResponse(&noketv1.CreateCliAuthorizationResponse{AuthorizationId: testAuthorizationID}), nil
		},
	}

	_, err := createLoginSession(context.Background(), newAuthServer(t, stub).URL)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want a missing field error", err)
	}
}

func TestCreateLoginSessionWithoutURL(t *testing.T) {
	stub := &authServiceStub{
		create: func(*connect.Request[noketv1.CreateCliAuthorizationRequest]) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error) {
			return connect.NewResponse(&noketv1.CreateCliAuthorizationResponse{
				AuthorizationId:     testAuthorizationID,
				AuthorizationSecret: testAuthorizationSecret,
			}), nil
		},
	}

	_, err := createLoginSession(context.Background(), newAuthServer(t, stub).URL)
	if err == nil || !strings.Contains(err.Error(), "missing the authorization URL") {
		t.Fatalf("error = %v, want a missing URL error", err)
	}
}

func TestCreateLoginSessionError(t *testing.T) {
	stub := &authServiceStub{
		create: func(*connect.Request[noketv1.CreateCliAuthorizationRequest]) (*connect.Response[noketv1.CreateCliAuthorizationResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("control plane is down"))
		},
	}

	_, err := createLoginSession(context.Background(), newAuthServer(t, stub).URL)
	if err == nil || !strings.Contains(err.Error(), "control plane is down") {
		t.Fatalf("error = %v, want the server message", err)
	}
}

func TestLoginSessionApproved(t *testing.T) {
	var gotPath string
	var polls []*noketv1.PollCliAuthorizationRequest
	statuses := []noketv1.CliAuthorizationStatus{
		noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_PENDING,
		noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_APPROVED,
	}

	stub := &authServiceStub{
		poll: func(request *connect.Request[noketv1.PollCliAuthorizationRequest]) (*connect.Response[noketv1.PollCliAuthorizationResponse], error) {
			polls = append(polls, request.Msg)
			response := &noketv1.PollCliAuthorizationResponse{}
			if len(polls) <= len(statuses) {
				response.Status = statuses[len(polls)-1]
			}
			if response.Status == noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_APPROVED {
				response.Token = "issued-token"
			}
			return connect.NewResponse(response), nil
		},
	}

	_, connectHandler := noketv1connect.NewAuthServiceHandler(stub)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		connectHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	session := &loginSession{
		client:    noketv1connect.NewAuthServiceClient(server.Client(), server.URL),
		id:        testAuthorizationID,
		secret:    testAuthorizationSecret,
		interval:  time.Millisecond,
		expiresIn: time.Minute,
	}

	token, err := session.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "issued-token" {
		t.Fatalf("token = %q", token)
	}
	if gotPath != "/noket.v1.AuthService/PollCliAuthorization" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(polls) != 2 {
		t.Fatalf("polls = %d, want 2", len(polls))
	}
	for _, poll := range polls {
		if poll.GetAuthorizationId() != testAuthorizationID || poll.GetAuthorizationSecret() != testAuthorizationSecret {
			t.Fatalf("poll = %v, want the session id and secret", poll)
		}
	}
}

func TestLoginSessionDecisions(t *testing.T) {
	tests := []struct {
		name    string
		status  noketv1.CliAuthorizationStatus
		token   string
		wantErr string
	}{
		{"denied", noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_DENIED, "", "denied in the browser"},
		{"expired", noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_EXPIRED, "", "expired"},
		{"approved without a token", noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_APPROVED, "", "no token"},
		{"unknown status", noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_UNSPECIFIED, "", "unexpected login status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &authServiceStub{
				poll: func(*connect.Request[noketv1.PollCliAuthorizationRequest]) (*connect.Response[noketv1.PollCliAuthorizationResponse], error) {
					return connect.NewResponse(&noketv1.PollCliAuthorizationResponse{Status: tt.status, Token: tt.token}), nil
				},
			}
			session := newTestSession(t, stub)

			_, err := session.wait(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoginSessionPollError(t *testing.T) {
	stub := &authServiceStub{
		poll: func(*connect.Request[noketv1.PollCliAuthorizationRequest]) (*connect.Response[noketv1.PollCliAuthorizationResponse], error) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid secret"))
		},
	}
	session := newTestSession(t, stub)

	_, err := session.wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid secret") {
		t.Fatalf("error = %v, want the server message", err)
	}
}

func TestLoginSessionTimeout(t *testing.T) {
	stub := &authServiceStub{
		poll: func(*connect.Request[noketv1.PollCliAuthorizationRequest]) (*connect.Response[noketv1.PollCliAuthorizationResponse], error) {
			return connect.NewResponse(&noketv1.PollCliAuthorizationResponse{
				Status: noketv1.CliAuthorizationStatus_CLI_AUTHORIZATION_STATUS_PENDING,
			}), nil
		},
	}
	session := newTestSession(t, stub)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := session.wait(ctx)
	if err == nil || !strings.Contains(err.Error(), "login timed out after 1m0s") {
		t.Fatalf("error = %v, want a timeout message", err)
	}
}

func TestLoginWaitError(t *testing.T) {
	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if err := loginWaitError(deadlineCtx, time.Minute); err == nil || !strings.Contains(err.Error(), "timed out after 1m0s") {
		t.Fatalf("deadline error = %v", err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := loginWaitError(cancelCtx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v, want context.Canceled", err)
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
