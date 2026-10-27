package app

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// The tray's use cases: its status line, the way to the site and the settings, and Quit.

// maxTrayStatus is the most a status line in the tray's menu shows, in characters.
const maxTrayStatus = 60

// The price lines addondata writes, which name a game folder: the page shows that, the tray does not.
var (
	pricesWritten    = regexp.MustCompile(`^prices for .+ written into .+ \((\d+) markets, (\d+) items\)$`)
	pricesNotWritten = regexp.MustCompile(`^prices not written into `)
)

// Status is the latest line the upload loop wrote, short enough for the tray's menu; while an older copy
// runs beside this one, that comes first.
func (c *App) Status() string {
	if c.olderLine() != "" {
		return "An older copy is still running: see Settings"
	}
	return trayStatus(c.status.String())
}

func trayStatus(line string) string {
	if m := pricesWritten.FindStringSubmatch(line); m != nil {
		markets, _ := strconv.Atoi(m[1])
		items, _ := strconv.Atoi(m[2])
		return fmt.Sprintf("Prices written: %s, %s items", plural(markets, "market"), thousands(items))
	}
	if pricesNotWritten.MatchString(line) {
		return "Prices not written: see Settings"
	}
	if utf8.RuneCountInString(line) > maxTrayStatus {
		return string([]rune(line)[:maxTrayStatus-1]) + "…"
	}
	return line
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// thousands writes n with a comma between each group of three digits: 36,317.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// OpenSite opens the site in the player's browser.
func (c *App) OpenSite() { c.openURL(c.url()) }

// OpenSettings opens the settings page in the player's browser.
func (c *App) OpenSettings() { c.openURL(c.pageURL) }

// Quit stops the companion.
func (c *App) Quit() { c.quit() }

// UpdateVersion is the newer release the last check found, for the tray's Update now; "" while none.
func (c *App) UpdateVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.update.Newer {
		return ""
	}
	return c.update.Latest
}

// UpdateNow runs Settings' Update. Where the companion cannot install the release itself it opens the
// release's page, and when the update fails, the settings page, which says why.
func (c *App) UpdateNow() {
	c.mu.Lock()
	u := c.update
	c.mu.Unlock()
	if !u.Newer {
		return
	}
	if !u.CanInstall {
		c.openURL(u.URL)
		return
	}
	c.owned.Go(func() {
		if err := c.Update(c.life); err != nil && !errors.Is(err, ErrUpdating) {
			c.openURL(c.pageURL)
		}
	})
}
