package noketd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func TestWaitForCredentialsReturnsTheSavedLogin(t *testing.T) {
	creds := &types.Credentials{Token: "token"}
	calls := 0
	load := func() (*types.Credentials, error) {
		calls++
		if calls < 3 {
			return nil, config.ErrNoCredentials
		}
		return creds, nil
	}

	got, err := waitForCredentials(context.Background(), time.Millisecond, load)
	if err != nil {
		t.Fatalf("waitForCredentials: %v", err)
	}
	if got != creds {
		t.Fatalf("got %v, want the saved credentials", got)
	}
	if calls != 3 {
		t.Fatalf("loaded %d times, want 3", calls)
	}
}

func TestWaitForCredentialsStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	load := func() (*types.Credentials, error) {
		cancel()
		return nil, config.ErrNoCredentials
	}

	if _, err := waitForCredentials(ctx, time.Millisecond, load); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWaitForCredentialsReturnsOtherErrors(t *testing.T) {
	sentinel := errors.New("parse credentials: unexpected end of JSON input")
	load := func() (*types.Credentials, error) { return nil, sentinel }

	if _, err := waitForCredentials(context.Background(), time.Millisecond, load); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the read error", err)
	}
}

// A supervised process must stay up while there is no login, so the supervisor
// neither restarts it nor logs the same error on every attempt.
func TestRunWaitsForLoginWhenSupervised(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, RunOptions{Logger: zerolog.Nop()}) }()

	select {
	case err := <-done:
		t.Fatalf("Run returned %v before a login existed", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after the context was canceled")
	}
}

func TestRunFailsFastWithoutLoginWhenManaged(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	err := Run(context.Background(), RunOptions{Managed: true, Logger: zerolog.Nop()})
	if err == nil || err.Error() != "not logged in, run noket login first" {
		t.Fatalf("Run = %v, want the login error", err)
	}
}
