package main

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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/backup"
	"github.com/gold-forever/companion/internal/config"
	"github.com/gold-forever/companion/internal/core"
	"github.com/gold-forever/companion/internal/find"
	"github.com/gold-forever/companion/internal/install"
	"github.com/gold-forever/companion/internal/link"
	"github.com/gold-forever/companion/internal/page"
	"github.com/gold-forever/companion/internal/prices"
	"github.com/gold-forever/companion/internal/refused"
	"github.com/gold-forever/companion/internal/sendlog"
	release "github.com/gold-forever/companion/internal/version"
	"github.com/gold-forever/companion/internal/watch"
)

// A token as /me/tokens makes one: gf_ and 43 letters and digits.
var tokenShape = regexp.MustCompile(`^gf_[0-9A-Za-z]{43}$`)

const checkVersionEvery = 24 * time.Hour

// refusedFile, in the data folder, lists the saves the server refused as too large.
const refusedFile = "refused.json"

// companion is the running app: what the settings page and the tray show and change, and the upload
// loop, which starts again whenever the token or the folders change.
type companion struct {
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
	prices     *prices.Report // how each price fetch and write went, kept across the loop's restarts
	// linker links by code: it shows the code, waits for the approval and saves the token with save.
	linker func(ctx context.Context, show func(api.DeviceCode), save func(token string) error) error
	// openURL opens a page in the player's browser.
	openURL func(url string)

	mu      sync.Mutex
	cfg     config.Config
	notice  *page.Notice
	linking *page.Linking
	changed chan struct{} // closed and replaced on every change the loop must see
	games   map[string]gameSeen
}

type gameSeen struct {
	modified time.Time
	size     int64
	game     string
}

type companionOptions struct {
	version, site, running, cfgPath, dataDir string
	layout                                   install.Layout
	candidates                               []string
	quit                                     func()
}

func newCompanion(o companionOptions) *companion {
	cfg, _ := config.Load(o.cfgPath) // an unreadable file is an unlinked companion, which the page shows
	c := &companion{
		version: o.version, site: o.site, running: o.running, layout: o.layout,
		cfgPath: o.cfgPath, dataDir: o.dataDir, candidates: o.candidates, quit: o.quit,
		backups: &backup.Store{Dir: filepath.Join(o.dataDir, "backups"), Keep: keepCopies, Now: time.Now},
		log:     &sendlog.Log{Path: filepath.Join(o.dataDir, "sent.jsonl"), Keep: keepLogFor},
		refused: refused.Load(filepath.Join(o.dataDir, refusedFile)),
		status:  &lastLine{},
		prices:  &prices.Report{Now: time.Now},
		cfg:     cfg,
		changed: make(chan struct{}),
		games:   map[string]gameSeen{},
		openURL: func(string) {},
	}
	c.linker = func(ctx context.Context, show func(api.DeviceCode), save func(string) error) error {
		hostname, _ := os.Hostname()
		client := &api.Client{BaseURL: c.url(), Version: c.version, OS: goos, Sleep: pollSleep}
		return link.Run(ctx, client, hostname, show, save)
	}
	return c
}

// changes is closed at the next change the upload loop must see.
func (c *companion) changes() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changed
}

// update changes the settings, saves them, and wakes the loop.
func (c *companion) update(change func(*config.Config) error) error {
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

func (c *companion) settings() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *companion) saves() []find.Save {
	return find.Saves(append(slices.Clone(c.settings().Folders), c.candidates...))
}

func (c *companion) isOff(path string) bool { return c.settings().IsOff(path) }

func (c *companion) url() string {
	if u := c.settings().URL; u != "" {
		return u
	}
	return c.site
}

