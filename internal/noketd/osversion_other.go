//go:build !linux && !darwin && !windows

package noketd

func detectOSVersion() string { return "" }
