package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/netguard"
	"github.com/noket-dev/noket/internal/noketd"
	"github.com/noket-dev/noket/internal/state"
	"github.com/noket-dev/noket/internal/types"
	"github.com/noket-dev/noket/internal/upstream"
)

// Model is the root bubbletea model. It owns one snapshot of the local state,
// the upstream table, and at most one modal form and one background task.
type Model struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   Options

	view    state.View
	viewErr error
	logPath string

	table   table.Model
	rows    []row
	results map[string]testResult

	modal *modalState
	busy  busyState
	toast toastState

	width, height int
	keys          keyMap
	help          help.Model
	spin          spinner.Model
}

type row struct {
	up     types.Upstream
	test   testResult
	tested bool
}

type testResult struct {
	models int
	err    error
}

func (r testResult) cell() string {
	switch {
	case r.err != nil:
		return "failed"
	case r.models > 0:
		return fmt.Sprintf("ok (%d)", r.models)
	default:
		return "-"
	}
}

type modalKind int

const (
	modalNone modalKind = iota
	modalDetail
	modalFields
	modalModels
	modalManualModels
	modalRecovery
	modalConfirmRemove
	modalConfirmStop
)

type modalState struct {
	kind modalKind
	form *huh.Form

	// Fields form.
	name, protocol, baseURL, key string
	clearKey                     bool
	editing                      bool
	expected                     *types.Upstream
	up                           types.Upstream

	// Model selection.
	modelItems    []string
	modelSelected []string
	manual        string

	// Probe recovery.
	probeErr error
	recovery string

	// Confirmations.
	confirm bool
	pid     int

	// Form to restore when a save fails.
	pending modalKind
}

type busyKind int

const (
	busyNone busyKind = iota
	busyProbe
	busySave
	busyRemove
	busyStart
	busyStop
)

type busyState struct {
	kind   busyKind
	seq    int
	detail string
}

func (b busyState) writing() bool {
	return b.kind == busySave || b.kind == busyRemove
}

type toastTone int

const (
	toneInfo toastTone = iota
	toneOK
	toneWarn
	toneErr
)

type toastState struct {
	text  string
	tone  toastTone
	until time.Time
}

type tickMsg time.Time

type refreshMsg struct {
	view state.View
	err  error
}

type probePurpose int

const (
	probeForModal probePurpose = iota
	probeForTest
)

type probeMsg struct {
	seq     int
	purpose probePurpose
	name    string
	baseURL string
	models  []string
	err     error
}

type saveDoneMsg struct {
	seq     int
	cfg     *types.Config
	err     error
	name    string
	editing bool
}

type removeDoneMsg struct {
	seq  int
	cfg  *types.Config
	err  error
	name string
}

type startDoneMsg struct {
	seq     int
	pid     int
	status  *types.DaemonStatus
	logPath string
	err     error
}

type stopDoneMsg struct {
	seq int
	pid int
	err error
}

func newModel(ctx context.Context, cancel context.CancelFunc, opts Options) Model {
	logPath, _ := config.LogPath()
	m := Model{
		ctx:     ctx,
		cancel:  cancel,
		opts:    opts,
		logPath: logPath,
		modal:   &modalState{},
		results: make(map[string]testResult),
		keys:    defaultKeys(),
		help:    help.New(),
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
	m.table = table.New(
		table.WithColumns(columns(80)),
		table.WithKeyMap(tableKeys()),
		table.WithStyles(tableStyles()),
		table.WithFocused(true),
		table.WithWidth(80),
		table.WithHeight(10),
	)
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), readStateCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetColumns(columns(msg.Width))
		m.table.SetWidth(msg.Width)
		m.table.SetHeight(max(3, msg.Height-7))
		m.help.Width = msg.Width
		if m.modal.kind != modalNone && m.modal.form != nil {
			form, cmd := m.modal.form.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				m.modal.form = f
			}
			m.modal.form.WithWidth(formWidth(msg.Width))
			return m, cmd
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(readStateCmd(), tickCmd())

	case refreshMsg:
		m.viewErr = msg.err
		if msg.err == nil {
			m.view = msg.view
			m.syncRows()
		}
		return m, nil

	case spinner.TickMsg:
		if m.busy.kind == busyNone {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch {
		case m.modal.kind == modalDetail:
			return m.updateDetail(msg)
		case m.modal.kind != modalNone:
			return m.updateForm(msg)
		}
		return m.updateBrowse(msg)

	case probeMsg:
		return m.handleProbe(msg)

	case saveDoneMsg:
		return m.handleSave(msg)

	case removeDoneMsg:
		return m.handleRemove(msg)

	case startDoneMsg:
		return m.handleStart(msg)

	case stopDoneMsg:
		return m.handleStop(msg)
	}

	// huh drives focus and validation through its own messages (nextFieldMsg,
	// cursor blinks, …); they must reach the form or enter never advances.
	if m.modal.kind != modalNone && m.modal.form != nil {
		return m.updateForm(msg)
	}
	return m, nil
}

