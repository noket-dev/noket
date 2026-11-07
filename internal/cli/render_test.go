package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/state"
	"github.com/noket-dev/noket/internal/types"
)

func TestInteractiveOutputRejectsNonTerminals(t *testing.T) {
	if interactiveOutput(&bytes.Buffer{}) {
		t.Fatal("a buffer is not a terminal")
	}

	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if interactiveOutput(file) {
		t.Fatal("a regular file is not a terminal")
	}
}

func TestRenderStatusAlignsLabelsWithoutStyling(t *testing.T) {
	sections := []statusSection{
		{title: "Account", rows: []statusRow{
			{label: "User", value: "alice"},
			{label: "Saved", value: "2026-01-02 15:04"},
		}},
		{title: "Node", rows: []statusRow{{label: "ID", value: "abc"}}},
	}

	var out bytes.Buffer
	renderStatus(&out, sections, false)

	want := "Account\n" +
		"  User   alice\n" +
		"  Saved  2026-01-02 15:04\n" +
		"\n" +
		"Node\n" +
		"  ID  abc\n"
	if got := out.String(); got != want {
		t.Fatalf("output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderStatusPlainOutputHasNoEscapes(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, buildStatus(state.View{}), false)
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatalf("plain status carries escape codes: %q", out.String())
	}
}

func TestPrintUpstreamTableStaysPlainWithoutATerminal(t *testing.T) {
	var out bytes.Buffer
	printUpstreamTable(&out, []types.Upstream{
		{Name: "a", Protocol: types.ProtocolOpenAI, BaseURL: "http://a.example/v1", Models: []string{"m1", "m2"}, APIKey: "secret"},
		{Name: "b", Protocol: types.ProtocolAnthropic, BaseURL: "http://b.example/v1", Models: []string{"m3"}},
	})

	got := out.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("piped table carries escape codes: %q", got)
	}
	for _, want := range []string{"Name", "Protocol", "Base URL", "m1, m2", "set", "not set"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "secret") {
		t.Fatal("the table must never print the API key")
	}
}

func rowFor(rows []statusRow, label string) (statusRow, bool) {
	for _, row := range rows {
		if row.label == label {
			return row, true
		}
	}
	return statusRow{}, false
}

func sectionFor(t *testing.T, sections []statusSection, title string) []statusRow {
	t.Helper()
	for _, section := range sections {
		if section.title == title {
			return section.rows
		}
	}
	t.Fatalf("no %q section in %v", title, sections)
	return nil
}

func TestBuildStatusWithoutLogin(t *testing.T) {
	sections := buildStatus(state.View{Dir: "/tmp/noket", CredsErr: config.ErrNoCredentials})

	account := sectionFor(t, sections, "Account")
	state, ok := rowFor(account, "State")
	if !ok || !strings.Contains(state.value, "not logged in") || state.tone != toneWarn {
		t.Fatalf("account rows = %v", account)
	}

	node := sectionFor(t, sections, "Node")
	if id, ok := rowFor(node, "ID"); !ok || !strings.Contains(id.value, "after login") {
		t.Fatalf("node rows = %v", node)
	}

	daemon := sectionFor(t, sections, "noketd")
	if state, ok := rowFor(daemon, "State"); !ok || state.value != "not running" {
		t.Fatalf("daemon rows = %v", daemon)
	}
}

func TestBuildStatusShowsTheAccountAndNode(t *testing.T) {
	creds := &types.Credentials{
		Token:   "test-token",
		UserID:  "alice",
		SavedAt: time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local),
	}
	sections := buildStatus(state.View{Dir: "/tmp/noket", Creds: creds, NodeID: "node-1"})

	account := sectionFor(t, sections, "Account")
	if user, ok := rowFor(account, "User"); !ok || user.value != "alice" {
		t.Fatalf("account rows = %v", account)
	}
	if saved, ok := rowFor(account, "Saved"); !ok || !strings.Contains(saved.value, "2026-01-02 15:04") {
		t.Fatalf("account rows = %v", account)
	}

	node := sectionFor(t, sections, "Node")
	if id, ok := rowFor(node, "ID"); !ok || id.value != "node-1" {
		t.Fatalf("node rows = %v", node)
	}
}

func TestBuildStatusDaemonStates(t *testing.T) {
	tests := []struct {
		name    string
		in      state.View
		want    string
		tone    statusTone
		lastErr string
	}{
		{
			name: "connected",
			in:   state.View{Alive: true, DaemonProcess: true, Daemon: &types.DaemonStatus{PID: 42, Registered: true}},
			want: "connected (pid 42)", tone: toneOK,
		},
		{
			name: "registering",
			in:   state.View{Alive: true, DaemonProcess: true, Daemon: &types.DaemonStatus{PID: 42, Connected: true}},
			want: "connected, registering (pid 42)", tone: toneWarn,
		},
		{
			name: "disconnected",
			in:   state.View{Alive: true, DaemonProcess: true, Daemon: &types.DaemonStatus{PID: 42}},
			want: "disconnected (pid 42)", tone: toneWarn,
		},
		{
			name: "stale",
			in:   state.View{Daemon: &types.DaemonStatus{PID: 42}},
			want: "not running (stale status, pid 42)", tone: toneDim,
		},
		{
			name: "foreground",
			in:   state.View{Alive: true, Daemon: &types.DaemonStatus{PID: 42}},
			want: "running in the foreground (pid 42); stop it with your supervisor", tone: toneWarn,
		},
		{
			name: "last error",
			in: state.View{Alive: true, DaemonProcess: true, Daemon: &types.DaemonStatus{
				PID: 42, Registered: true, LastError: "listen tcp: address already in use",
			}},
			want: "connected (pid 42)", tone: toneOK,
			lastErr: "listen tcp: address already in use",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := daemonRows(test.in)
			state, ok := rowFor(rows, "State")
			if !ok || state.value != test.want || state.tone != test.tone {
				t.Fatalf("state = %+v (ok = %v), want %q with tone %v", state, ok, test.want, test.tone)
			}
			lastError, ok := rowFor(rows, "Last error")
			switch {
			case test.lastErr == "" && ok:
				t.Fatalf("unexpected last error row: %+v", lastError)
			case test.lastErr != "" && (!ok || lastError.value != test.lastErr || lastError.tone != toneErr):
				t.Fatalf("last error = %+v (ok = %v)", lastError, ok)
			}
		})
	}
}

func TestDaemonRowsShowTheNodeNameAndStart(t *testing.T) {
	rows := daemonRows(state.View{Alive: true, DaemonProcess: true, Daemon: &types.DaemonStatus{
		PID:        42,
		Registered: true,
		NodeName:   "macbook",
		StartedAt:  time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local),
	}})

	if name, ok := rowFor(rows, "Name"); !ok || name.value != "macbook" {
		t.Fatalf("rows = %v", rows)
	}
	if started, ok := rowFor(rows, "Started"); !ok || !strings.Contains(started.value, "2026-01-02 15:04") {
		t.Fatalf("rows = %v", rows)
	}
}
