//go:build !windows

package alert

import (
	"slices"
	"strings"
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

// A question reaches the desktop the same way, with its two answers as the buttons, and the yes is
// told apart from the no, from closing it and from a desktop that cannot ask.
func TestAQuestionReachesTheDesktopAndItsYesIsToldApart(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		ways := questions(goos, "Gold Forever Companion", `v0.2.0 is "out"`, "Update now", "Later")
		if len(ways) == 0 || !slices.Contains(ways[0].args, `v0.2.0 is "out"`) || !strings.Contains(strings.Join(ways[0].args, "\x00"), "Update now") {
			t.Errorf("%s: %v", goos, ways)
		}
	}
	mac := questions("darwin", "T", "M", "Update now", "Later")[0]
	if yes, ok := mac.answer([]byte("button returned:Update now\n"), 0); !yes || !ok {
		t.Error("macOS: the yes button")
	}
	if yes, ok := mac.answer([]byte("button returned:Later\n"), 0); yes || !ok {
		t.Error("macOS: the no button")
	}
	linux := questions("linux", "T", "M", "Update now", "Later")
	if linux[0].args[0] != "notify-send" {
		t.Fatalf("Linux asks in a notification first: %q", linux[0].args)
	}
	for out, want := range map[string][2]bool{"yes\n": {true, true}, "no\n": {false, true}, "": {false, true}} {
		if yes, ok := linux[0].answer([]byte(out), 0); [2]bool{yes, ok} != want {
			t.Errorf("notify-send said %q: %v %v", out, yes, ok)
		}
	}
	if _, ok := linux[0].answer(nil, 1); ok {
		t.Error("a notify-send without actions cannot ask: the next way must")
	}
	zenity := linux[1]
	if yes, ok := zenity.answer(nil, 0); !yes || !ok {
		t.Error("zenity: OK is the yes")
	}
	if yes, ok := zenity.answer(nil, 1); yes || !ok {
		t.Error("zenity: Cancel, or closing it, is the no")
	}
	if _, ok := zenity.answer(nil, 255); ok {
		t.Error("zenity failing is no answer")
	}
}
