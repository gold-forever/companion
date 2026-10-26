//go:build !windows

package alert

import (
	"slices"
	"testing"
)

// A double-click has no terminal, so an error must reach the desktop: the text travels as arguments,
// never inside a script, so nothing in a message can run as code.
func TestAnAlertReachesTheDesktop(t *testing.T) {
	mac := alertCommands("darwin", "Gold Forever Companion", `it said "no"`)
	if len(mac) != 1 || mac[0][0] != "osascript" || !slices.Contains(mac[0], `it said "no"`) {
		t.Errorf("macOS shows a dialog with the message as an argument: %q", mac)
	}
	linux := alertCommands("linux", "Gold Forever Companion", "the server is down")
	if len(linux) == 0 || linux[0][0] != "notify-send" || linux[0][len(linux[0])-1] != "the server is down" {
		t.Errorf("Linux sends a desktop notification first: %q", linux)
	}
}
