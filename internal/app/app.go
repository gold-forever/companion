// Package app is the running companion: its settings and their changes, linking, the saves found and
// switched on or off, restore, uninstall, version checks, the upload loop, and the State the settings
// page shows.
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/platform/install"
	release "github.com/gold-forever/companion/internal/platform/version"
	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// A token as /me/tokens makes one: gf_ and 43 letters and digits.
var tokenShape = regexp.MustCompile(`^gf_[0-9A-Za-z]{43}$`)

const checkVersionEvery = 24 * time.Hour

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
	cfgPath    string
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
	cfg     config.Config
	notice  *page.Notice
	warning string // what the page must tell the player about their settings
	linking *page.Linking
	changed chan struct{} // closed and replaced on every change the loop must see
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
	cfg, err := config.Load(o.cfgPath)
	var warning string
	if err != nil {
		warning = setAsideSettings(o.cfgPath, err)
	}
	c := &App{
		version: o.version, site: o.site, running: o.running, layout: o.layout,
		cfgPath: o.cfgPath, dataDir: o.dataDir, candidates: o.candidates, quit: o.quit,
		backups: &backup.Store{Dir: filepath.Join(o.dataDir, "backups"), Keep: KeepCopies, Now: time.Now},
		log:     &sendlog.Log{Path: filepath.Join(o.dataDir, "sent.jsonl"), Keep: KeepLogFor},
		refused: refused.Load(filepath.Join(o.dataDir, RefusedFile)),
		status:  &lastLine{},
		prices:  &addondata.Report{Now: time.Now},
		cfg:     cfg,
		warning: warning,
		changed: make(chan struct{}),
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

// setAsideSettings keeps an unreadable settings file aside, so the next save cannot overwrite the
// token, folders and switched-off saves it may still hold, and says so for the page.
func setAsideSettings(path string, readErr error) string {
	aside, err := config.SetAside(path)
	if err != nil {
		return fmt.Sprintf("Your settings file %s could not be read (%v), nor moved aside (%v): the companion starts unlinked.", path, readErr, err)
	}
	return fmt.Sprintf("Your settings file could not be read (%v). It is kept as %s; the companion starts unlinked, so link this computer again.", readErr, aside)
}

// changes is closed at the next change the upload loop must see.
func (c *App) changes() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changed
}

// update changes the settings, saves them, and wakes the loop.
func (c *App) update(change func(*config.Config) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.cfg
	next.Off = slices.Clone(c.cfg.Off)
	next.Folders = slices.Clone(c.cfg.Folders)
	if err := change(&next); err != nil {
		return err
	}
	if err := config.Save(c.cfgPath, next); err != nil {
		return err
	}
	c.cfg = next
	close(c.changed)
	c.changed = make(chan struct{})
	return nil
}

func (c *App) settings() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *App) saves() []find.Save {
	return find.Saves(append(slices.Clone(c.settings().Folders), c.candidates...))
}

func (c *App) isOff(path string) bool { return c.settings().IsOff(path) }

func (c *App) url() string {
	if u := c.settings().URL; u != "" {
		return u
	}
	return c.site
}

// State is what the settings page shows.
func (c *App) State() page.State {
	cfg := c.settings()
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
		Market: c.settings().Market, DataDir: c.dataDir, Out: c.status, Report: c.prices,
	}
}