func (m Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.cancel()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Open):
		return m.openDetail()
	case key.Matches(msg, m.keys.Add):
		return m.openFields(nil)
	case key.Matches(msg, m.keys.Edit):
		up, ok := m.selected()
		if !ok {
			return m.notify("select an upstream first", toneWarn), nil
		}
		return m.openFields(&up)
	case key.Matches(msg, m.keys.Remove):
		up, ok := m.selected()
		if !ok {
			return m.notify("select an upstream first", toneWarn), nil
		}
		if m.busy.kind != busyNone {
			return m.notify("another operation is still running", toneWarn), nil
		}
		*m.modal = modalState{kind: modalConfirmRemove, expected: upstream.Clone(&up)}
		m.modal.form = m.confirmRemoveForm(up)
		return m, m.modal.form.Init()
	case key.Matches(msg, m.keys.Test):
		up, ok := m.selected()
		if !ok {
			return m.notify("select an upstream first", toneWarn), nil
		}
		if !m.configUsable() {
			return m.notify(m.configProblem(), toneErr), nil
		}
		if m.busy.kind != busyNone {
			return m.notify("another operation is still running", toneWarn), nil
		}
		var seq int
		m, seq = m.beginBusy(busyProbe, "Reading the model list from "+up.BaseURL)
		return m, tea.Batch(probeCmd(m.ctx, seq, probeForTest, m.view.Cfg, up), m.spin.Tick)
	case key.Matches(msg, m.keys.Up):
		return m.startDaemon()
	case key.Matches(msg, m.keys.Down):
		return m.stopDaemon()
	case key.Matches(msg, m.keys.Refresh):
		return m, readStateCmd()
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// openDetail shows the selected upstream in a read-only popup. The snapshot is
// taken now, so a background refresh cannot change what is on screen.
func (m Model) openDetail() (tea.Model, tea.Cmd) {
	up, ok := m.selected()
	if !ok {
		return m.notify("select an upstream first", toneWarn), nil
	}
	*m.modal = modalState{kind: modalDetail, expected: upstream.Clone(&up)}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		return m.closeModal(), nil
	case "e", "t", "d":
		// Close the popup and run the browse action on the same row.
		m = m.closeModal()
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A write is already on its way to disk; its result decides what the form
	// does next, so keys are ignored until it lands.
	if m.busy.writing() {
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// huh does not bind esc; closing the modal is the TUI's job.
		if keyMsg.String() == "esc" {
			return m.closeModal(), nil
		}
	}
	if m.modal.form == nil {
		return m.closeModal(), nil
	}
	form, cmd := m.modal.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.modal.form = f
	}
	switch m.modal.form.State {
	case huh.StateCompleted:
		return m.modalCompleted()
	case huh.StateAborted:
		return m.closeModal(), nil
	}
	return m, cmd
}

func (m Model) modalCompleted() (tea.Model, tea.Cmd) {
	switch m.modal.kind {
	case modalFields:
		return m.submitFields()
	case modalModels:
		m.modal.up.Models = upstream.Trim(m.modal.modelSelected)
		return m.beginSave()
	case modalManualModels:
		m.modal.up.Models = upstream.ParseList(m.modal.manual)
		return m.beginSave()
	case modalRecovery:
		return m.recover()
	case modalConfirmRemove:
		if !m.modal.confirm {
			return m.closeModal(), nil
		}
		return m.beginRemove()
	case modalConfirmStop:
		if !m.modal.confirm {
			return m.closeModal(), nil
		}
		return m.beginStop()
	}
	return m, nil
}

func (m Model) openFields(existing *types.Upstream) (tea.Model, tea.Cmd) {
	if !m.configUsable() {
		return m.notify(m.configProblem(), toneErr), nil
	}
	if m.busy.kind != busyNone {
		return m.notify("another operation is still running", toneWarn), nil
	}

	*m.modal = modalState{
		kind:     modalFields,
		editing:  existing != nil,
		expected: upstream.Clone(existing),
	}
	if existing != nil {
		m.modal.name = existing.Name
		m.modal.protocol = existing.Protocol
		m.modal.baseURL = existing.BaseURL
	} else {
		m.modal.protocol = types.ProtocolOpenAI
		m.modal.baseURL = "http://127.0.0.1:11434"
	}
	m.modal.form = m.fieldsForm()
	return m, m.modal.form.Init()
}

