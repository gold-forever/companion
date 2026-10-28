//go:build !linux

package tray

import (
	"runtime"

	"fyne.io/systray"
)

// lookBeforeStart does nothing: Windows' and macOS' trays take their look once they are up.
func lookBeforeStart() {}

// lookOnceUp sets the icon and tooltip; no title, which macOS would show beside the icon.
func lookOnceUp() {
	if runtime.GOOS == "windows" {
		systray.SetIcon(icoIcon())
	} else {
		systray.SetIcon(pngIcon())
	}
	systray.SetTooltip(name)
}