// installed says, one line each, what installing the companion put on this computer.
func (c *App) installed() []string {
	target := c.layout.Target(c.running)
	lines := []string{"The companion runs from " + target + "."}
	if c.layout.Transient(target) {
		lines = append(lines, moveToApplications)
	}
	if c.layout.Shortcut != "" {
		lines = append(lines, "A shortcut to it: "+c.layout.Shortcut+".")
	}
	lines = append(lines,
		"Starting with your computer is recorded in "+c.layout.AutostartAt+".",
		"Its settings, with your token, are in "+c.layout.Config+"; copies of your saves and the upload log in "+c.dataDir+".",
		"Uninstall, at the bottom of this page, removes all of it.")
	return lines
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

func (c *App) Link(token string) error {
	token = strings.TrimSpace(token)
	if !tokenShape.MatchString(token) {
		return page.BadRequest("that is not a token: a token starts with gf_ and is 46 letters and digits, from your tokens page")
	}
	return c.update(func(cfg *config.Config) error { cfg.Token = token; return nil })
}

// linkTimeout outlasts a code's ten minutes, so the server's expiry is what ends a link.
const linkTimeout = 15 * time.Minute

// StartDeviceLink starts linking by code in the background, opening the approval page in the browser;
// the page follows it in State. A press while one waits starts nothing new.
func (c *App) StartDeviceLink() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.linking != nil && (c.linking.State == "starting" || c.linking.State == "waiting") {
		return nil
	}
	c.linking = &page.Linking{State: "starting"}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
		defer cancel()
		show := func(code server.DeviceCode) {
			// The browser first, so a page that sees "waiting" knows the approval page was opened.
			if code.VerificationURIComplete != "" {
				c.openURL(code.VerificationURIComplete)
			}
			c.setLinking(page.Linking{State: "waiting", UserCode: code.UserCode, URL: code.VerificationURIComplete})
		}
		save := func(token string) error {
			return c.update(func(cfg *config.Config) error { cfg.Token = token; return nil })
		}
		err := c.linker(ctx, show, save)
		switch {
		case err == nil:
			c.setLinking(page.Linking{State: "linked"})
		case errors.Is(err, server.ErrLinkDenied):
			c.setLinking(page.Linking{State: "denied"})
		case errors.Is(err, server.ErrLinkExpired), errors.Is(err, context.DeadlineExceeded):
			c.setLinking(page.Linking{State: "expired"})
		default:
			c.setLinking(page.Linking{State: "failed", Error: err.Error()})
		}
	}()
	return nil
}

func (c *App) setLinking(l page.Linking) {
	c.mu.Lock()
	c.linking = &l
	c.mu.Unlock()
}

func (c *App) Unlink() error {
	return c.update(func(cfg *config.Config) error { cfg.Token = ""; return nil })
}

func (c *App) SetSaveOn(path string, on bool) error {
	if !slices.ContainsFunc(c.saves(), func(s find.Save) bool { return s.Path == path }) {
		return page.BadRequest("the companion has not found that save")
	}
	return c.update(func(cfg *config.Config) error {
		cfg.Off = slices.DeleteFunc(cfg.Off, func(p string) bool { return p == path })
		if !on {
			cfg.Off = append(cfg.Off, path)
		}
		return nil
	})
}

func (c *App) AddFolder(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if len(find.Saves([]string{path})) == 0 {
		return page.BadRequest("no GoldForever.lua under %s yet: choose the World of Warcraft folder, the one with _retail_ or _forever_ in it, after playing once with the addon", path)
	}
	return c.update(func(cfg *config.Config) error {
		if !slices.Contains(cfg.Folders, path) {
			cfg.Folders = append(cfg.Folders, path)
		}
		return nil
	})
}

// moveToApplications is why a macOS app run from its disk image, or from Downloads before macOS has
// cleared it, cannot start with the computer.
const moveToApplications = "Drag the app to Applications and open it from there: from where it runs now, it cannot start with your computer."

func (c *App) SetAutostart(on bool) error {
	if on && c.layout.Transient(c.layout.Target(c.running)) {
		return page.BadRequest("%s", moveToApplications)
	}
	if on {
		return c.layout.Autostart.Enable(c.layout.Target(c.running))
	}
	return c.layout.Autostart.Disable()
}

func (c *App) Welcomed() error {
	return c.update(func(cfg *config.Config) error { cfg.Welcomed = true; return nil })
}

// kept is one copy of a save on disk, as the page names it.
type kept struct {
	page.Copy
	path, source string
}

