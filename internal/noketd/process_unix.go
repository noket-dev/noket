//go:build !windows

package noketd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ProcessAlive reports whether pid exists.
func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Terminate asks pid to shut down.
func Terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// isNoketdProcess reports whether pid is a noket or noketd process. The check
// keeps `noket down` and `noket logout` from signaling an unrelated process
// that reused a stale pid.
func isNoketdProcess(pid int) bool {
	if pid <= 0 || !processOwnedByCurrentUser(pid) {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return false
	}

	// Accept the standalone noketd binary, the hidden `noket noketd`
	// subcommand and `noket up --foreground`.
	fields := strings.Fields(line)
	for i, field := range fields {
		switch filepath.Base(field) {
		case "noketd", "noketd.exe":
			return true
		case "noket", "noket.exe":
			if i+1 < len(fields) && (fields[i+1] == "noketd" || fields[i+1] == "up") {
				return true
			}
		}
	}
	return false
}

func processOwnedByCurrentUser(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "uid=").Output()
	if err != nil {
		return false
	}
	uid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return err == nil && uid == os.Getuid()
}
