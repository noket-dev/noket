//go:build darwin

package noketd

import (
	"os/exec"
	"strings"
)

func detectOSVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	if version := strings.TrimSpace(string(out)); version != "" {
		return "macOS " + version
	}
	return ""
}
