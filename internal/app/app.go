// Package app is the running companion: its settings and their changes, linking, the saves found and
// switched on or off, restore, uninstall, version checks, the upload loop, and the State the settings
// page shows.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// RefusedFile, in the data folder, lists the saves the server refused as too large.
const RefusedFile = "refused.json"

// The timings and data folder files the app shares with `run`.
const (
	PollEvery = 2 * time.Second
	// The prices' freshness without an upload of one's own: most refreshes cost a 304 (D-013.4).
	RefreshPricesEvery = time.Hour
	// Where the ETag of the prices in the game is kept, in the data folder.
	PricesState = "prices.json"
	KeepCopies  = 10
	KeepLogFor  = 30 * 24 * time.Hour
	// InstanceFile, in the data folder, says which copy runs and where its page is.
	InstanceFile = "instance.json"
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
	status     *lastLine
	prices     *addondata.Report // how each price fetch and write went, kept across the loop's restarts
	// linker links by code: it shows the code, waits for the approval and saves the token with save.
	linker func(ctx context.Context, show func(server.DeviceCode), save func(token string) error) error
	// openURL opens a page in the player's browser.
	openURL func(url string)

	mu      sync.Mutex
	notice  *page.Notice
	warning string // what the page must tell the player about their settings
	linking *page.Linking
	games   map[string]gameSeen
}

type gameSeen struct {
	modified time.Time
	size     int64
	game     string
}

type appOptions struct {
	version, site, running, cfgPath, dataDir string
	layout                                   install.Layout
	candidates                               []string
	quit                                     func()
}

func newApp(o appOptions) *App {
	settings, err := config.Open(o.cfgPath)
	var warning string
	if err != nil {
		warning = setAsideSettings(o.cfgPath, err)
		settings = config.New(o.cfgPath, config.Config{})
	}
	c := &App{
		version: o.version, site: o.site, running: o.running, layout: o.layout,
		settings: settings, dataDir: o.dataDir, candidates: o.candidates, quit: o.quit,
		backups: &backup.Store{Dir: filepath.Join(o.dataDir, "backups"), Keep: KeepCopies, Now: time.Now},
		log:     &sendlog.Log{Path: filepath.Join(o.dataDir, "sent.jsonl"), Keep: KeepLogFor},
		refused: refused.Load(filepath.Join(o.dataDir, RefusedFile)),
		status:  &lastLine{},
		prices:  &addondata.Report{Now: time.Now},
		warning: warning,
		games:   map[string]gameSeen{},
		openURL: func(string) {},
	}
	c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
		hostname, _ := os.Hostname()
		client := &server.Client{BaseURL: c.url(), Version: c.version, OS: goos}
		return link.Run(ctx, client, hostname, show, save)
	}
	return c
}

// State is what the settings page shows.
func (c *App) State() page.State {
	cfg := c.settings.Get()
	s := page.State{
		Version:   c.version,
		Site:      c.url(),
		Linked:    cfg.Token != "",
		Sharing:   link.SharingLine,
		Warning:   c.warning,
		Status:    c.status.String(),
		FirstRun:  !cfg.Welcomed,
		Installed: c.installed(),
		Autostart: c.layout.Autostart.Enabled(),
		Folders:   cfg.Folders,
	}
	for _, save := range c.saves() {
		p := page.Save{Path: save.Path, Label: save.Label(), Game: c.gameOf(save.Path), On: !cfg.IsOff(save.Path)}
		if c.refused.Holds(save.Path) {
			p.Note = "refused as too large, retrying daily"
		}
		s.Saves = append(s.Saves, p)
	}
	for _, f := range c.prices.Files(c.saves()) {
		s.Prices = append(s.Prices, priceFile(f))
	}
	c.mu.Lock()
	s.Notice = c.notice
	if c.linking != nil {
		l := *c.linking
		s.Linking = &l
	}
	c.mu.Unlock()
	return s
}

// priceFile is a flavour's price file as the page shows it, its folder named as saves are.
func priceFile(f addondata.File) page.PriceFile {
	p := page.PriceFile{
		Flavour: filepath.Base(filepath.Dir(f.Flavour)) + " › " + filepath.Base(f.Flavour),
		Game:    f.Game, Items: f.Items, Size: f.Size, Outcome: f.Outcome, Error: f.Error,
	}
	for _, m := range f.Markets {
		p.Markets = append(p.Markets, page.PriceMarket{Market: m.Key, Items: m.Items})
	}
	if !f.Fetched.IsZero() {
		p.Fetched = &f.Fetched
	}
	if f.Dings != nil {
		p.Dings = dingsLine(*f.Dings)
	}
	if f.Shared != nil {
		p.Shared = fmt.Sprintf("Lists: %d · Profiles: %d", f.Shared.Lists, f.Shared.Profiles)
	}
	return p
}

// dingsLine is the in-game dings switch as a price file carries it: "Dings: webhook on · 2 characters on".
func dingsLine(d addondata.Dings) string {
	webhook, on := "off", 0
	if d.Webhook {
		webhook = "on"
	}
	for _, choice := range d.Characters {
		if choice {
			on++
		}
	}
	characters := "characters"
	if on == 1 {
		characters = "character"
	}
	return fmt.Sprintf("Dings: webhook %s · %d %s on", webhook, on, characters)
}

// refresher keeps the account's prices, dings state and shared lists and profiles in the game, fetched
// with f, d and s.
func (c *App) refresher(f addondata.Fetcher, d addondata.DingsFetcher, s addondata.SharedFetcher) *addondata.Refresher {
	return c.parts(nil).refresher(f, d, s)
}

// parts is what an upload round with server is built from, with the current settings.
func (c *App) parts(server *server.Client) Parts {
	return Parts{
		Server: server, Backups: c.backups, Log: c.log, Refused: c.refused, Saves: c.saves, IsOff: c.isOff,
		Market: c.settings.Get().Market, DataDir: c.dataDir, Out: c.status, Report: c.prices,
	}
}

// gameOf reads which game wrote a save, again only when the file has changed.
func (c *App) gameOf(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	c.mu.Lock()
	seen, ok := c.games[path]
	c.mu.Unlock()
	if ok && seen.modified.Equal(info.ModTime()) && seen.size == info.Size() {
		return seen.game
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	game := find.Game(data)
	c.mu.Lock()
	c.games[path] = gameSeen{info.ModTime(), info.Size(), game}
	c.mu.Unlock()
	return game
}
