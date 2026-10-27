//go:build !windows

package alert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Show prints the error and shows it on the desktop: a companion started by a double-click or at
// login has no terminal, so stderr alone would reach nobody.
func Show(title, message string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, message)
	for _, c := range alertCommands(runtime.GOOS, title, message) {
		if _, err := exec.LookPath(c[0]); err == nil && exec.Command(c[0], c[1:]...).Run() == nil {
			return
		}
	}
}

// alertCommands are the ways to show an error on goos's desktop, best first. The text travels as
// arguments, never inside a script, so nothing in a message can run as code.
func alertCommands(goos, title, message string) [][]string {
	if goos == "darwin" {
		return [][]string{{"osascript",
			"-e", "on run argv", "-e", "display alert (item 1 of argv) message (item 2 of argv)", "-e", "end run",
			title, message}}
	}
	return [][]string{
		{"notify-send", "--app-name", title, title, message},
		{"zenity", "--error", "--title", title, "--text", message},
		{"kdialog", "--title", title, "--error", message},
	}
}

// Ask puts a question with two answers on the desktop and waits for it: true for yes. Closing it, or
// a desktop with no way to ask, is no.
func Ask(ctx context.Context, title, message, yes, no string) bool {
	for _, q := range questions(runtime.GOOS, title, message, yes, no) {
		if _, err := exec.LookPath(q.args[0]); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, q.args[0], q.args[1:]...).Output()
		code := 0
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			continue
		}
		if said, ok := q.answer(out, code); ok {
			return said
		}
	}
	return false
}

// question is one way to ask on a desktop: its command, and how its output and exit code answer it;
// not ok when it could not ask.
type question struct {
	args   []string
	answer func(out []byte, code int) (yes, ok bool)
}

// questions are the ways to ask on goos's desktop, best first, the text as arguments as alerts' is.
func questions(goos, title, message, yes, no string) []question {
	if goos == "darwin" {
		return []question{{
			args: []string{"osascript", "-e", "on run argv",
				"-e", "display dialog (item 2 of argv) with title (item 1 of argv) buttons {item 4 of argv, item 3 of argv} default button 2",
				"-e", "end run", title, message, yes, no},
			answer: func(out []byte, code int) (bool, bool) {
				return strings.TrimSpace(string(out)) == "button returned:"+yes, code == 0
			},
		}}
	}
	// zenity and kdialog answer 0 for yes and 1 for no or closed.
	dialog := func(out []byte, code int) (bool, bool) { return code == 0, code == 0 || code == 1 }
	return []question{
		{
			args: []string{"notify-send", "--app-name", title, "--wait", "--action=yes=" + yes, "--action=no=" + no, title, message},
			answer: func(out []byte, code int) (bool, bool) {
				return strings.TrimSpace(string(out)) == "yes", code == 0
			},
		},
		{args: []string{"zenity", "--question", "--title", title, "--text", message, "--ok-label", yes, "--cancel-label", no}, answer: dialog},
		{args: []string{"kdialog", "--title", title, "--yesno", message, "--yes-label", yes, "--no-label", no}, answer: dialog},
	}
}
