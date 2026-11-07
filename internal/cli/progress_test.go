package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestProgressModelStopsAdvancingAtTheLastStep(t *testing.T) {
	m := newProgressModel("Starting", []string{"one", "two", "three"})

	for _, want := range []int{1, 2, 2} {
		next, _ := m.Update(progressStepMsg{})
		m = next.(progressModel)
		if m.active != want {
			t.Fatalf("active = %d, want %d", m.active, want)
		}
	}
}

func TestProgressModelRendersActiveAndDoneSteps(t *testing.T) {
	m := newProgressModel("Starting noketd", []string{"one", "two", "three"})
	next, _ := m.Update(progressStepMsg{})
	m = next.(progressModel)
	next, _ = m.Update(progressDetailMsg("reconnecting"))
	m = next.(progressModel)

	view := m.View()
	for _, want := range []string{"Starting noketd", "✓ one", "two", "○ three", "reconnecting"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
	}
	if !strings.HasSuffix(view, "\n") {
		t.Fatal("view must end with a newline so the final line survives the renderer shutdown")
	}
}

func TestProgressModelMarksFailure(t *testing.T) {
	m := newProgressModel("Starting noketd", []string{"one", "two"})
	next, cmd := m.Update(progressDoneMsg{err: errors.New("boom")})
	if cmd == nil {
		t.Fatal("a finished program must quit")
	}
	m = next.(progressModel)

	view := m.View()
	for _, want := range []string{"✗ Starting noketd", "✗ one", "○ two"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestProgressModelMarksEveryStepDoneOnSuccess(t *testing.T) {
	m := newProgressModel("Starting noketd", []string{"one", "two"})
	next, _ := m.Update(progressDoneMsg{})
	m = next.(progressModel)

	view := m.View()
	if strings.Contains(view, "○") || strings.Contains(view, "✗") {
		t.Fatalf("success view still has pending markers:\n%s", view)
	}
	for _, want := range []string{"✓ Starting noketd", "✓ one", "✓ two"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestProgressIsSilentWithoutATerminal(t *testing.T) {
	var buf bytes.Buffer
	progress := startProgress(&buf, "Starting", "one")
	if progress.enabled() {
		t.Fatal("progress must stay disabled for a non-file writer")
	}

	progress.Advance()
	progress.Detail("detail")
	progress.Stop(nil)
	if buf.Len() != 0 {
		t.Fatalf("non-terminal output = %q, want nothing", buf.String())
	}
}
