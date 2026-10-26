package addondata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/server"
)

// Fetcher gets the server's answer for game (one market's, or the account's when market is empty) and
// its ETag, or server.ErrNotModified when etag is still current; server.Client is the real one.
type Fetcher interface {
	Prices(ctx context.Context, game, market, etag string) ([]byte, string, error)
}

// The games the server has prices for, as find.Game names them; each is asked for once per refresh.
var games = []string{"forever", "retail"}

// Refresher keeps the prices of the account's markets in every Forever and retail flavour the saves
// are in.
type Refresher struct {
	Fetcher Fetcher
	Saves   func() []find.Save
	Market  string // one market instead of the account's in its game (forever-…, retail-…), to try it out
	State   string // the file holding what each game's Data.lua holds now, and its ETag
	Out     io.Writer
	Report  *Report // how each fetch and write went, for the page; may be nil
	// Dings fetches the account's dings state, written beside the prices in every game; nil writes none.
	Dings DingsFetcher
	// Shared fetches the account's lists and profiles, written beside the prices in every game; nil
	// writes none.
	Shared SharedFetcher

	last map[string]written // by game: the answer its files were last written from in this run
}

// held is what a game's Data.lua files hold: the answer asked for (a market, or "" for the account's
// markets) under its ETag, and the dings and shared entries written with it. Under dingsKey and
// sharedKey it is the account's dings or shared state as the server sent it, under its ETag.
type held struct {
	Ask    string          `json:"ask"`
	ETag   string          `json:"etag"`
	Dings  string          `json:"dings,omitempty"`
	Shared string          `json:"shared,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
}

// The state file's entry for the dings state, beside the games'.
const dingsKey = "dings"

// extras is what every game's Data.lua carries beside its prices, the same for the account.
type extras struct {
	dings  *Dings
	shared *Shared
}

// written is an answer a game's files were written from, under the ETag it came with.
type written struct {
	ask, etag string
	answer    Answer
}

// Run refreshes at once, then every interval and after each imported upload, until ctx ends. A server
// that fails is asked again sooner, waiting as uploads do; after is the clock (time.After).
func (r *Refresher) Run(ctx context.Context, every time.Duration, after func(time.Duration) <-chan time.Time, imported <-chan struct{}) {
	var backoff time.Duration
	for ctx.Err() == nil { // select picks among ready cases at random: a poke can win over the stop
		wait := every
		var limited *server.RateLimitedError
		switch err := r.Refresh(ctx); {
		case errors.As(err, &limited):
			wait = limited.RetryAfter
		case server.Retryable(err):
			backoff = server.NextWait(backoff)
			wait = min(backoff, every)
		default:
			backoff = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		case <-imported:
		}
	}
}

// Trigger makes the channel Run listens on and the function that pokes it without waiting: pokes
// while a refresh runs fold into one more.
func Trigger() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	return ch, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Refresh asks once per game the saves are in and writes the answer into each of its flavours. A
// failing game leaves the others written; the error is the failed fetches'.
func (r *Refresher) Refresh(ctx context.Context) error {
	byGame := flavoursByGame(r.Saves())
	state := r.state()
	var x extras
	var dingsErr, sharedErr error
	if r.Dings != nil {
		x.dings, dingsErr = accountState(ctx, r, state, dingsKey, "dings state", r.Dings.DingsState, ParseDings, server.ErrNoDings)
	}
	if r.Shared != nil {
		x.shared, sharedErr = accountState(ctx, r, state, sharedKey, "shared lists and profiles", r.Shared.SharedState, ParseShared, server.ErrNoShared)
	}
	errs := []error{dingsErr, sharedErr}
	for _, game := range games {
		if len(byGame[game]) > 0 {
			errs = append(errs, r.refreshGame(ctx, game, byGame[game], state, x))
		}
	}
	if err := r.saveState(state); err != nil {
		fmt.Fprintf(r.Out, "could not keep the prices' ETag: %v\n", err)
	}
	return errors.Join(errs...)
}

// noted is a state with something the log says once, when it arrives.
type noted interface{ note() string }

// accountState fetches the account's state under key (the dings or shared state) with the ETag of the
// one held. A failed fetch or a state out of shape keeps the one held; a server without it (none, a
// 404) has none. nil is none: the files then go without that entry, as an older companion's.
func accountState[T any](ctx context.Context, r *Refresher, state map[string]held, key, label string,
	fetch func(context.Context, string) ([]byte, string, error), parse func([]byte) (T, error), none error) (*T, error) {
	var last *T
	etag := ""
	if h := state[key]; h.State != nil {
		if v, err := parse(h.State); err == nil {
			last, etag = &v, h.ETag
		}
	}
	body, etag, err := fetch(ctx, etag)
	switch {
	case errors.Is(err, server.ErrNotModified):
		return last, nil
	case errors.Is(err, none):
		delete(state, key)
		return nil, nil
	case err != nil:
		fmt.Fprintf(r.Out, "%s not fetched: %v\n", label, err)
		return last, err
	}
	v, err := parse(body)
	if err != nil {
		fmt.Fprintf(r.Out, "%s refused: %v\n", label, err)
		return last, nil
	}
	if n, ok := any(v).(noted); ok && n.note() != "" {
		fmt.Fprintf(r.Out, "%s: %s\n", label, n.note())
	}
	state[key] = held{ETag: etag, State: body}
	return &v, nil
}

// refreshGame fetches game's answer once and writes it, with the extras, into each flavour. It asks
// only for a change when every flavour has the file it last wrote and that file's dings and shared
// entries are current or the answer is in memory to write it again. A failed fetch or an answer out of shape writes nothing, so
// the last good files stay; the error is the fetch's.
func (r *Refresher) refreshGame(ctx context.Context, game string, flavours map[string]int, state map[string]held, x extras) error {
	ask, label := "", "the account's "+game+" markets"
	if strings.HasPrefix(r.Market, game+"-") {
		ask, label = r.Market, r.Market
	}
	h, dings, shared := state[game], dingsPart(x.dings), sharedPart(x.shared)
	current := h.Dings == dings && h.Shared == shared
	last, inMemory := r.last[game]
	inMemory = inMemory && last.ask == h.Ask && last.etag == h.ETag
	etag := ""
	if h.Ask == ask && allHaveData(flavours) && (current || inMemory) {
		etag = h.ETag
	}
	body, etag, err := r.Fetcher.Prices(ctx, game, ask, etag)
	var a Answer
	switch {
	case errors.Is(err, server.ErrNotModified):
		r.Report.fetched(game, OutcomeNotModified, nil)
		if current {
			return nil
		}
		a = last.answer
	case err != nil:
		fmt.Fprintf(r.Out, "prices for %s not fetched: %v\n", label, err)
		r.Report.fetched(game, OutcomeError, err)
		return err
	default:
		if a, err = Parse(body, game, ask); err != nil {
			fmt.Fprintf(r.Out, "prices for %s refused: %v\n", label, err)
			r.Report.fetched(game, OutcomeError, fmt.Errorf("refused: %w", err))
			return nil
		}
	}
	a.Dings, a.Shared = x.dings, x.shared
	lua, err := r.render(game, a)
	if err != nil {
		fmt.Fprintf(r.Out, "prices for %s refused: %v\n", label, err)
		r.Report.fetched(game, OutcomeError, fmt.Errorf("refused: %w", err))
		return err
	}
	if body != nil {
		r.Report.fetched(game, OutcomeOK, nil)
	}
	state[game] = held{Ask: ask, ETag: etag, Dings: dings, Shared: shared}
	r.remember(game, written{ask: ask, etag: etag, answer: a})
	for dir, iface := range flavours {
		err := Write(dir, iface, lua)
		r.Report.wrote(dir, err)
		if err != nil {
			fmt.Fprintf(r.Out, "prices not written into %s: %v\n", dir, err)
			delete(state, game) // a flavour without this answer must be fetched in full next time
			continue
		}
		fmt.Fprintf(r.Out, "prices for %s written into %s (%d markets, %d items)\n", label, dir, len(a.Markets), items(a))
	}
	return nil
}

func (r *Refresher) remember(game string, w written) {
	if r.last == nil {
		r.last = map[string]written{}
	}
	r.last[game] = w
}

func items(a Answer) int {
	n := 0
	for _, p := range a.Markets {
		n += len(p.Items)
	}
	return n
}

// render writes a's Data.lua for game, refusing one over the game's file limit and warning of a market
// over its budget.
func (r *Refresher) render(game string, a Answer) ([]byte, error) {
	lim := limits[game]
	for _, p := range a.Markets {
		if part := len(marketPart(p)); part > lim.market {
			fmt.Fprintf(r.Out, "warning: the prices for %s take %d bytes, over the %d-byte budget of a market\n", p.Market, part, lim.market)
		}
	}
	lua := Lua(a)
	if len(lua) > lim.data {
		return nil, fmt.Errorf("the Data.lua would be %d bytes, over the %d-byte limit; the last good one stays", len(lua), lim.data)
	}
	return lua, nil
}

// state reads what the files hold per game; anything unreadable is nothing held, which costs one full
// fetch.
func (r *Refresher) state() map[string]held {
	state := map[string]held{}
	if data, err := os.ReadFile(r.State); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

func (r *Refresher) saveState(state map[string]held) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.State), 0o700); err != nil {
		return err
	}
	return replace(r.State, append(data, '\n'))
}

// allHaveData says whether every flavour already has a Data.lua, which a 304 would leave as it is.
func allHaveData(flavours map[string]int) bool {
	for dir := range flavours {
		if _, err := os.Stat(filepath.Join(addonDir(dir), dataFile)); err != nil {
			return false
		}
	}
	return true
}

// flavoursByGame maps each game to its flavour folders and their interface numbers, read from the
// saves.
func flavoursByGame(saves []find.Save) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, s := range saves {
		data, err := os.ReadFile(s.Path)
		if err != nil {
			continue
		}
		game := find.Game(data)
		if out[game] == nil {
			out[game] = map[string]int{}
		}
		dir := filepath.Join(s.Install, s.Flavour)
		out[game][dir] = max(out[game][dir], find.Interface(data))
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

func addonDir(flavourDir string) string {
	return filepath.Join(flavourDir, "Interface", "AddOns", addonName)
}

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
