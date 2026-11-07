//go:build windows

package noketd

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func detectOSVersion() string {
	info := windows.RtlGetVersion()
	if info.MajorVersion == 0 {
		return ""
	}

	var name string
	switch {
	case info.MajorVersion == 10 && info.BuildNumber >= 22000:
		name = "Windows 11"
	case info.MajorVersion == 10:
		name = "Windows 10"
	case info.MajorVersion == 6 && info.MinorVersion == 3:
		name = "Windows 8.1"
	case info.MajorVersion == 6 && info.MinorVersion == 2:
		name = "Windows 8"
	case info.MajorVersion == 6 && info.MinorVersion == 1:
		name = "Windows 7"
	default:
		name = fmt.Sprintf("Windows %d.%d", info.MajorVersion, info.MinorVersion)
	}
	return fmt.Sprintf("%s build %d", name, info.BuildNumber)
}
