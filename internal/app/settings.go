package app

import (
	"fmt"
	"slices"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/saves/find"
)

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
