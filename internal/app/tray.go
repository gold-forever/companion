package app

// The tray's use cases: its status line, the way to the site and the settings, and Quit.

// Status is the latest line the upload loop wrote.
func (c *App) Status() string { return c.status.String() }

// OpenSite opens the site in the player's browser.
func (c *App) OpenSite() { c.openURL(c.url()) }

// OpenSettings opens the settings page in the player's browser.
func (c *App) OpenSettings() { c.openURL(c.pageURL) }

// Quit stops the companion.
func (c *App) Quit() { c.quit() }
