package cli

import (
	"io"
	"testing"

	"github.com/spf13/cobra"

	"github.com/noket-dev/noket/internal/config"
)

// Only --foreground waits for a login; `noket up` still reports it.
func TestUpFailsWithoutLogin(t *testing.T) {
	t.Setenv(config.DirEnv, t.TempDir())

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := runUp(cmd, false, "", "test")
	if err == nil || err.Error() != "not logged in, run noket login first" {
		t.Fatalf("runUp = %v, want the login error", err)
	}
}
