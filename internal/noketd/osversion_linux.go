//go:build linux

package noketd

import (
	"os"
	"strings"
)

func detectOSVersion() string {
	if content := readOSRelease(); content != "" {
		if pretty := parseOSRelease(content, "PRETTY_NAME"); pretty != "" {
			return pretty
		}
		if name := parseOSRelease(content, "NAME"); name != "" {
			if version := parseOSRelease(content, "VERSION_ID"); version != "" {
				return name + " " + version
			}
			return name
		}
	}
	if content, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		if version := strings.TrimSpace(string(content)); version != "" {
			return "Linux " + version
		}
	}
	return ""
}

func readOSRelease() string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if content, err := os.ReadFile(path); err == nil {
			return string(content)
		}
	}
	return ""
}
