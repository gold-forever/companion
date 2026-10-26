// Package addondata turns the server's pooled prices, dings and shared lists into the GoldForever_Data
// addon the game loads. The server sends JSON, never Lua: every number is checked and the Lua is
// written here, so a server can send wrong prices but cannot run code in the game.
package addondata

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
)

// The answer's shape this companion reads; another version is refused, keeping the last good file.
const version = 1

// Lua numbers are doubles: integers up to 2^53 are exact.
const maxExact = 1<<53 - 1

// A market key as the server names one (forever-EU-normal-horde): nothing a Lua string needs escaped.
var marketKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)

// Item is one item's pooled figures: copper, contributors, the newest scan in epoch seconds, and the
// 7-day mean of daily values in copper, 0 when the server sent none.
type Item struct {
	ID, Value, MinBuyout, Players, ScannedAt, Week int64
}

// Prices is one market's items, sorted by id.
type Prices struct {
	Market string
	Items  []Item
}

// Answer is what the server sent: when it built it, and its markets sorted by key. Dings, when not nil,
// is the account's dings state written beside them.
type Answer struct {
	At      int64
	Markets []Prices
	Dings   *Dings
	Shared  *Shared
}

type rawMarket struct {
	Market string `json:"market"`
	Items  []struct {
		ID        json.RawMessage `json:"id"`
		Value     json.RawMessage `json:"value"`
		MinBuyout json.RawMessage `json:"minBuyout"`
		Players   json.RawMessage `json:"players"`
		ScannedAt json.RawMessage `json:"scannedAt"`
		Week      json.RawMessage `json:"week"`
	} `json:"items"`
}

// answer holds both shapes: one market's (market and items at the top) and the account's (markets).
type answer struct {
	V    json.RawMessage `json:"v"`
	Game string          `json:"game"`
	At   json.RawMessage `json:"at"`
	rawMarket
	Markets []rawMarket `json:"markets"`
}

// Parse checks the server's answer for game: for market when one was asked for, else the account's
// markets. Any field out of shape refuses all of it.
func Parse(body []byte, game, market string) (Answer, error) {
	var a answer
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&a); err != nil {
		return Answer{}, fmt.Errorf("not a price answer: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Answer{}, errors.New("not a price answer: data after it")
	}
	if v, err := integer(a.V, version, version); err != nil || v != version {
		return Answer{}, fmt.Errorf("answer version: want %d", version)
	}
	if a.Game != game {
		return Answer{}, fmt.Errorf("an answer for %q, not %q", a.Game, game)
	}
	raw := a.Markets
	switch {
	case market != "" && a.Market != market:
		return Answer{}, fmt.Errorf("an answer for %q, not %q", a.Market, market)
	case market != "":
		raw = []rawMarket{a.rawMarket}
	case raw == nil:
		return Answer{}, errors.New("no markets list")
	}
	out := Answer{}
	var err error
	if out.At, err = integer(a.At, 0, maxExact); err != nil {
		return Answer{}, fmt.Errorf("at: %w", err)
	}
	seen := map[string]bool{}
	for _, m := range raw {
		p, err := parseMarket(m)
		if err != nil {
			return Answer{}, err
		}
		if seen[p.Market] {
			return Answer{}, fmt.Errorf("market %s named twice", p.Market)
		}
		seen[p.Market] = true
		out.Markets = append(out.Markets, p)
	}
	slices.SortFunc(out.Markets, func(a, b Prices) int { return cmp.Compare(a.Market, b.Market) })
	return out, nil
}

func parseMarket(m rawMarket) (Prices, error) {
	if !marketKey.MatchString(m.Market) {
		return Prices{}, fmt.Errorf("market key %q is not one", m.Market)
	}
	if m.Items == nil {
		return Prices{}, fmt.Errorf("market %s: no items list", m.Market)
	}
	p := Prices{Market: m.Market}
	seen := map[int64]bool{}
	for i, raw := range m.Items {
		var it Item
		for _, f := range []struct {
			name string
			num  json.RawMessage
			min  int64
			to   *int64
		}{
			{"id", raw.ID, 1, &it.ID},
			{"value", raw.Value, 0, &it.Value},
			{"minBuyout", raw.MinBuyout, 0, &it.MinBuyout},
			{"players", raw.Players, 0, &it.Players},
			{"scannedAt", raw.ScannedAt, 0, &it.ScannedAt},
		} {
			var err error
			if *f.to, err = integer(f.num, f.min, maxExact); err != nil {
				return Prices{}, fmt.Errorf("market %s item %d %s: %w", m.Market, i, f.name, err)
			}
		}
		it.Week = optional(raw.Week, 1, maxExact)
		if seen[it.ID] {
			return Prices{}, fmt.Errorf("market %s: item %d named twice", m.Market, it.ID)
		}
		seen[it.ID] = true
		p.Items = append(p.Items, it)
	}
	slices.SortFunc(p.Items, func(a, b Item) int { return cmp.Compare(a.ID, b.ID) })
	return p, nil
}

// optional reads a field a server may leave out: one absent or out of shape is 0, none, so a single
// odd value cannot refuse a whole market.
func optional(n json.RawMessage, min, max int64) int64 {
	v, err := integer(n, min, max)
	if err != nil {
		return 0
	}
	return v
}

// integer reads a JSON value written as a plain integer between min and max: 1.0, 1e3, "1" and null
// are not.
func integer(n json.RawMessage, min, max int64) (int64, error) {
	if n == nil {
		return 0, errors.New("missing")
	}
	v, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not an integer", n)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%d is outside %d..%d", v, min, max)
	}
	return v, nil
}
