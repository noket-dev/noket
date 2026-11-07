package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestCanUseFormRejectsNonTerminalIO(t *testing.T) {
	cmd := &cobra.Command{}
	var in, out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)

	if canUseForm(cmd) {
		t.Fatal("canUseForm must be false for buffers")
	}
}

func TestRunFormFlowSkipsTheFormWithoutATerminal(t *testing.T) {
	cmd := &cobra.Command{}
	var in, out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)

	called := false
	handled, err := runFormFlow(cmd, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("runFormFlow: %v", err)
	}
	if handled || called {
		t.Fatalf("handled = %v, called = %v; want both false", handled, called)
	}
}

func TestPickUpstreamFormSkipsWithoutATerminal(t *testing.T) {
	cmd := &cobra.Command{}
	var in, out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)

	index, handled, err := pickUpstreamForm(cmd, []string{"a", "b"})
	if handled || err != nil || index != 0 {
		t.Fatalf("index = %d, handled = %v, err = %v; want the numbered fallback", index, handled, err)
	}
}

func TestConfirmRemoveSkipsWithoutATerminal(t *testing.T) {
	cmd := &cobra.Command{}
	var in, out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)

	ok, handled, err := confirmRemove(cmd, "ollama", 2)
	if ok || handled || err != nil {
		t.Fatalf("ok = %v, handled = %v, err = %v; want the y/N fallback", ok, handled, err)
	}
}

func TestReportCanceledPrintsOneLine(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := reportCanceled(cmd); err != nil {
		t.Fatalf("reportCanceled: %v", err)
	}
	if got := out.String(); got != "Canceled.\n" {
		t.Fatalf("output = %q, want %q", got, "Canceled.\n")
	}
}

func TestAskNameValidatorRejectsUnsafeNames(t *testing.T) {
	// The form field validates with config.ValidateName; guard the rule the
	// picker relies on so a slash can never reach the config file.
	for _, bad := range []string{"", "a/b", "with space"} {
		if err := validateNameForForm(bad); err == nil {
			t.Fatalf("validateNameForForm(%q) accepted an invalid name", bad)
		}
	}
	if err := validateNameForForm("ollama.local-1"); err != nil {
		t.Fatalf("validateNameForForm rejected a valid name: %v", err)
	}
}

func TestValidateNameForFormTrims(t *testing.T) {
	if err := validateNameForForm("  ollama  "); err != nil {
		t.Fatalf("validateNameForForm must ignore surrounding spaces: %v", err)
	}
	if err := validateNameForForm("   "); err == nil {
		t.Fatal("validateNameForForm accepted a blank name")
	}
}
