package noketd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func TestStartupErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  *StartupError
		want string
	}{
		{
			name: "child exited cleanly",
			err:  &StartupError{LogPath: "/tmp/noketd.log"},
			want: "noketd failed to start\nlog: /tmp/noketd.log",
		},
		{
			name: "child exited with an error",
			err:  &StartupError{ExitErr: errors.New("exit status 1"), LogPath: "/tmp/noketd.log"},
			want: "noketd failed to start: exit status 1\nlog: /tmp/noketd.log",
		},
		{
			name: "timed out",
			err:  &StartupError{Timeout: 30 * time.Second, LogPath: "/tmp/noketd.log"},
			want: "noketd did not register within 30s and was stopped\nlog: /tmp/noketd.log",
		},
		{
			name: "timed out with a last error and a log tail",
			err: &StartupError{
				Timeout:   30 * time.Second,
				LastError: "dial tcp: connection refused",
				LogTail:   "line one\nline two",
				LogPath:   "/tmp/noketd.log",
			},
			want: "noketd did not register within 30s and was stopped\nlast error: dial tcp: connection refused\nline one\nline two\nlog: /tmp/noketd.log",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); got != test.want {
				t.Fatalf("Error():\n%q\nwant:\n%q", got, test.want)
			}
		})
	}
}

// fakeProcess is a Process whose child never exits.
func fakeProcess(pid int) *Process {
	return &Process{PID: pid, LogPath: "", done: make(chan error, 1)}
}

func TestAwaitRegistrationSucceeds(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())
	proc := fakeProcess(4242)

	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = config.WriteStatus(&types.DaemonStatus{PID: 4242, Connected: true})
		time.Sleep(80 * time.Millisecond)
		_ = config.WriteStatus(&types.DaemonStatus{PID: 4242, Connected: true, Registered: true, NodeID: "node-1"})
	}()

	var sawConnected bool
	status, err := AwaitRegistration(context.Background(), proc, 5*time.Second, func(s types.DaemonStatus) {
		if s.Connected {
			sawConnected = true
		}
	})
	if err != nil {
		t.Fatalf("AwaitRegistration: %v", err)
	}
	if !status.Registered || status.NodeID != "node-1" {
		t.Fatalf("status = %+v", status)
	}
	if !sawConnected {
		t.Fatal("onStatus never saw the connected state")
	}
}

func TestAwaitRegistrationTimesOut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	logPath, err := config.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("boom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc := fakeProcess(999999)
	proc.LogPath = logPath

	_, err = AwaitRegistration(context.Background(), proc, 50*time.Millisecond, nil)
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Timeout == 0 {
		t.Fatalf("AwaitRegistration = %v, want a StartupError timeout", err)
	}
	if startup.LogTail != "boom" {
		t.Fatalf("log tail = %q", startup.LogTail)
	}
}

func TestAwaitRegistrationStopsOnCancel(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())
	proc := fakeProcess(999999)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AwaitRegistration(ctx, proc, 5*time.Second, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitRegistration = %v, want context.Canceled", err)
	}
}

func TestAwaitRegistrationReportsAChildExit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	logPath, err := config.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	proc := fakeProcess(999999)
	proc.LogPath = logPath
	proc.done <- errors.New("exit status 2")

	_, err = AwaitRegistration(context.Background(), proc, 5*time.Second, nil)
	var startup *StartupError
	if !errors.As(err, &startup) || startup.ExitErr == nil {
		t.Fatalf("AwaitRegistration = %v, want a StartupError carrying the exit", err)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestWaitStoppedTimesOutThenSeesTheExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX sleep process")
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid

	err := WaitStopped(pid, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not exit within") {
		t.Fatalf("WaitStopped = %v, want a timeout", err)
	}

	_ = child.Process.Kill()
	_ = child.Wait()
	if err := WaitStopped(pid, time.Second); err != nil {
		t.Fatalf("WaitStopped after exit = %v", err)
	}
}

func TestStopClearsTheDaemonState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX sleep process")
	}
	t.Setenv(config.DirEnv, t.TempDir())

	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	if err := config.WritePID(pid); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteStatus(&types.DaemonStatus{PID: pid}); err != nil {
		t.Fatal(err)
	}
	go func() { _ = child.Wait() }()

	if err := Stop(pid, 2*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := config.ReadPID(); !errors.Is(err, config.ErrNotRunning) {
		t.Fatalf("pid file survived Stop: %v", err)
	}
	if _, err := config.ReadStatus(); !errors.Is(err, config.ErrNoStatus) {
		t.Fatalf("status file survived Stop: %v", err)
	}
}
