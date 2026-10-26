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

func (c *App) saves() []find.Save {
	return find.Saves(append(slices.Clone(c.settings.Get().Folders), c.candidates...))
}

func (c *App) isOff(path string) bool { return c.settings.Get().IsOff(path) }

func (c *App) url() string {
	if u := c.settings.Get().URL; u != "" {
		return u
	}
	return c.site
}
