package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func TestReadWithoutAConfig(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	view, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Cfg == nil || view.CfgErr != nil {
		t.Fatalf("config = %v, err = %v; want the defaults", view.Cfg, view.CfgErr)
	}
	if len(view.Cfg.Upstreams) != 0 {
		t.Fatalf("upstreams = %v, want none", view.Cfg.Upstreams)
	}
	if !errors.Is(view.CredsErr, config.ErrNoCredentials) {
		t.Fatalf("creds error = %v, want ErrNoCredentials", view.CredsErr)
	}
	if view.Daemon != nil {
		t.Fatalf("daemon status = %+v, want none", view.Daemon)
	}
}

func TestReadReportsABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	path, err := config.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not = [valid"), 0o600); err != nil {
		t.Fatal(err)
	}

	view, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.CfgErr == nil {
		t.Fatal("Read accepted a broken config")
	}
}

func TestReadLoadsTheAccountAndNode(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	creds := &types.Credentials{Token: "token", UserID: "alice"}
	if err := config.SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
	node, err := config.EnsureNode("alice")
	if err != nil {
		t.Fatal(err)
	}

	view, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if view.CredsErr != nil || view.Creds == nil || view.Creds.UserID != "alice" {
		t.Fatalf("creds = %+v, err = %v", view.Creds, view.CredsErr)
	}
	if view.NodeID != node.ID {
		t.Fatalf("node id = %q, want %q", view.NodeID, node.ID)
	}
}

func TestReadPublishesTheDaemonStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	status := &types.DaemonStatus{PID: os.Getpid(), Registered: true}
	if err := config.WriteStatus(status); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "noketd.status.json")); err != nil {
		t.Fatalf("status file: %v", err)
	}

	view, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if view.Daemon == nil || view.Daemon.PID != os.Getpid() || !view.Daemon.Registered {
		t.Fatalf("daemon = %+v", view.Daemon)
	}
	if !view.Alive {
		t.Fatal("the test process is alive but Read says otherwise")
	}
	// go test is not a noket process, so it must not look like a background
	// daemon.
	if view.DaemonProcess {
		t.Fatal("go test was classified as a noketd process")
	}
}
