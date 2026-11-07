package noketd

import (
	"os"

	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

// RunningPID returns the managed pid when it still belongs to a live noketd.
// A stale pid or status file is removed.
func RunningPID() (int, bool) {
	pid, err := config.ReadPID()
	if err != nil {
		return 0, false
	}
	if !ProcessAlive(pid) || !isNoketdProcess(pid) {
		config.RemovePID()
		config.RemoveStatus()
		return 0, false
	}
	return pid, true
}

// Running also covers `noket up --foreground`, which has no pid file but
// publishes a status file.
func Running() (int, bool) {
	if status, ok := LiveStatus(); ok {
		return status.PID, true
	}
	return RunningPID()
}

// IsDaemonProcess reports whether pid belongs to a noket or noketd process.
func IsDaemonProcess(pid int) bool { return isNoketdProcess(pid) }

// LiveStatus returns the published status when its pid is a live noketd.
func LiveStatus() (*types.DaemonStatus, bool) {
	status, err := config.ReadStatus()
	if err != nil || status.PID <= 0 || status.PID == os.Getpid() {
		return nil, false
	}
	if !ProcessAlive(status.PID) || !isNoketdProcess(status.PID) {
		return nil, false
	}
	return status, true
}
