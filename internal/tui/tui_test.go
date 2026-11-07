package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/state"
	"github.com/noket-dev/noket/internal/types"
	"github.com/noket-dev/noket/internal/upstream"
)

func testModel(t *testing.T, ups ...types.Upstream) Model {
	t.Helper()
	t.Setenv(config.DirEnv, t.TempDir())

	m := newModel(context.Background(), func() {}, Options{Version: "test"})
	cfg := config.Default()
	cfg.Upstreams = ups
	m.view = state.View{Dir: t.TempDir(), Cfg: cfg}
	m.width, m.height = 100, 30
	m.table.SetWidth(100)
	m.table.SetHeight(23)
	m.help.Width = 100
	m.syncRows()
	return m
}

func press(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return next, cmd
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func sampleUpstream(name string) types.Upstream {
	return types.Upstream{
		Name:     name,
		Protocol: types.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:11434",
		Models:   []string{"llama3"},
	}
}

func TestAddOpensTheFieldsForm(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('a'))

	if m.modal.kind != modalFields {
		t.Fatalf("modal = %v, want modalFields", m.modal.kind)
	}
	if m.modal.form == nil {
		t.Fatal("no form")
	}
	if m.modal.protocol != types.ProtocolOpenAI || m.modal.baseURL == "" {
		t.Fatalf("defaults = %q %q", m.modal.protocol, m.modal.baseURL)
	}
	if m.modal.expected != nil {
		t.Fatal("add must not have an expected snapshot")
	}
}

func TestEditKeepsASnapshotOfTheSelectedUpstream(t *testing.T) {
	m := testModel(t, sampleUpstream("local"))
	m, _ = press(t, m, runeKey('e'))

	if m.modal.kind != modalFields || !m.modal.editing {
		t.Fatalf("modal = %v, editing = %v", m.modal.kind, m.modal.editing)
	}
	if m.modal.expected == nil || m.modal.expected.Name != "local" {
		t.Fatalf("expected = %+v", m.modal.expected)
	}
	if m.modal.name != "local" {
		t.Fatalf("name = %q", m.modal.name)
	}
}

func TestRemoveIsNotSwallowedByTheTable(t *testing.T) {
	m := testModel(t, sampleUpstream("local"))
	m, _ = press(t, m, runeKey('d'))

	if m.modal.kind != modalConfirmRemove {
		t.Fatalf("modal = %v, want modalConfirmRemove", m.modal.kind)
	}
	if m.modal.expected == nil || m.modal.expected.Name != "local" {
		t.Fatalf("expected = %+v", m.modal.expected)
	}
	if m.table.Cursor() != 0 {
		t.Fatalf("cursor = %d, the table moved instead", m.table.Cursor())
	}
}

func TestUpWithoutLoginExplainsInsteadOfTouchingTheTable(t *testing.T) {
	m := testModel(t)
	cursor := m.table.Cursor()
	m, _ = press(t, m, runeKey('u'))

	if !strings.Contains(m.toast.text, "not logged in, run noket login first") {
		t.Fatalf("toast = %q", m.toast.text)
	}
	if m.table.Cursor() != cursor {
		t.Fatalf("cursor = %d, was %d", m.table.Cursor(), cursor)
	}
}

func TestDownWithNothingRunningSaysSo(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('x'))

	if m.toast.text != "noketd is not running." {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestEscClosesTheModal(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('a'))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.modal.kind != modalNone {
		t.Fatalf("modal = %v, want none", m.modal.kind)
	}
}

func TestQTypesIntoTheFormInsteadOfQuitting(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('a'))
	m, _ = press(t, m, runeKey('q'))

	if m.modal.kind != modalFields {
		t.Fatalf("modal = %v, q closed the program instead", m.modal.kind)
	}
	if m.modal.form.State != huh.StateNormal {
		t.Fatalf("form state = %v", m.modal.form.State)
	}
}

func TestCompletedFormMovesOnToTheProbe(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('a'))
	m.modal.name = "local"
	m.modal.baseURL = "http://localhost:11434/v1/"
	m.modal.form.State = huh.StateCompleted

	m, cmd := press(t, m, runeKey('x'))

	if m.busy.kind != busyProbe {
		t.Fatalf("busy = %v, want probe", m.busy.kind)
	}
	if cmd == nil {
		t.Fatal("no probe command")
	}
	if m.modal.up.Name != "local" || m.modal.up.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("built = %+v", m.modal.up)
	}
}

