//go:build !windows

package alert

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
