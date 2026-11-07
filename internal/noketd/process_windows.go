//go:build windows

package noketd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ProcessAlive reports whether pid exists.
func ProcessAlive(pid int) bool {
	return tasklist(pid) != ""
}

// Terminate stops pid.
func Terminate(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func isNoketdProcess(pid int) bool {
	return strings.Contains(strings.ToLower(tasklist(pid)), "noket")
}

// tasklist returns the CSV row for pid, or an empty string when it is gone.
func tasklist(pid int) string {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if line == "" || strings.Contains(line, "INFO:") {
		return ""
	}
	if !strings.Contains(line, strconv.Itoa(pid)) {
		return ""
	}
	return line
}
