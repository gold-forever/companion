package prices

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/find"
)

// What the latest fetch of a game came to.
const (
	OutcomeOK          = "ok"
	OutcomeNotModified = "not modified"
	OutcomeError       = "error"
)

// File is one flavour's price file as the settings page shows it: what Data.lua holds now and how the
// latest fetch of its game went.
type File struct {
	Flavour string // the flavour folder
	Game    string
	Markets []Market
	Items   int
	Size    int64     // bytes of Data.lua; 0 when there is none
	Dings   *Dings    // the dings state Data.lua holds; nil when it holds none
	Fetched time.Time // zero before the first fetch
	Outcome string    // OutcomeOK, OutcomeNotModified, OutcomeError, or empty before the first fetch
	Error   string
}

// Market is one market in a Data.lua and how many items it holds.
type Market struct {
	Key   string
	Items int
}

// Report keeps how the latest fetch of each game and the latest write into each flavour went, for the
// page. A nil Report keeps nothing.
type Report struct {
	Now func() time.Time

	mu      sync.Mutex
	fetches map[string]fetch  // by game
	failed  map[string]string // a flavour folder's latest write error
}

type fetch struct {
	at      time.Time
	outcome string
	err     string
}

func (r *Report) fetched(game, outcome string, err error) {
	if r == nil {
		return
	}
	f := fetch{at: r.Now(), outcome: outcome}
	if err != nil {
		f.err = err.Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetches == nil {
		r.fetches = map[string]fetch{}
	}
	r.fetches[game] = f
}

// wrote records a flavour's write; err nil clears its failure.
func (r *Report) wrote(dir string, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed == nil {
		r.failed = map[string]string{}
	}
	delete(r.failed, dir)
	if err != nil {
		r.failed[dir] = err.Error()
	}
}

// Files lists every Forever and retail flavour the saves are in, sorted by folder, with its Data.lua
// read from disk, so it is true after a restart and a 304 as well.
func (r *Report) Files(saves []find.Save) []File {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []File
	for game, flavours := range flavoursByGame(saves) {
		if !slices.Contains(games, game) {
			continue
		}
		for dir := range flavours {
			f := File{Flavour: dir, Game: game}
			if lua, err := os.ReadFile(filepath.Join(addonDir(dir), "Data.lua")); err == nil {
				f.Size = int64(len(lua))
				f.Markets, f.Items, f.Dings = contents(lua)
			}
			if last, ok := r.fetches[game]; ok {
				f.Fetched, f.Outcome, f.Error = last.at, last.outcome, last.err
			}
			if err, ok := r.failed[dir]; ok && f.Outcome == OutcomeOK {
				f.Outcome, f.Error = OutcomeError, err
			}
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b File) int { return cmp.Compare(a.Flavour, b.Flavour) })
	return out
}

// contents reads a Data.lua as Lua writes it: in the markets table a market opens with
// `    ["key"] = {` and an item is `      [id] = { … },`; in the dings table its time is `    at = n,`,
// the webhook `    webhook = true,` and a character `      ["guid"] = true,`.
func contents(lua []byte) ([]Market, int, *Dings) {
	var markets []Market
	var dings *Dings
	total, section := 0, ""
	for line := range bytes.Lines(lua) {
		switch {
		case bytes.HasPrefix(line, []byte("  ")) && !bytes.HasPrefix(line, []byte("   ")):
			section = string(bytes.TrimSpace(line))
			if section == "dings = {" {
				dings = &Dings{Characters: map[string]bool{}}
			}
		case section == "markets = {" && bytes.HasPrefix(line, []byte(`    ["`)):
			key, _, _ := bytes.Cut(line[len(`    ["`):], []byte(`"]`))
			markets = append(markets, Market{Key: string(key)})
		case section == "markets = {" && bytes.HasPrefix(line, []byte("      [")) && len(markets) > 0:
			markets[len(markets)-1].Items++
			total++
		case section == "dings = {" && bytes.HasPrefix(line, []byte("    at = ")):
			dings.At, _ = strconv.ParseInt(string(bytes.TrimSuffix(bytes.TrimSpace(line[len("    at = "):]), []byte(","))), 10, 64)
		case section == "dings = {" && bytes.HasPrefix(line, []byte("    webhook = ")):
			dings.Webhook = bytes.HasPrefix(line, []byte("    webhook = true,"))
		case section == "dings = {" && bytes.HasPrefix(line, []byte(`      ["`)):
			guid, choice, _ := bytes.Cut(line[len(`      ["`):], []byte(`"] = `))
			dings.Characters[string(guid)] = bytes.HasPrefix(choice, []byte("true"))
		}
	}
	return markets, total, dings
}
