package noketd

import (
	"strings"
	"sync"
)

// maxOSVersionLength matches the server-side bound on NodeConfig fields.
const maxOSVersionLength = 64

var (
	osVersionOnce sync.Once
	osVersion     string
)

// hostOSVersion returns the host operating system as one display string, for
// example "Ubuntu 24.04.1 LTS" or "macOS 15.5". Detection runs once; a platform
// that cannot be identified returns an empty string.
func hostOSVersion() string {
	osVersionOnce.Do(func() { osVersion = truncateOSVersion(detectOSVersion()) })
	return osVersion
}

// parseOSRelease returns the unquoted value of key from os-release content,
// ignoring comments and blank lines.
func parseOSRelease(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func truncateOSVersion(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxOSVersionLength {
		return value
	}
	return string(runes[:maxOSVersionLength])
}