func (m Model) submitFields() (tea.Model, tea.Cmd) {
	base, err := netguard.ParseBaseURL(m.modal.baseURL)
	if err != nil {
		return m.reopenFields(err), nil
	}

	var key *string
	switch {
	case m.modal.clearKey:
		empty := ""
		key = &empty
	default:
		key = upstream.KeyPointer(m.modal.expected, base.String(), m.modal.key)
	}

	built, err := upstream.Build(m.modal.expected, upstream.Draft{
		Name:     strings.TrimSpace(m.modal.name),
		Protocol: m.modal.protocol,
		BaseURL:  base.String(),
		Key:      key,
	})
	if err != nil {
		return m.reopenFields(err), nil
	}

	m.modal.up = built
	var seq int
	m, seq = m.beginBusy(busyProbe, "Reading the model list from "+built.BaseURL)
	return m, tea.Batch(probeCmd(m.ctx, seq, probeForModal, m.view.Cfg, built), m.spin.Tick)
}

func (m Model) reopenFields(err error) Model {
	m.modal.kind = modalFields
	m.modal.form = m.fieldsForm()
	m = m.notify(err.Error(), toneErr)
	return m
}

func (m Model) recover() (tea.Model, tea.Cmd) {
	switch m.modal.recovery {
	case "url":
		m.modal.kind = modalFields
		m.modal.form = m.fieldsForm()
		return m, m.modal.form.Init()
	case "manual":
		m.modal.kind = modalManualModels
		m.modal.form = m.manualForm()
		return m, m.modal.form.Init()
	case "retry":
		var seq int
		m, seq = m.beginBusy(busyProbe, "Reading the model list from "+m.modal.up.BaseURL)
		return m, tea.Batch(probeCmd(m.ctx, seq, probeForModal, m.view.Cfg, m.modal.up), m.spin.Tick)
	default:
		return m.closeModal(), nil
	}
}

func (m Model) beginSave() (tea.Model, tea.Cmd) {
	m.modal.pending = m.modal.kind
	var seq int
	m, seq = m.beginBusy(busySave, "Saving the upstream")
	return m, tea.Batch(
		saveCmd(seq, m.modal.up, upstream.Clone(m.modal.expected), m.modal.expected != nil),
		m.spin.Tick,
	)
}

func (m Model) beginRemove() (tea.Model, tea.Cmd) {
	expected := m.modal.expected
	if expected == nil {
		return m.closeModal(), nil
	}
	name := expected.Name
	var seq int
	m, seq = m.beginBusy(busyRemove, fmt.Sprintf("Removing upstream %q", name))
	m = m.closeModal()
	return m, tea.Batch(removeCmd(seq, name, expected), m.spin.Tick)
}

func (m Model) beginStop() (tea.Model, tea.Cmd) {
	pid := m.modal.pid
	var seq int
	m, seq = m.beginBusy(busyStop, fmt.Sprintf("Stopping noketd (pid %d)", pid))
	m = m.closeModal()
	return m, tea.Batch(stopCmd(seq, pid), m.spin.Tick)
}

func (m Model) startDaemon() (tea.Model, tea.Cmd) {
	if pid, running := noketd.Running(); running {
		return m.notify(fmt.Sprintf("noketd is already running (pid %d); not starting a second instance", pid), toneWarn), nil
	}
	if _, err := config.LoadCredentials(); err != nil {
		if errors.Is(err, config.ErrNoCredentials) {
			return m.notify("not logged in, run noket login first", toneWarn), nil
		}
		return m.notify(err.Error(), toneErr), nil
	}
	if m.busy.kind != busyNone {
		return m.notify("another operation is still running", toneWarn), nil
	}

	var seq int
	m, seq = m.beginBusy(busyStart, "Starting noketd")
	return m, tea.Batch(
		startCmd(m.ctx, seq, noketd.StartOptions{Verbose: m.opts.Verbose}),
		m.spin.Tick,
	)
}

