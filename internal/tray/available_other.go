//go:build !linux

package tray

// Available is always true on Windows and macOS, which always show tray icons.
func Available() bool { return true }
