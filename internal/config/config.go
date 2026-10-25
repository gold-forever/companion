// Package config is the companion's settings file. It holds the upload token, so only its owner may
// read it (mode 0600; on Windows the user profile's own permissions).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/gold-forever/companion/internal/atomicfile"
)

// Config is everything the player sets.
type Config struct {
	Token   string   `json:"token,omitempty"`
	URL     string   `json:"url,omitempty"`     // empty means the public site
	Off     []string `json:"off,omitempty"`     // saves the player switched off, by path
	Folders []string `json:"folders,omitempty"` // WoW folders added by hand
	// Market is one market whose prices are written into its game instead of the account's markets
	// there, e.g. forever-EU-normal-horde, to try a market out; empty writes the account's.
	Market string `json:"market,omitempty"`
	// Welcomed is set once the player has seen the first-run panel; until then autostart may be turned
	// on for them, after it only by their own choice.
	Welcomed bool `json:"welcomed,omitempty"`
}

// IsOff says whether the player switched the save at path off.
func (c Config) IsOff(path string) bool { return slices.Contains(c.Off, path) }

// FileName is the settings file's name in its folder.
const FileName = "config.json"

// Path is the settings file in the operating system's per-user config folder.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gold-forever-companion", FileName), nil
}

// Load reads the settings; a missing file is an empty Config.
func Load(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

// SetAside moves an unreadable settings file out of the way, so the next save cannot overwrite what
// it held, and returns where it is now.
func SetAside(path string) (string, error) {
	aside := path + ".bad"
	return aside, os.Rename(path, aside)
}

// Save writes the settings, readable by their owner only, replacing the file whole.
func Save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'))
}

// DataDir is where the companion keeps its backups and log on goos: the system's place for an
// application's own files, not a cache that may be cleared.
func DataDir(goos, home string, getenv func(string) string) string {
	const name = "gold-forever-companion"
	switch goos {
	case "windows":
		return filepath.Join(getenv("LOCALAPPDATA"), name)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", name)
	default:
		if xdg := getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, name)
		}
		return filepath.Join(home, ".local", "share", name)
	}
}
