// Package backup keeps the last copies of each save in the companion's own folder. A copy is
// "pending" until the server has answered for it, so the backups are also the upload queue: what
// was not sent before the computer shut down is sent at the next start.
package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pendingSuffix = ".pending.lua"
	sentSuffix    = ".lua"
	sourceFile    = "source"
	stampLayout   = "20060102T150405.000000000Z"
)

// Store keeps up to Keep copies per save under Dir.
type Store struct {
	Dir  string
	Keep int
	Now  func() time.Time
}

// Copy is one kept copy of a save.
type Copy struct {
	Path    string // the copy
	Source  string // the save it was taken from
	Pending bool
}

// Add copies data, taken from the save at source, unless it is the same as the newest copy. It
// reports whether a copy was made.
func (s *Store) Add(source string, data []byte) (Copy, bool, error) {
	dir := s.folder(source)
	copies, err := s.Copies(source)
	if err != nil {
		return Copy{}, false, err
	}
	if n := len(copies); n > 0 {
		if newest, err := os.ReadFile(copies[n-1].Path); err == nil && bytes.Equal(newest, data) {
			return copies[n-1], false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Copy{}, false, err
	}
	if err := os.WriteFile(filepath.Join(dir, sourceFile), []byte(source), 0o600); err != nil {
		return Copy{}, false, err
	}
	sum := sha256.Sum256(data)
	name := s.Now().UTC().Format(stampLayout) + "-" + hex.EncodeToString(sum[:6]) + pendingSuffix
	c := Copy{Path: filepath.Join(dir, name), Source: source, Pending: true}
	if err := os.WriteFile(c.Path, data, 0o600); err != nil {
		return Copy{}, false, err
	}
	return c, true, s.prune(source)
}

// MarkSent records that the server answered for the copy, so it is not sent again.
func (s *Store) MarkSent(c Copy) error {
	if !c.Pending {
		return nil
	}
	return os.Rename(c.Path, strings.TrimSuffix(c.Path, pendingSuffix)+sentSuffix)
}

// Copies lists the save's copies, oldest first.
func (s *Store) Copies(source string) ([]Copy, error) {
	entries, err := os.ReadDir(s.folder(source))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Copy
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, sentSuffix) {
			continue
		}
		out = append(out, Copy{Path: filepath.Join(s.folder(source), name), Source: source, Pending: strings.HasSuffix(name, pendingSuffix)})
	}
	// Names start with the time they were taken, so name order is time order.
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i].Path) < filepath.Base(out[j].Path) })
	return out, nil
}

// Pending lists every copy not yet sent, of every save, oldest first.
func (s *Store) Pending() ([]Copy, error) {
	folders, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Copy
	for _, f := range folders {
		source, err := os.ReadFile(filepath.Join(s.Dir, f.Name(), sourceFile))
		if err != nil {
			continue
		}
		copies, err := s.Copies(string(source))
		if err != nil {
			return nil, err
		}
		for _, c := range copies {
			if c.Pending {
				out = append(out, c)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return filepath.Base(out[i].Path) < filepath.Base(out[j].Path) })
	return out, nil
}

// prune removes the oldest copies beyond Keep, sent or not: a save holds the whole recent history, so
// the newest copies carry what the older ones did.
func (s *Store) prune(source string) error {
	copies, err := s.Copies(source)
	if err != nil {
		return err
	}
	for len(copies) > s.Keep {
		if err := os.Remove(copies[0].Path); err != nil {
			return err
		}
		copies = copies[1:]
	}
	return nil
}

// folder is the save's own folder, named by a hash of its path, so no account name is in a file name.
func (s *Store) folder(source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:8]))
}