func (m Model) stopDaemon() (tea.Model, tea.Cmd) {
	pid, running := noketd.RunningPID()
	if !running {
		if status, ok := noketd.LiveStatus(); ok {
			return m.notify(fmt.Sprintf("noketd is running in the foreground (pid %d); stop it with your supervisor. noket down only stops background mode.", status.PID), toneWarn), nil
		}
		config.RemovePID()
		config.RemoveStatus()
		return m.notify("noketd is not running.", toneInfo), nil
	}
	if m.busy.kind != busyNone {
		return m.notify("another operation is still running", toneWarn), nil
	}

	*m.modal = modalState{kind: modalConfirmStop, pid: pid}
	m.modal.form = m.confirmStopForm(pid)
	return m, m.modal.form.Init()
}

func (m Model) handleProbe(msg probeMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.busy.seq {
		return m, nil
	}
	m.busy = busyState{seq: m.busy.seq}

	if msg.purpose == probeForTest {
		m.results[resultKey(msg.name, msg.baseURL)] = testResult{models: len(msg.models), err: msg.err}
		m.syncRows()
		if msg.err != nil {
			return m.notify(fmt.Sprintf("%q: %v", msg.name, msg.err), toneErr), nil
		}
		return m.notify(fmt.Sprintf("%q: %d models", msg.name, len(msg.models)), toneOK), nil
	}

	if m.modal.kind == modalNone {
		return m, nil
	}
	if msg.err != nil {
		m.modal.probeErr = msg.err
		m.modal.kind = modalRecovery
		m.modal.recovery = "retry"
		m.modal.form = m.recoveryForm()
		return m, m.modal.form.Init()
	}

	m.modal.modelItems, m.modal.modelSelected = upstream.Choices(msg.models, m.modal.expected, m.modal.editing)
	m.modal.kind = modalModels
	m.modal.form = m.modelsForm()
	return m, m.modal.form.Init()
}

func (m Model) handleSave(msg saveDoneMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.busy.seq {
		return m, nil
	}
	m.busy = busyState{seq: m.busy.seq}
	m.applyConfig(msg.cfg)

	switch {
	case errors.Is(msg.err, upstream.ErrStale):
		m = m.closeModal()
		return m.notify("the upstream list changed on disk; reloading", toneWarn), nil
	case msg.err != nil:
		m.modal.kind = m.modal.pending
		switch m.modal.kind {
		case modalModels:
			m.modal.form = m.modelsForm()
		case modalManualModels:
			m.modal.form = m.manualForm()
		default:
			m.modal.form = m.fieldsForm()
		}
		m = m.notify(msg.err.Error(), toneErr)
		return m, m.modal.form.Init()
	default:
		editing := msg.editing
		m = m.closeModal()
		action := "Added"
		if editing {
			action = "Updated"
		}
		return m.notify(fmt.Sprintf("%s upstream %q", action, msg.name), toneOK), nil
	}
}

func (m Model) handleRemove(msg removeDoneMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.busy.seq {
		return m, nil
	}
	m.busy = busyState{seq: m.busy.seq}
	m.applyConfig(msg.cfg)

	switch {
	case errors.Is(msg.err, upstream.ErrStale):
		return m.notify("the upstream list changed on disk; reloading", toneWarn), nil
	case msg.err != nil:
		return m.notify(msg.err.Error(), toneErr), nil
	default:
		return m.notify(fmt.Sprintf("Removed upstream %q", msg.name), toneOK), nil
	}
}

func (m Model) handleStart(msg startDoneMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.busy.seq {
		return m, nil
	}
	m.busy = busyState{seq: m.busy.seq}

	if msg.err != nil {
		detail := firstLine(msg.err.Error())
		if msg.logPath != "" {
			detail += " · log " + msg.logPath
		}
		m = m.notify(detail, toneErr)
		return m, readStateCmd()
	}

	text := fmt.Sprintf("noketd started (pid %d)", msg.pid)
	if msg.status != nil && msg.status.NodeID != "" {
		text += " · node " + msg.status.NodeID
	}
	if msg.logPath != "" {
		text += " · log " + msg.logPath
	}
	m = m.notify(text, toneOK)
	return m, readStateCmd()
}

func (m Model) handleStop(msg stopDoneMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.busy.seq {
		return m, nil
	}
	m.busy = busyState{seq: m.busy.seq}
	if msg.err != nil {
		m = m.notify(msg.err.Error(), toneErr)
		return m, readStateCmd()
	}
	m = m.notify(fmt.Sprintf("noketd stopped (pid %d).", msg.pid), toneOK)
	return m, readStateCmd()
}

