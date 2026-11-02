// Package app is the running companion: its settings and their changes, linking, the saves found and
// switched on or off, restore, uninstall, version checks, the upload loop, and the State the settings
// page shows.
package app

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/platform/alert"
	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/platform/instance"
	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// The timings and the files the app keeps in the data folder.
const (
	pollEvery = 2 * time.Second
	// The prices' freshness without an upload of one's own: most refreshes cost a 304 (D-013.4).
	refreshPricesEvery = time.Hour
	// Where the ETag of the prices in the game is kept, in the data folder.
	pricesState = "prices.json"
	keepCopies  = 10
	keepLogFor  = 30 * 24 * time.Hour
	// sentPerPage is how many uploads a page of "What was sent" shows.
	sentPerPage = 10
	// downloadFor is how long downloading an update may take.
	downloadFor = 10 * time.Minute
	// instanceFile, in the data folder, says which copy runs and where its page is.
	instanceFile = "instance.json"
	// refusedFile, in the data folder, lists the saves the server refused as too large.
	refusedFile = "refused.json"
	// remindedFile, in the data folder, says which release the player was last reminded of, and when.
	remindedFile = "reminded.json"
)

const goos = runtime.GOOS

// App is the running companion: what the settings page and the tray show and change, and the upload
// loop, which starts again whenever the token or the folders change.
type App struct {
	version    string
	site       string
	running    string
	layout     install.Layout
	settings   *config.Settings // the settings, which announce each change the loop must see
	dataDir    string
	candidates []string
	backups    *backup.Store
	log        *sendlog.Log
	refused    *refused.List
	quit       func()
	// life ends when the companion stops; every goroutine the app starts is in owned, which ends with it.
	life   context.Context
	owned  sync.WaitGroup
	status *lastLine
	prices *addondata.Report // how each price fetch and write went, kept across the loop's restarts
	// connect is the server at the settings' site, asked with token ("" for what needs none).
	connect func(token string) connection
	// linker links by code: it shows the code, waits for the approval and saves the token with save.
	linker func(ctx context.Context, show func(server.DeviceCode), save func(token string) error) error
	// openURL opens a page in the player's browser.
	openURL func(url string)
	// pageURL is the settings page's address, with its secret; tab is its port and secret.
	pageURL string
	tab     instance.Info
	// releases downloads a release's files; releaseFile is this system's file in a release, "" where
	// the companion does not install itself (macOS); runInstaller starts a downloaded companion.
	releases     *http.Client
	releaseFile  string
	runInstaller func(path string) error
	// ask puts a question to the player on the desktop: true for its yes. now is the clock.
	ask func(ctx context.Context, message, yes, no string) bool
	now func() time.Time
	// olderRuns says whether an older copy's process still runs, and closeOlder stops it; olderGrace is
	// how long one that was asked to quit has to exit.
	olderRuns  func(instance.Info) bool
	closeOlder func(instance.Info) error
	olderGrace time.Duration

	mu      sync.Mutex
	notice  *page.Notice
	update  page.Update
	warning string // what the page must tell the player about their settings
	linking *page.Linking
	older   *instance.Info // an older copy still running beside this one, which could not be asked to quit
	account knownAccount
	games   map[string]gameSeen
}

// connection is everything the app asks of the server.
type connection interface {
	Server
	accountAsker
	releaseAsker
}

type appOptions struct {
	version, site, running, cfgPath, dataDir string
	layout                                   install.Layout
	candidates                               []string
	quit                                     func()
	life                                     context.Context
}

func newApp(o appOptions) *App {
	settings, err := config.Open(o.cfgPath)
	var warning string
	if err != nil {
		warning = setAsideSettings(o.cfgPath, err)
		settings = config.New(o.cfgPath, config.Config{})
	}
	records := OpenRecords(o.dataDir)
	c := &App{
		version: o.version, site: o.site, running: o.running, layout: o.layout,
		settings: settings, dataDir: o.dataDir, candidates: o.candidates, quit: o.quit, life: o.life,
		backups: records.Backups, log: records.Log, refused: records.Refused,
		status:       &lastLine{},
		prices:       &addondata.Report{Now: time.Now},
		warning:      warning,
		games:        map[string]gameSeen{},
		openURL:      func(string) {},
		releases:     &http.Client{Timeout: downloadFor},
		releaseFile:  releaseFile(goos, runtime.GOARCH),
		runInstaller: install.Run,
		ask: func(ctx context.Context, message, yes, no string) bool {
			return alert.Ask(ctx, "Gold Forever Companion", message, yes, no)
		},
		now:        time.Now,
		olderRuns:  instance.Running,
		closeOlder: instance.Close,
		olderGrace: olderGrace,
	}
	c.connect = func(token string) connection {
		return &server.Client{BaseURL: c.url(), Token: token, Version: c.version, OS: goos}
	}
	c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
		hostname, _ := os.Hostname()
		client := &server.Client{BaseURL: c.url(), Version: c.version, OS: goos}
		return link.Run(ctx, client, hostname, show, save)
	}
	return c
}

// parts is what an upload round with server is built from, with the current settings.
func (c *App) parts(server Server) Parts {
	return Parts{
		Server: server, Records: Records{Backups: c.backups, Log: c.log, Refused: c.refused}, Saves: c.saves, IsOff: c.isOff,
		Market: c.settings.Get().Market, DataDir: c.dataDir, Out: c.status, Report: c.prices,
	}
}
