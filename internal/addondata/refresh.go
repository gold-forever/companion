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
	// Fetcher fetches the prices and, when it is a NamesFetcher too, each flavour's names wanted in its
	// client's locale; one that is not writes none.
	Fetcher Fetcher
	Saves   func() []find.Save
	Market  string // one market instead of the account's in its game (forever-…, retail-…), to try it out
	State   string // the file holding what each game's Data.lua holds now, and its ETag
	Out     io.Writer
	Report  *Report // how each fetch and write went, for the page; may be nil
	// Shared fetches the account's lists and profiles, written beside the prices in every game; nil
	// writes none.
	Shared SharedFetcher
	// Characters fetches the account's characters, each game's written beside its prices; nil writes
	// none.
	Characters CharactersFetcher

	last map[string]written // by game: the answer its files were last written from in this run
}

// held is what a game's Data.lua files hold: the answer asked for (a market, or "" for the account's
// markets) under its ETag, and the shared and characters entries written with it. Under sharedKey and
// charactersKey it is the account's shared state or characters as the server sent them, under their
// ETag, and under namesKey a game's names wanted in a locale. Names is a digest of the names wanted the
// game's files were written with. Dings is the dings entry an older companion wrote: a file holding one
// is written again without it.
type held struct {
	Ask        string          `json:"ask"`
	ETag       string          `json:"etag"`
	Dings      string          `json:"dings,omitempty"`
	Shared     string          `json:"shared,omitempty"`
	Characters string          `json:"characters,omitempty"`
	Names      string          `json:"names,omitempty"`
	State      json.RawMessage `json:"state,omitempty"`
}

// extras is what every game's Data.lua carries beside its prices, fetched once for the account.
type extras struct {
	shared     *Shared
	characters *Characters
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
	var sharedErr, charactersErr error
	if r.Shared != nil {
		x.shared, sharedErr = accountState(ctx, r, state, sharedKey, "shared lists and profiles", r.Shared.SharedState, ParseShared, server.ErrNoShared)
	}
	if r.Characters != nil {
		x.characters, charactersErr = accountState(ctx, r, state, charactersKey, "characters", r.Characters.Characters, ParseCharacters, server.ErrNoCharacters)
	}
	errs := []error{sharedErr, charactersErr}
	asked := map[string]bool{}
	for _, game := range games {
		flavours := byGame[game]
		if len(flavours) == 0 {
			continue
		}
		wanted, namesErr := r.namesWanted(ctx, game, flavours, state, asked)
		errs = append(errs, namesErr, r.refreshGame(ctx, game, flavours, state, x, wanted))
	}
	forgetNames(state, asked)
	if err := r.saveState(state); err != nil {
		fmt.Fprintf(r.Out, "could not keep the prices' ETag: %v\n", err)
	}
	return errors.Join(errs...)
}

// noted is a state with something the log says once, when it arrives.
type noted interface{ note() string }

// accountState fetches a state held under key (the account's shared state or characters, or a game's
// names wanted in a locale) with the ETag of the one held. A failed fetch or a state out of shape keeps the one held; a server without it (none, a
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
// only for a change when every flavour has the file it last wrote and that file's shared and characters
// entries are current, without an older companion's dings entry, or the answer is in memory to write it
// again. A failed fetch or an answer out of shape writes nothing, so the last good files stay; the error
// is the fetch's.
func (r *Refresher) refreshGame(ctx context.Context, game string, flavours map[string]flavour, state map[string]held, x extras, wanted map[string]*NamesWanted) error {
	ask, label := r.askFor(game)
	chars := x.characters.in(game)
	h, shared, characters, names := state[game], sharedPart(x.shared), charactersPart(chars), namesDigest(flavours, wanted)
	current := h.Dings == "" && h.Shared == shared && h.Characters == characters && h.Names == names
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
			r.refuse(game, label, err)
			return nil
		}
	}
	a.Shared, a.Characters = x.shared, chars
	luas, err := r.renderEach(game, a, flavours, wanted)
	if err != nil {
		r.refuse(game, label, err)
		return err
	}
	if body != nil {
		r.Report.fetched(game, OutcomeOK, nil)
	}
	state[game] = held{Ask: ask, ETag: etag, Shared: shared, Characters: characters, Names: names}
	r.remember(game, written{ask: ask, etag: etag, answer: a})
	r.writeInto(flavours, game, label, a, luas, wanted, state)
	return nil
}

// renderEach renders a's Data.lua for each flavour, with the names wanted in its client's locale: once
// per locale.
func (r *Refresher) renderEach(game string, a Answer, flavours map[string]flavour, wanted map[string]*NamesWanted) (map[string][]byte, error) {
	byLocale := map[string][]byte{}
	out := map[string][]byte{}
	for dir, f := range flavours {
		lua, ok := byLocale[f.locale]
		if !ok {
			b := a
			b.Names = wanted[f.locale]
			var err error
			if lua, err = r.render(game, b); err != nil {
				return nil, err
			}
			byLocale[f.locale] = lua
		}
		out[dir] = lua
	}
	return out, nil
}

// askFor is the answer to ask for game, "" for the account's markets, and how the log names it.
func (r *Refresher) askFor(game string) (ask, label string) {
	if strings.HasPrefix(r.Market, game+"-") {
		return r.Market, r.Market
	}
	return "", "the account's " + game + " markets"
}

// refuse logs and reports an answer for game that is out of shape or over its limit.
func (r *Refresher) refuse(game, label string, err error) {
	fmt.Fprintf(r.Out, "prices for %s refused: %v\n", label, err)
	r.Report.fetched(game, OutcomeError, fmt.Errorf("refused: %w", err))
}

// writeInto writes each flavour's lua, rendered from a and its locale's names wanted. A flavour it
// cannot write into drops game from state, so the next refresh fetches in full.
func (r *Refresher) writeInto(flavours map[string]flavour, game, label string, a Answer, luas map[string][]byte, wanted map[string]*NamesWanted, state map[string]held) {
	for dir, f := range flavours {
		err := Write(dir, f.iface, luas[dir])
		r.Report.wrote(dir, err)
		if err != nil {
			fmt.Fprintf(r.Out, "prices not written into %s: %v\n", dir, err)
			delete(state, game)
			continue
		}
		names := ""
		if n := wanted[f.locale]; n != nil {
			names = fmt.Sprintf(", %d names wanted in %s", n.count(), n.Locale)
		}
		fmt.Fprintf(r.Out, "prices for %s written into %s (%d markets, %d items%s)\n", label, dir, len(a.Markets), items(a), names)
	}
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
