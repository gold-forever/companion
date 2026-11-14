package addondata

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/saves/find"
)

// allHaveData says whether every flavour already has a Data.lua, which a 304 would leave as it is.
func allHaveData(flavours map[string]flavour) bool {
	for dir := range flavours {
		if _, err := os.Stat(dataPath(dir)); err != nil {
			return false
		}
	}
	return true
}

// flavour is what a flavour folder's saves say of its client: the highest interface number, and the
// locale of the newest save naming one ("" for none).
type flavour struct {
	iface    int
	locale   string
	localeAt time.Time
}

// flavoursByGame maps each game to its flavour folders and what their saves say of the client.
func flavoursByGame(saves []find.Save) map[string]map[string]flavour {
	out := map[string]map[string]flavour{}
	for _, s := range saves {
		data, err := os.ReadFile(s.Path)
		if err != nil {
			continue
		}
		game := find.Game(data)
		if out[game] == nil {
			out[game] = map[string]flavour{}
		}
		dir := filepath.Join(s.Install, s.Flavour)
		f := out[game][dir]
		f.iface = max(f.iface, find.Interface(data))
		if locale := localeOf(data); locale != "" {
			if info, err := os.Stat(s.Path); err == nil && (f.locale == "" || info.ModTime().After(f.localeAt)) {
				f.locale, f.localeAt = locale, info.ModTime()
			}
		}
		out[game][dir] = f
	}
	return out
}

// Write puts the data addon into a flavour folder, each file through a temporary file renamed into
// place, so the game never loads half a file.
func Write(flavourDir string, iface int, lua []byte) error {
	dir := addonDir(flavourDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := replace(filepath.Join(dir, tocFile), TOC(iface)); err != nil {
		return err
	}
	return replace(filepath.Join(dir, dataFile), lua)
}

// AddOns is a flavour folder's addons folder.
func AddOns(flavourDir string) string { return filepath.Join(flavourDir, "Interface", "AddOns") }

func addonDir(flavourDir string) string { return filepath.Join(AddOns(flavourDir), AddonName) }

func dataPath(flavourDir string) string { return filepath.Join(addonDir(flavourDir), dataFile) }

func replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; cleans up after a failure
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