func TestSubmittedFormShowsProgressInsteadOfAnEmptyPopup(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, runeKey('a'))
	m.modal.name = "local"
	m.modal.baseURL = "http://localhost:11434"
	m.modal.form.State = huh.StateCompleted
	m, _ = press(t, m, runeKey('x'))

	if m.busy.kind != busyProbe {
		t.Fatalf("busy = %v, want probe", m.busy.kind)
	}
	view := m.View()
	if !strings.Contains(view, "Reading the model list from") {
		t.Fatalf("the popup does not show what is running:\n%s", view)
	}
	if !strings.Contains(view, "New upstream") {
		t.Fatalf("the popup lost its title:\n%s", view)
	}
}

func TestStaleProbeResultsAreDropped(t *testing.T) {
	m := testModel(t)
	m.busy = busyState{kind: busyProbe, seq: 2}

	updated, _ := m.Update(probeMsg{seq: 1, purpose: probeForModal, models: []string{"a"}})
	next := updated.(Model)
	if next.modal.kind != modalNone {
		t.Fatalf("modal = %v, a stale probe opened it", next.modal.kind)
	}
	if next.busy.seq != 2 || next.busy.kind != busyProbe {
		t.Fatalf("busy = %+v, a stale probe cleared it", next.busy)
	}
}

func TestProbeSuccessOpensTheModelPicker(t *testing.T) {
	m := testModel(t)
	*m.modal = modalState{kind: modalFields}
	m.busy = busyState{kind: busyProbe, seq: 1}

	updated, _ := m.Update(probeMsg{seq: 1, purpose: probeForModal, models: []string{"a", "b"}})
	next := updated.(Model)
	if next.modal.kind != modalModels {
		t.Fatalf("modal = %v", next.modal.kind)
	}
	if next.busy.kind != busyNone {
		t.Fatalf("busy = %+v", next.busy)
	}
	if len(next.modal.modelSelected) != 2 {
		t.Fatalf("selected = %v, want every found model preselected", next.modal.modelSelected)
	}
}

func TestModelsPopupSaysHowToSelect(t *testing.T) {
	m := testModel(t)
	*m.modal = modalState{kind: modalFields}
	m.busy = busyState{kind: busyProbe, seq: 1}

	updated, _ := m.Update(probeMsg{seq: 1, purpose: probeForModal, models: []string{"a", "b"}})
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"space", "toggle", "/", "filter", "↑/↓", "confirm"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the model picker does not say %q:\n%s", want, view)
		}
	}
}

func TestProbeFailureOpensTheRecoveryPicker(t *testing.T) {
	m := testModel(t)
	*m.modal = modalState{kind: modalFields}
	m.busy = busyState{kind: busyProbe, seq: 1}

	updated, _ := m.Update(probeMsg{seq: 1, purpose: probeForModal, err: errFake})
	next := updated.(Model)
	if next.modal.kind != modalRecovery || next.modal.probeErr == nil {
		t.Fatalf("modal = %v, probeErr = %v", next.modal.kind, next.modal.probeErr)
	}
}

func TestTestResultLandsInTheRow(t *testing.T) {
	up := sampleUpstream("local")
	m := testModel(t, up)
	m.busy = busyState{kind: busyProbe, seq: 3}

	updated, _ := m.Update(probeMsg{
		seq: 3, purpose: probeForTest, name: up.Name, baseURL: up.BaseURL,
		models: []string{"a", "b"},
	})
	next := updated.(Model)
	if len(next.rows) != 1 || !next.rows[0].tested || next.rows[0].test.cell() != "ok (2)" {
		t.Fatalf("rows = %+v", next.rows)
	}
	if next.busy.kind != busyNone {
		t.Fatalf("busy = %+v", next.busy)
	}
}

func TestErrStaleRedrawsAndClosesTheModal(t *testing.T) {
	m := testModel(t)
	*m.modal = modalState{kind: modalModels, pending: modalModels, form: m.modelsForm()}
	m, seq := m.beginBusy(busySave, "Saving the upstream")

	fresh := config.Default()
	fresh.Upstreams = []types.Upstream{sampleUpstream("from-cloud")}

	updated, _ := m.Update(saveDoneMsg{seq: seq, cfg: fresh, err: upstream.ErrStale, name: "mine"})
	next := updated.(Model)
	if next.modal.kind != modalNone {
		t.Fatalf("modal = %v", next.modal.kind)
	}
	if !strings.Contains(next.toast.text, "changed on disk") {
		t.Fatalf("toast = %q", next.toast.text)
	}
	if len(next.rows) != 1 || next.rows[0].up.Name != "from-cloud" {
		t.Fatalf("rows = %+v, the fresh config was not applied", next.rows)
	}
	if next.busy.kind != busyNone {
		t.Fatalf("busy = %+v", next.busy)
	}
}

