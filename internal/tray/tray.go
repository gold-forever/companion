// Package tray is the companion's icon by the clock: its status, and the way to the site, the settings
// and Quit. It wraps fyne.io/systray, which needs no C compiler on Windows and Linux.
package tray

import (
	"runtime"
	"time"

	"fyne.io/systray"
)

// Menu is what the tray shows and does.
type Menu struct {
	Status   func() string
	OpenSite func()
	Settings func()
	Quit     func()
}

// Run shows the icon and blocks until Stop; on macOS it must be called from the main goroutine.
func Run(m Menu) {
	systray.Run(func() { ready(m) }, func() {})
}

// Stop removes the icon and makes Run return.
func Stop() { systray.Quit() }

func ready(m Menu) {
	if runtime.GOOS == "windows" {
		systray.SetIcon(icoIcon())
	} else {
		systray.SetIcon(pngIcon())
	}
	systray.SetTooltip("Gold Forever Companion")
	status := systray.AddMenuItem(m.Status(), "")
	status.Disable()
	systray.AddSeparator()
	site := systray.AddMenuItem("Open Gold Forever", "The site, in your browser")
	settings := systray.AddMenuItem("Settings", "What the companion finds and sends")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Stop uploading until the companion starts again")
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-site.ClickedCh:
				m.OpenSite()
			case <-settings.ClickedCh:
				m.Settings()
			case <-quit.ClickedCh:
				m.Quit()
				return
			case <-tick.C:
				status.SetTitle(m.Status())
			}
		}
	}()
}
