//go:build !windows

package noketd

import (
	"os/exec"
	"syscall"
)

func configureDetached(cmd *exec.Cmd) {
	// setsid detaches noketd from the terminal's process group and keeps it
	// alive after the CLI exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func exeSuffix() string { return "" }