// copies lists every kept copy of every save found, by an ID made from its path: the page never sends
// a path, so it can only ever reach a copy listed here.
func (c *App) copies() map[string]kept {
	out := map[string]kept{}
	for _, save := range c.saves() {
		list, _ := c.backups.Copies(save.Path)
		for _, cp := range list {
			info, err := os.Stat(cp.Path)
			if err != nil {
				continue
			}
			sum := sha256.Sum256([]byte(cp.Path))
			id := hex.EncodeToString(sum[:8])
			out[id] = kept{page.Copy{ID: id, Save: save.Label(), Taken: info.ModTime(), Pending: cp.Pending}, cp.Path, save.Path}
		}
	}
	return out
}

func (c *App) History() page.History {
	entries, _ := c.log.Read()
	h := page.History{Entries: entries}
	for _, k := range c.copies() {
		h.Copies = append(h.Copies, k.Copy)
	}
	slices.SortFunc(h.Copies, func(a, b page.Copy) int { return a.Taken.Compare(b.Taken) })
	return h
}

func (c *App) CopyBytes(id string) ([]byte, bool) {
	k, ok := c.copies()[id]
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(k.path)
	return data, err == nil
}

// Restore writes a kept copy back over its save. The page has the player confirm that WoW is closed.
func (c *App) Restore(id string) error {
	k, ok := c.copies()[id]
	if !ok {
		return page.BadRequest("that copy is no longer kept")
	}
	data, err := os.ReadFile(k.path)
	if err != nil {
		return err
	}
	tmp := k.source + ".restoring"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, k.source)
}

// Uninstall removes the companion and everything it put on the computer, then stops it.
func (c *App) Uninstall(keepData bool) error {
	err := c.layout.Uninstall(c.running, keepData)
	_ = os.Remove(filepath.Join(c.dataDir, InstanceFile))
	go func() {
		time.Sleep(300 * time.Millisecond) // let the page get its answer first
		c.quit()
	}()
	return err
}

func (c *App) Quit() { c.quit() }

// loop runs the uploads until ctx ends: with the current token and folders, starting again whenever
// they change, and waiting for a change while there is no token or the server refused it.
func (c *App) loop(ctx context.Context) {
	for ctx.Err() == nil {
		changed := c.changes()
		token := c.settings().Token
		if token == "" {
			c.status.Set("Not linked yet: open Settings and press \"Link this computer\".")
		} else {
			run, stop := context.WithCancel(ctx)
			var started sync.WaitGroup
			started.Go(func() {
				select {
				case <-changed:
					stop()
				case <-run.Done():
				}
			})
			client := &server.Client{BaseURL: c.url(), Token: token, Version: c.version, OS: goos}
			err := NewUploads(c.parts(client)).Run(run)
			stop()
			started.Wait() // the next round, or the caller, owns the data folder alone
			if err == nil {
				continue // a change, or the companion is stopping
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
		}
	}
}

// checkVersion asks the server once a day whether a newer release exists, for the page's notice.
func (c *App) checkVersion(ctx context.Context) {
	client := &server.Client{BaseURL: c.url(), Version: c.version, OS: goos}
	for {
		if latest, err := client.Latest(ctx); err == nil && release.Newer(latest.Version, c.version) {
			c.mu.Lock()
			c.notice = &page.Notice{Version: latest.Version, URL: latest.URL}
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkVersionEvery):
		}
	}
}

// lastLine keeps the last line the upload loop wrote, for the tray and the page, and passes every
// line on to standard output.
type lastLine struct {
	mu   sync.Mutex
	line string
	out  io.Writer
}

func (l *lastLine) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lines := bytes.Split(bytes.TrimRight(p, "\n"), []byte("\n")); len(lines) > 0 {
		l.line = string(lines[len(lines)-1])
	}
	if l.out != nil {
		l.out.Write(p)
	}
	return len(p), nil
}

func (l *lastLine) Set(line string) { fmt.Fprintln(l, line) }

func (l *lastLine) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}
