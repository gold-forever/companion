package app

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/ui/page"
)

func (c *App) SetSaveOn(path string, on bool) error {
	if !slices.ContainsFunc(c.saves(), func(s find.Save) bool { return s.Path == path }) {
		return page.BadRequest("the companion has not found that save")
	}
	return c.settings.Update(func(cfg *config.Config) error {
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
	return c.settings.Update(func(cfg *config.Config) error {
		if !slices.Contains(cfg.Folders, path) {
			cfg.Folders = append(cfg.Folders, path)
		}
		return nil
	})
}
