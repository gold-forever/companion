// Package tray is the companion's icon by the clock: its status, and the way to the site, the settings
// and Quit. It wraps fyne.io/systray, which needs no C compiler on Windows and Linux.
package tray

import (
	"context"
	"runtime"
	"sync"
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

// Run shows the icon until ctx ends or the player quits, and returns once its menu has stopped; on
// macOS it must be called from the main goroutine.
func Run(ctx context.Context, app App) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(ctx, systray.Quit)()
	// The menu is built on systray's own goroutine; the clicks are followed on one Run waits for.
	built := make(chan menu, 1)
	var following sync.WaitGroup
	following.Go(func() {
		select {
		case m := <-built:
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			follow(ctx, app, m.clicks, tick.C, m.status.SetTitle)
		case <-ctx.Done():
		}
	})
	systray.Run(func() { built <- build(app) }, cancel)
	cancel()
	following.Wait()
}

// menu is the built menu: its status line and the items that send clicks.
type menu struct {
	status *systray.MenuItem
	clicks clicks
}

func build(app App) menu {
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
	return menu{status: status, clicks: clicks{site: site.ClickedCh, settings: settings.ClickedCh, quit: quit.ClickedCh}}
}

// clicks are the menu's items, each sending when clicked.
type clicks struct {
	site, settings, quit <-chan struct{}
}

// follow passes each click on to app until Quit or ctx's end, and shows app's status at every tick.
func follow(ctx context.Context, app App, c clicks, tick <-chan time.Time, show func(status string)) {
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
		case <-ctx.Done():
			return
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
