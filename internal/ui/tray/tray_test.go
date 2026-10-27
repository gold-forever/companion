package tray

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeApp struct {
	status, account                 string
	update                          atomic.Value // the version Update now names, set while the menu runs
	sites, settings, quits, updates int
}

func (a *fakeApp) Status() string        { return a.status }
func (a *fakeApp) Account() string       { return a.account }
func (a *fakeApp) OpenSite()             { a.sites++ }
func (a *fakeApp) OpenSettings()         { a.settings++ }
func (a *fakeApp) Quit()                 { a.quits++ }
func (a *fakeApp) UpdateVersion() string { v, _ := a.update.Load().(string); return v }
func (a *fakeApp) UpdateNow()            { a.updates++ }

func TestTheMenuReachesTheAppUntilQuit(t *testing.T) {
	app := &fakeApp{status: "Uploaded GoldForever.lua", account: "Goldsmith#1234"}
	site, settings, quit := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tick := make(chan time.Time)
	var shown, accounts []string
	done := make(chan struct{})
	go func() {
		follow(t.Context(), app, clicks{site: site, settings: settings, quit: quit}, tick, func(s, a, _ string) {
			shown, accounts = append(shown, s), append(accounts, a)
		})
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
	if len(accounts) != 1 || accounts[0] != "Uploading to Goldsmith#1234" {
		t.Errorf("account shown %q", accounts)
	}
}

func TestTheMenuStopsWhenTheCompanionStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		follow(ctx, &fakeApp{}, clicks{}, nil, func(string, string, string) {})
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the menu still runs after the companion stopped")
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

func TestTheAccountLineShowsOnlyOnceTheAccountIsKnown(t *testing.T) {
	if got := accountOf(&fakeApp{}); got != "" {
		t.Errorf("account line = %q", got)
	}
	if got := accountOf(&fakeApp{account: "Goldsmith"}); got != "Uploading to Goldsmith" {
		t.Errorf("account line = %q", got)
	}
}

func TestUpdateNowShowsWhileAnUpdateIsKnownAndRunsTheAppsUpdate(t *testing.T) {
	app := &fakeApp{}
	update, quit := make(chan struct{}), make(chan struct{})
	tick := make(chan time.Time)
	var shown []string
	done := make(chan struct{})
	go func() {
		follow(t.Context(), app, clicks{update: update, quit: quit}, tick, func(_, _, u string) { shown = append(shown, u) })
		close(done)
	}()

	tick <- time.Time{}
	app.update.Store("v0.2.0")
	tick <- time.Time{}
	update <- struct{}{}
	quit <- struct{}{}
	<-done

	if len(shown) != 2 || shown[0] != "" || shown[1] != "Update now to v0.2.0" {
		t.Errorf("update item %q", shown)
	}
	if app.updates != 1 {
		t.Errorf("%d updates", app.updates)
	}
}
