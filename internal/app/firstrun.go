package app

import "github.com/gold-forever/companion/internal/account/config"

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

func (c *App) Welcomed() error {
	return c.update(func(cfg *config.Config) error { cfg.Welcomed = true; return nil })
}
