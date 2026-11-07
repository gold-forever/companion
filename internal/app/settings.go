package app

import (
	"fmt"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/server"
)

// refusedSite says, for the page, why the companion will not upload to the site the settings name;
// the file is left as it is for the player to fix. Empty when the site is fine or not set.
func refusedSite(path, site string) string {
	if site == "" {
		return ""
	}
	if err := server.CheckSite(site); err != nil {
		return fmt.Sprintf("Your settings file %s names a site the companion will not upload to: %v. %s", path, err, config.RefusedSiteAdvice)
	}
	return ""
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

func (c *App) url() string {
	if u := c.settings.Get().URL; u != "" {
		return u
	}
	return c.site
}
