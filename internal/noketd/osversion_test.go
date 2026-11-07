package noketd

import (
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	content := strings.Join([]string{
		`# PRETTY_NAME is quoted by convention`,
		`PRETTY_NAME="Ubuntu 24.04.1 LTS"`,
		`NAME=Ubuntu`,
		`VERSION_ID="24.04"`,
		`EMPTY=`,
		`FALSY="'unbalanced"`,
	}, "\n")

	cases := map[string]string{
		"PRETTY_NAME":   "Ubuntu 24.04.1 LTS",
		"NAME":          "Ubuntu",
		"VERSION_ID":    "24.04",
		"EMPTY":         "",
		"MISSING":       "",
		"# PRETTY_NAME": "",
	}
	for key, want := range cases {
		if got := parseOSRelease(content, key); got != want {
			t.Errorf("parseOSRelease(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestTruncateOSVersion(t *testing.T) {
	if got := truncateOSVersion("  macOS 15.5  "); got != "macOS 15.5" {
		t.Errorf("trimmed = %q", got)
	}
	long := strings.Repeat("x", maxOSVersionLength+10)
	if got := truncateOSVersion(long); len([]rune(got)) != maxOSVersionLength {
		t.Errorf("truncated length = %d, want %d", len([]rune(got)), maxOSVersionLength)
	}
}
