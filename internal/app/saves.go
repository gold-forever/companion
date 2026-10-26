package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/saves/find"
)

func (c *App) SetSaveOn(ctx context.Context, path string, on bool) error {
	if !slices.ContainsFunc(c.saves(), func(s find.Save) bool { return s.Path == path }) {
		return ErrUnknownSave
	}
	return c.settings.Update(func(cfg *config.Config) error {
		cfg.Off = slices.DeleteFunc(cfg.Off, func(p string) bool { return p == path })
		if !on {
			cfg.Off = append(cfg.Off, path)
		}
		return nil
	})
}

func (c *App) AddFolder(ctx context.Context, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if len(find.Saves([]string{path})) == 0 {
		return &NoSaveError{Folder: path}
	}
	return c.settings.Update(func(cfg *config.Config) error {
		if !slices.Contains(cfg.Folders, path) {
			cfg.Folders = append(cfg.Folders, path)
		}
		return nil
	})
}
