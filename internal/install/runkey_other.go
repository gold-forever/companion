//go:build !windows

package install

// RunKey exists on Windows only.
func RunKey() Autostart { return nil }
