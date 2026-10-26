package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"

	"github.com/gold-forever/companion/internal/ui/page"
)

// kept is one copy of a save on disk, as the page names it.
type kept struct {
	page.Copy
	path, source string
}

// copies lists every kept copy of every save found, by an ID made from its path: the page never sends
// a path, so it can only ever reach a copy listed here.
func (c *App) copies() map[string]kept {
	out := map[string]kept{}
	for _, save := range c.saves() {
		list, _ := c.backups.Copies(save.Path)
		for _, cp := range list {
			info, err := os.Stat(cp.Path)
			if err != nil {
				continue
			}
			sum := sha256.Sum256([]byte(cp.Path))
			id := hex.EncodeToString(sum[:8])
			out[id] = kept{page.Copy{ID: id, Save: save.Label(), Taken: info.ModTime(), Pending: cp.Pending}, cp.Path, save.Path}
		}
	}
	return out
}

func (c *App) History() page.History {
	entries, _ := c.log.Read()
	h := page.History{Entries: entries}
	for _, k := range c.copies() {
		h.Copies = append(h.Copies, k.Copy)
	}
	slices.SortFunc(h.Copies, func(a, b page.Copy) int { return a.Taken.Compare(b.Taken) })
	return h
}

func (c *App) CopyBytes(id string) ([]byte, bool) {
	k, ok := c.copies()[id]
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(k.path)
	return data, err == nil
}

// Restore writes a kept copy back over its save. The page has the player confirm that WoW is closed.
func (c *App) Restore(id string) error {
	k, ok := c.copies()[id]
	if !ok {
		return page.BadRequest("that copy is no longer kept")
	}
	data, err := os.ReadFile(k.path)
	if err != nil {
		return err
	}
	tmp := k.source + ".restoring"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, k.source)
}
