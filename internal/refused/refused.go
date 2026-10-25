// Package refused keeps the saves the server refused as too large, each by its path and the SHA-256
// of what was sent, so the companion can send them again: the server's limit may have been raised.
package refused

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Every is how often a refused save is tried again.
const Every = 24 * time.Hour

// Save is one save the server refused as too large.
type Save struct {
	Source string `json:"source"` // the save's path
	SHA256 string `json:"sha256"` // of the copy that was refused
}

type held struct {
	Save
	next time.Time
}

// List is the refused saves, kept in the file at its path. Only who was refused is kept, not when:
// every save on the list is tried once when the companion starts.
type List struct {
	path  string
	mu    sync.Mutex
	saves map[string]held
}

// Load reads the list kept at path; a missing or unreadable file is an empty list.
func Load(path string) *List {
	l := &List{path: path, saves: map[string]held{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var saves []Save
	if json.Unmarshal(data, &saves) != nil {
		return l
	}
	for _, s := range saves {
		l.saves[s.Source] = held{Save: s}
	}
	return l
}

// Refuse records that the server refused the save at source, holding data with this SHA-256; it is
// due again a day after now.
func (l *List) Refuse(source, sha string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.saves[source] = held{Save{source, sha}, now.Add(Every)}
	return l.write()
}

// Drop forgets the save at source.
func (l *List) Drop(source string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.saves[source]; !ok {
		return nil
	}
	delete(l.saves, source)
	return l.write()
}

// Due lists the refused saves to try again at now.
func (l *List) Due(now time.Time) []Save {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Save
	for _, h := range l.saves {
		if !now.Before(h.next) {
			out = append(out, h.Save)
		}
	}
	slices.SortFunc(out, func(a, b Save) int { return strings.Compare(a.Source, b.Source) })
	return out
}

// Holds says whether the save at source was refused as too large.
func (l *List) Holds(source string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.saves[source]
	return ok
}

func (l *List) write() error {
	saves := []Save{}
	for _, h := range l.saves {
		saves = append(saves, h.Save)
	}
	slices.SortFunc(saves, func(a, b Save) int { return strings.Compare(a.Source, b.Source) })
	data, err := json.Marshal(saves)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(l.path, data, 0o600)
}