func TestSavedUpstreamClosesTheModalWithAToast(t *testing.T) {
	m := testModel(t)
	*m.modal = modalState{kind: modalModels, pending: modalModels, form: m.modelsForm()}
	m, seq := m.beginBusy(busySave, "Saving the upstream")

	fresh := config.Default()
	fresh.Upstreams = []types.Upstream{sampleUpstream("local")}

	updated, _ := m.Update(saveDoneMsg{seq: seq, cfg: fresh, name: "local", editing: false})
	next := updated.(Model)
	if next.modal.kind != modalNone {
		t.Fatalf("modal = %v", next.modal.kind)
	}
	if next.toast.text != `Added upstream "local"` {
		t.Fatalf("toast = %q", next.toast.text)
	}
	if len(next.rows) != 1 {
		t.Fatalf("rows = %+v", next.rows)
	}
}

func TestHeaderShowsTheDaemonState(t *testing.T) {
	m := testModel(t)
	m.view.Daemon = &types.DaemonStatus{PID: 42, Registered: true, Connected: true}
	m.view.Alive = true

	view := m.View()
	if !strings.Contains(view, "connected") || !strings.Contains(view, "pid 42") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestEnterOpensTheDetailsPopup(t *testing.T) {
	up := sampleUpstream("local")
	up.APIKey = "sk-test"
	up.Headers = map[string]string{"anthropic-version": "2023-06-01"}
	m := testModel(t, up)
	m.results[resultKey(up.Name, up.BaseURL)] = testResult{models: 2}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal.kind != modalDetail {
		t.Fatalf("modal = %v, want modalDetail", m.modal.kind)
	}

	view := m.View()
	for _, want := range []string{"Upstream", "local", "openai", up.BaseURL, "set", "llama3", "ok (2)", "anthropic-version"} {
		if !strings.Contains(view, want) {
			t.Fatalf("details are missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "sk-test") {
		t.Fatalf("the API key is on screen:\n%s", view)
	}
	if !strings.Contains(view, "close") || !strings.Contains(view, "edit") {
		t.Fatalf("detail help line is missing:\n%s", view)
	}
}

func TestDetailsShowWhatIsMissing(t *testing.T) {
	m := testModel(t, sampleUpstream("local"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if !strings.Contains(view, "not set") {
		t.Fatalf("the key is not marked as unset:\n%s", view)
	}
	if !strings.Contains(view, "Test") || !strings.Contains(view, "-") {
		t.Fatalf("the untested row is missing:\n%s", view)
	}
}

func TestDetailsCapTheModelList(t *testing.T) {
	up := sampleUpstream("local")
	up.Models = []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8", "m9", "m10"}
	m := testModel(t, up)
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if !strings.Contains(view, "m8") {
		t.Fatalf("the first eight models are missing:\n%s", view)
	}
	if strings.Contains(view, "m9") {
		t.Fatalf("the list was not capped:\n%s", view)
	}
	if !strings.Contains(view, "and 2 more") {
		t.Fatalf("the remainder count is missing:\n%s", view)
	}
}

func TestDetailsCloseAndHandOverToBrowseActions(t *testing.T) {
	m := testModel(t, sampleUpstream("local"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.modal.kind != modalNone {
		t.Fatalf("modal = %v, esc did not close the details", m.modal.kind)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(t, m, runeKey('e'))
	if m.modal.kind != modalFields || !m.modal.editing {
		t.Fatalf("modal = %v, editing = %v", m.modal.kind, m.modal.editing)
	}
	if m.modal.expected == nil || m.modal.expected.Name != "local" {
		t.Fatalf("expected = %+v", m.modal.expected)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(t, m, runeKey('d'))
	if m.modal.kind != modalConfirmRemove {
		t.Fatalf("modal = %v, d did not open the confirmation", m.modal.kind)
	}
}

func TestDetailsNeedASelectedRow(t *testing.T) {
	m := testModel(t)
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.modal.kind != modalNone {
		t.Fatalf("modal = %v, an empty list has nothing to show", m.modal.kind)
	}
	if !strings.Contains(m.toast.text, "select an upstream") {
		t.Fatalf("toast = %q", m.toast.text)
	}
}

func TestModalIsAFramedPopupOverTheTable(t *testing.T) {
	m := testModel(t, sampleUpstream("local"), sampleUpstream("other"))
	m.height = 40
	m, _ = press(t, m, runeKey('a'))

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 40 {
		t.Fatalf("view has %d lines, want the terminal height", len(lines))
	}

	top := -1
	for i, line := range lines {
		if strings.Contains(line, "╭") {
			top = i
			break
		}
	}
	if top < 4 {
		t.Fatalf("popup top at line %d, want it inside the body", top)
	}
	if !strings.Contains(lines[top], "╮") {
		t.Fatalf("top border is not closed: %q", lines[top])
	}

	frame := []rune(ansi.Strip(lines[top]))
	left, right := 0, 0
	for i, r := range frame {
		switch r {
		case '╭':
			left = i
		case '╮':
			right = i
		}
	}
	if left < 10 {
		t.Fatalf("popup starts at column %d, it is not centred", left)
	}
	if left != 99-right {
		t.Fatalf("popup is off centre: left margin %d, right margin %d", left, 99-right)
	}

	// The table still shows above the popup; the rows the frame covers do not
	// leak through it.
	if !strings.Contains(strings.Join(lines[:top], "\n"), "Name") {
		t.Fatalf("the table header is not visible above the popup:\n%s", view)
	}
	if strings.Contains(lines[top], "local") {
		t.Fatalf("table text shows through the popup border: %q", lines[top])
	}
	if !strings.Contains(view, "New upstream") || !strings.Contains(view, "Upstream name") {
		t.Fatalf("popup content is missing:\n%s", view)
	}

	// The frame keeps a blank line above the title and below the form, so the
	// content never touches the border.
	bottom := top
	for i := top; i < len(lines); i++ {
		if strings.Contains(lines[i], "╰") {
			bottom = i
		}
	}
	for _, i := range []int{top + 1, bottom - 1} {
		inner := strings.Trim(strings.TrimSpace(ansi.Strip(lines[i])), "│")
		if strings.TrimSpace(inner) != "" {
			t.Fatalf("line %d is not padding: %q", i, ansi.Strip(lines[i]))
		}
	}
}

func TestModalHelpReplacesTheBrowseKeys(t *testing.T) {
	m := testModel(t, sampleUpstream("local"))
	m, _ = press(t, m, runeKey('a'))

	view := m.View()
	if !strings.Contains(view, "enter next") {
		t.Fatalf("fields form help is missing:\n%s", view)
	}
	if strings.Count(view, "enter next") != 1 {
		t.Fatalf("huh drew its own help line as well:\n%s", view)
	}
	if strings.Contains(view, "add") || strings.Contains(view, "refresh") {
		t.Fatalf("browse keys are still on screen:\n%s", view)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = press(t, m, runeKey('d'))
	if view := m.View(); !strings.Contains(view, "enter confirm") {
		t.Fatalf("confirmation help is missing:\n%s", view)
	}
}

func TestEmptyListExplainsTheAddKey(t *testing.T) {
	m := testModel(t)
	if view := m.View(); !strings.Contains(view, "No upstreams yet. Press a to add one.") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestWindowSizeResizesTheTable(t *testing.T) {
	m := testModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(Model)

	if next.width != 120 || next.help.Width != 120 {
		t.Fatalf("width = %d, help = %d", next.width, next.help.Width)
	}
	// SetHeight takes the whole widget: the viewport gives up one row to the
	// header.
	if next.table.Height() != 32 {
		t.Fatalf("table height = %d, want 32", next.table.Height())
	}
}

func TestTableKeyMapLeavesTheActionLettersAlone(t *testing.T) {
	km := tableKeys()
	bindings := [][]string{
		km.LineUp.Keys(), km.LineDown.Keys(),
		km.PageUp.Keys(), km.PageDown.Keys(),
		km.HalfPageUp.Keys(), km.HalfPageDown.Keys(),
		km.GotoTop.Keys(), km.GotoBottom.Keys(),
	}
	for _, keys := range bindings {
		for _, k := range keys {
			switch k {
			case "u", "d", "f", "b", "g", "G", "a", "e", "t", "x", "q", "r":
				t.Fatalf("the table still binds %q", k)
			}
		}
	}
}

var errFake = errors.New("probe failed")