// State is what the settings page shows.
func (c *companion) State() page.State {
	cfg := c.settings()
	s := page.State{
		Version:   c.version,
		Site:      c.url(),
		Linked:    cfg.Token != "",
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
func priceFile(f prices.File) page.PriceFile {
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
func dingsLine(d prices.Dings) string {
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
func (c *companion) refresher(f prices.Fetcher, d prices.DingsFetcher, s prices.SharedFetcher) *prices.Refresher {
	return &prices.Refresher{
		Fetcher: f, Dings: d, Shared: s, Saves: c.saves, Market: c.settings().Market,
		State: filepath.Join(c.dataDir, pricesState), Out: c.status, Report: c.prices,
	}
}

// installed says, one line each, what installing the companion put on this computer.
func (c *companion) installed() []string {
	target := c.layout.Target(c.running)
	lines := []string{"The companion runs from " + target + "."}
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
func (c *companion) gameOf(path string) string {
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

func (c *companion) Link(token string) error {
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
func (c *companion) StartDeviceLink() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.linking != nil && (c.linking.State == "starting" || c.linking.State == "waiting") {
		return nil
	}
	c.linking = &page.Linking{State: "starting"}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
		defer cancel()
		show := func(code api.DeviceCode) {
			// The browser first, so a page that sees "waiting" knows the approval page was opened.
			c.openURL(code.VerificationURIComplete)
			c.setLinking(page.Linking{State: "waiting", UserCode: code.UserCode, URL: code.VerificationURIComplete})
		}
		save := func(token string) error {
			return c.update(func(cfg *config.Config) error { cfg.Token = token; return nil })
		}
		err := c.linker(ctx, show, save)
		switch {
		case err == nil:
			c.setLinking(page.Linking{State: "linked"})
		case errors.Is(err, api.ErrLinkDenied):
			c.setLinking(page.Linking{State: "denied"})
		case errors.Is(err, api.ErrLinkExpired), errors.Is(err, context.DeadlineExceeded):
			c.setLinking(page.Linking{State: "expired"})
		default:
			c.setLinking(page.Linking{State: "failed", Error: err.Error()})
		}
	}()
	return nil
}

func (c *companion) setLinking(l page.Linking) {
	c.mu.Lock()
	c.linking = &l
	c.mu.Unlock()
}

func (c *companion) Unlink() error {
	return c.update(func(cfg *config.Config) error { cfg.Token = ""; return nil })
}

func (c *companion) SetSaveOn(path string, on bool) error {
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

func (c *companion) AddFolder(path string) error {
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

func (c *companion) SetAutostart(on bool) error {
	if on {
		return c.layout.Autostart.Enable(c.layout.Target(c.running))
	}
	return c.layout.Autostart.Disable()
}

func (c *companion) Welcomed() error {
	return c.update(func(cfg *config.Config) error { cfg.Welcomed = true; return nil })
}

// kept is one copy of a save on disk, as the page names it.
type kept struct {
	page.Copy
	path, source string
}

// copies lists every kept copy of every save found, by an ID made from its path: the page never sends
// a path, so it can only ever reach a copy listed here.
func (c *companion) copies() map[string]kept {
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

func (c *companion) History() page.History {
	entries, _ := c.log.Read()
	h := page.History{Entries: entries}
	for _, k := range c.copies() {
		h.Copies = append(h.Copies, k.Copy)
	}
	slices.SortFunc(h.Copies, func(a, b page.Copy) int { return a.Taken.Compare(b.Taken) })
	return h
}

func (c *companion) CopyBytes(id string) ([]byte, bool) {
	k, ok := c.copies()[id]
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(k.path)
	return data, err == nil
}

// Restore writes a kept copy back over its save. The page has the player confirm that WoW is closed.
func (c *companion) Restore(id string) error {
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
func (c *companion) Uninstall(keepData bool) error {
	err := c.layout.Uninstall(c.running, keepData)
	_ = os.Remove(filepath.Join(c.dataDir, instanceFile))
	go func() {
		time.Sleep(300 * time.Millisecond) // let the page get its answer first
		c.quit()
	}()
	return err
}

func (c *companion) Quit() { c.quit() }

// loop runs the uploads until ctx ends: with the current token and folders, starting again whenever
// they change, and waiting for a change while there is no token or the server refused it.
func (c *companion) loop(ctx context.Context) {
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
			client := &api.Client{BaseURL: c.url(), Token: token, Version: c.version, OS: goos}
			imported, announce := prices.Trigger()
			refresher := c.refresher(client, client, client)
			started.Go(func() { refresher.Run(run, refreshPricesEvery, time.After, imported) })
			loop := &core.Core{
				Uploader: client,
				Backups:  c.backups, Log: c.log, Refused: c.refused, Watcher: watch.New(pollEvery),
				Saves: c.saves, IsOff: c.isOff, Out: c.status, Imported: announce,
			}
			err := loop.Run(run, pollEvery)
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
func (c *companion) checkVersion(ctx context.Context) {
	client := &api.Client{BaseURL: c.url(), Version: c.version, OS: goos}
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
