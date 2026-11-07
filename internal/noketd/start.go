package noketd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

const (
	// StartupTimeout is how long a fresh daemon gets to register.
	StartupTimeout = 30 * time.Second
	// ShutdownTimeout is how long a terminating daemon gets to exit.
	ShutdownTimeout = 30 * time.Second

	statusPollInterval = 250 * time.Millisecond
	exitPollInterval   = 100 * time.Millisecond
	tailLines          = 12
)

// StartOptions mirrors the flags `noket up` and the TUI expose.
type StartOptions struct {
	Name    string
	Verbose bool
}

// Process is a daemon started by Start.
type Process struct {
	PID     int
	LogPath string
	done    chan error
}

// Done receives the child's Wait result once it exits.
func (p *Process) Done() <-chan error { return p.done }

// Start spawns the daemon detached from the terminal and appends its output to
// the log file. It returns as soon as the process exists; AwaitRegistration
// reports whether it comes up.
func Start(opts StartOptions) (*Process, error) {
	logPath, err := config.LogPath()
	if err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	// The child inherits the descriptor; the parent only needs it to spawn.
	defer func() { _ = logFile.Close() }()

	child, err := daemonCommand(opts.Name, opts.Verbose)
	if err != nil {
		return nil, err
	}
	child.Stdout = logFile
	child.Stderr = logFile
	child.Stdin = nil
	configureDetached(child)
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("start noketd: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	return &Process{PID: child.Process.Pid, LogPath: logPath, done: done}, nil
}

// StartupError describes a daemon that never registered. Its message is the
// one `noket up` has always printed, log tail and all.
type StartupError struct {
	ExitErr   error
	Timeout   time.Duration
	LastError string
	LogTail   string
	LogPath   string
}

func (e *StartupError) Error() string {
	message := "noketd failed to start"
	if e.ExitErr != nil {
		message = fmt.Sprintf("noketd failed to start: %v", e.ExitErr)
	}
	if e.Timeout > 0 {
		message = fmt.Sprintf("noketd did not register within %s and was stopped", e.Timeout)
	}
	if e.LastError != "" {
		message += "\nlast error: " + e.LastError
	}
	if e.LogTail != "" {
		message += "\n" + e.LogTail
	}
	message += "\nlog: " + e.LogPath
	return message
}

// AwaitRegistration polls the status file until proc registers, exits, times
// out, or ctx is canceled. onStatus sees every status written by this process.
func AwaitRegistration(ctx context.Context, proc *Process, timeout time.Duration, onStatus func(types.DaemonStatus)) (*types.DaemonStatus, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(statusPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			status, err := config.ReadStatus()
			if err != nil || status.PID != proc.PID {
				continue
			}
			if onStatus != nil {
				onStatus(*status)
			}
			if status.Registered {
				return status, nil
			}
		case err := <-proc.Done():
			config.RemovePID()
			return nil, &StartupError{
				ExitErr: err,
				LogTail: TailLog(proc.LogPath, tailLines),
				LogPath: proc.LogPath,
			}
		case <-deadline.C:
			_ = Terminate(proc.PID)
			config.RemovePID()
			lastError := ""
			if status, err := config.ReadStatus(); err == nil {
				lastError = status.LastError
			}
			return nil, &StartupError{
				Timeout:   timeout,
				LastError: lastError,
				LogTail:   TailLog(proc.LogPath, tailLines),
				LogPath:   proc.LogPath,
			}
		case <-ctx.Done():
			_ = Terminate(proc.PID)
			return nil, ctx.Err()
		}
	}
}

// WaitStopped polls until pid exits or timeout elapses.
func WaitStopped(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for ProcessAlive(pid) {
		if time.Now().After(deadline) {
			return fmt.Errorf("noketd (pid %d) did not exit within %s", pid, timeout)
		}
		time.Sleep(exitPollInterval)
	}
	return nil
}

// Stop terminates a background daemon, waits for it, and clears its state.
func Stop(pid int, timeout time.Duration) error {
	if err := Terminate(pid); err != nil {
		return fmt.Errorf("stop noketd (pid %d): %w", pid, err)
	}
	if err := WaitStopped(pid, timeout); err != nil {
		return err
	}
	config.RemovePID()
	config.RemoveStatus()
	return nil
}

// TailLog returns the last lines of the daemon log, or "" when it is empty.
func TailLog(path string, lines int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	const maxTail = 64 << 10
	if len(data) > maxTail {
		data = data[len(data)-maxTail:]
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// daemonCommand prefers a noketd binary next to noket. Development builds and
// single-binary installs fall back to the hidden `noket noketd` subcommand.
func daemonCommand(name string, verbose bool) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate the noket executable: %w", err)
	}
	args := make([]string, 0, 3)
	if name != "" {
		args = append(args, "--name", name)
	}
	if verbose {
		args = append(args, "--verbose")
	}

	sibling := filepath.Join(filepath.Dir(exe), "noketd"+exeSuffix())
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return exec.Command(sibling, args...), nil
	}
	return exec.Command(exe, append([]string{"noketd"}, args...)...), nil
}
