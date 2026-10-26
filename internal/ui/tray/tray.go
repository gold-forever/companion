// Package tray is the companion's icon by the clock: its status, and the way to the site, the settings
// and Quit. It wraps fyne.io/systray, which needs no C compiler on Windows and Linux.
package tray

import (
	"runtime"
	"time"

	"fyne.io/systray"
)

// App is what the tray shows and does.
type App interface {
	Status() string // the latest line the uploads wrote; "" before the first
	OpenSite()
	OpenSettings()
	Quit()
}

// Run shows the icon and blocks until Stop; on macOS it must be called from the main goroutine.
func Run(app App) {
	systray.Run(func() { ready(app) }, func() {})
}

// Stop removes the icon and makes Run return.
func Stop() { systray.Quit() }

func ready(app App) {
	if runtime.GOOS == "windows" {
		systray.SetIcon(icoIcon())
	} else {
		systray.SetIcon(pngIcon())
	}
	systray.SetTooltip("Gold Forever Companion")
	status := systray.AddMenuItem(statusOf(app), "")
	status.Disable()
	systray.AddSeparator()
	site := systray.AddMenuItem("Open Gold Forever", "The site, in your browser")
	settings := systray.AddMenuItem("Settings", "What the companion finds and sends")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Stop uploading until the companion starts again")
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		follow(app, clicks{site: site.ClickedCh, settings: settings.ClickedCh, quit: quit.ClickedCh}, tick.C, status.SetTitle)
	}()
}

// clicks are the menu's items, each sending when clicked.
type clicks struct {
	site, settings, quit <-chan struct{}
}

// follow passes each click on to app until Quit, and shows app's status at every tick.
func follow(app App, c clicks, tick <-chan time.Time, show func(status string)) {
	for {
		select {
		case <-c.site:
			app.OpenSite()
		case <-c.settings:
			app.OpenSettings()
		case <-c.quit:
			app.Quit()
			return
		case <-tick:
			show(statusOf(app))
		}
	}
}

// statusOf is the line the menu shows.
func statusOf(app App) string {
	if s := app.Status(); s != "" {
		return s
	}
	return "Starting…"
}