func (m Model) configUsable() bool {
	return m.viewErr == nil && m.view.CfgErr == nil && m.view.Cfg != nil
}

func (m Model) configProblem() string {
	if m.viewErr != nil {
		return m.viewErr.Error()
	}
	if m.view.CfgErr != nil {
		return "the config could not be read: " + m.view.CfgErr.Error()
	}
	return "the config is not loaded yet"
}

func (m *Model) applyConfig(cfg *types.Config) {
	if cfg == nil {
		return
	}
	m.view.Cfg = cfg
	m.view.CfgErr = nil
	m.syncRows()
}

func (m *Model) syncRows() {
	cursor := m.table.Cursor()
	m.rows = m.rows[:0]
	if m.view.Cfg != nil {
		for _, up := range m.view.Cfg.Upstreams {
			r := row{up: up}
			if result, ok := m.results[resultKey(up.Name, up.BaseURL)]; ok {
				r.test = result
				r.tested = true
			}
			m.rows = append(m.rows, r)
		}
	}

	rows := make([]table.Row, 0, len(m.rows))
	for _, r := range m.rows {
		key := "not set"
		if r.up.HasAPIKey() {
			key = "set"
		}
		test := "-"
		if r.tested {
			test = r.test.cell()
		}
		rows = append(rows, table.Row{
			r.up.Name,
			r.up.Protocol,
			r.up.BaseURL,
			strconv.Itoa(len(r.up.Models)),
			key,
			test,
		})
	}

	m.table.SetRows(rows)
	switch {
	case len(rows) == 0:
		m.table.SetCursor(0)
	case cursor >= len(rows):
		m.table.SetCursor(len(rows) - 1)
	default:
		m.table.SetCursor(cursor)
	}
}

func (m Model) selected() (types.Upstream, bool) {
	index := m.table.Cursor()
	if index < 0 || index >= len(m.rows) {
		return types.Upstream{}, false
	}
	return m.rows[index].up, true
}

func (m Model) beginBusy(kind busyKind, detail string) (Model, int) {
	m.busy = busyState{kind: kind, detail: detail, seq: m.busy.seq + 1}
	return m, m.busy.seq
}

func (m Model) closeModal() Model {
	*m.modal = modalState{}
	return m
}

func (m Model) notify(text string, tone toastTone) Model {
	m.toast = toastState{text: text, tone: tone, until: time.Now().Add(6 * time.Second)}
	return m
}

func resultKey(name, baseURL string) string { return name + "\x00" + baseURL }

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func readStateCmd() tea.Cmd {
	return func() tea.Msg {
		view, err := state.Read()
		return refreshMsg{view: view, err: err}
	}
}

func probeCmd(ctx context.Context, seq int, purpose probePurpose, cfg *types.Config, up types.Upstream) tea.Cmd {
	return func() tea.Msg {
		models, err := upstream.Probe(ctx, cfg, up)
		if err == nil && len(models) == 0 {
			err = errors.New("the upstream returned no models")
		}
		return probeMsg{
			seq:     seq,
			purpose: purpose,
			name:    up.Name,
			baseURL: up.BaseURL,
			models:  models,
			err:     err,
		}
	}
}

func saveCmd(seq int, up types.Upstream, expected *types.Upstream, editing bool) tea.Cmd {
	return func() tea.Msg {
		cfg, err := upstream.SaveFresh(up, expected)
		return saveDoneMsg{seq: seq, cfg: cfg, err: err, name: up.Name, editing: editing}
	}
}

func removeCmd(seq int, name string, expected *types.Upstream) tea.Cmd {
	return func() tea.Msg {
		cfg, err := upstream.RemoveFresh(name, expected)
		return removeDoneMsg{seq: seq, cfg: cfg, err: err, name: name}
	}
}

func startCmd(ctx context.Context, seq int, opts noketd.StartOptions) tea.Cmd {
	return func() tea.Msg {
		proc, err := noketd.Start(opts)
		if err != nil {
			return startDoneMsg{seq: seq, err: err}
		}
		status, err := noketd.AwaitRegistration(ctx, proc, noketd.StartupTimeout, nil)
		return startDoneMsg{
			seq:     seq,
			pid:     proc.PID,
			status:  status,
			logPath: proc.LogPath,
			err:     err,
		}
	}
}

func stopCmd(seq, pid int) tea.Cmd {
	return func() tea.Msg {
		return stopDoneMsg{seq: seq, pid: pid, err: noketd.Stop(pid, noketd.ShutdownTimeout)}
	}
}
