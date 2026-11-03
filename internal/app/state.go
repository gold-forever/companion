package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/ui/page"
)

type gameSeen struct {
	modified time.Time
	size     int64
	game     string
}

func (c *App) State() page.State {
	cfg := c.settings.Get()
	s := page.State{
		Version:   c.version,
		Site:      c.url(),
		Linked:    cfg.Token != "",
		Sharing:   link.SharingLine,
		Warning:   c.pageWarning(),
		Status:    c.status.String(),
		Account:   c.Account(),
		FirstRun:  !cfg.Welcomed,
		Installed: c.installed(),
		Autostart: c.layout.Autostart.Enabled(),
		Folders:   cfg.Folders,
		Addon:     c.addonState(),
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
	s.Update = c.update
	if c.linking != nil {
		l := *c.linking
		s.Linking = &l
	}
	c.mu.Unlock()
	return s
}

// pageWarning is what the page must tell the player: an older copy running beside this one, then a
// problem with their settings.
func (c *App) pageWarning() string {
	var lines []string
	for _, line := range []string{c.olderLine(), c.warning} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, " ")
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
	if f.Shared != nil {
		p.Shared = fmt.Sprintf("Lists: %d · Profiles: %d", f.Shared.Lists, f.Shared.Profiles)
	}
	return p
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
