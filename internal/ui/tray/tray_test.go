package tray

import (
	"testing"
	"time"
)

type fakeApp struct {
	status                 string
	sites, settings, quits int
}

func (a *fakeApp) Status() string { return a.status }
func (a *fakeApp) OpenSite()      { a.sites++ }
func (a *fakeApp) OpenSettings()  { a.settings++ }
func (a *fakeApp) Quit()          { a.quits++ }

func TestTheMenuReachesTheAppUntilQuit(t *testing.T) {
	app := &fakeApp{status: "Uploaded GoldForever.lua"}
	site, settings, quit := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tick := make(chan time.Time)
	var shown []string
	done := make(chan struct{})
	go func() {
		follow(app, clicks{site: site, settings: settings, quit: quit}, tick, func(s string) { shown = append(shown, s) })
		close(done)
	}()

	site <- struct{}{}
	settings <- struct{}{}
	tick <- time.Time{}
	quit <- struct{}{}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the menu still runs after Quit")
	}
	if app.sites != 1 || app.settings != 1 || app.quits != 1 {
		t.Errorf("site %d, settings %d, quit %d: want each once", app.sites, app.settings, app.quits)
	}
	if len(shown) != 1 || shown[0] != "Uploaded GoldForever.lua" {
		t.Errorf("status shown %q", shown)
	}
}

func TestTheStatusSaysStartingBeforeTheFirstLine(t *testing.T) {
	if got := statusOf(&fakeApp{}); got != "Starting…" {
		t.Errorf("status = %q", got)
	}
	if got := statusOf(&fakeApp{status: "Watching 2 saves"}); got != "Watching 2 saves" {
		t.Errorf("status = %q", got)
	}
}
