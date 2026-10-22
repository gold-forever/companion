// Package prices turns the server's pooled prices into the GoldForever_Data addon the game loads. The
// server sends JSON, never Lua: every number is checked and the Lua is written here, so a server can
// send wrong prices but cannot run code in the game.
package prices

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

// Item is one item's pooled figures: copper, contributors, and the newest scan in epoch seconds.
type Item struct {
	ID, Value, MinBuyout, Players, ScannedAt int64
}

// Prices is one market's answer, items sorted by id.
type Prices struct {
	At     int64
	Market string
	Items  []Item
}

type answer struct {
	V      json.RawMessage `json:"v"`
	Game   string          `json:"game"`
	Market string          `json:"market"`
	At     json.RawMessage `json:"at"`
	Items  []struct {
		ID        json.RawMessage `json:"id"`
		Value     json.RawMessage `json:"value"`
		MinBuyout json.RawMessage `json:"minBuyout"`
		Players   json.RawMessage `json:"players"`
		ScannedAt json.RawMessage `json:"scannedAt"`
	} `json:"items"`
}

// Parse checks the server's answer for game and market: any field out of shape refuses all of it.
func Parse(body []byte, game, market string) (Prices, error) {
	var a answer
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&a); err != nil {
		return Prices{}, fmt.Errorf("not a price answer: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Prices{}, errors.New("not a price answer: data after it")
	}
	if v, err := integer(a.V, version, version); err != nil || v != version {
		return Prices{}, fmt.Errorf("answer version: want %d", version)
	}
	if a.Game != game || a.Market != market {
		return Prices{}, fmt.Errorf("an answer for %q %q, not %q %q", a.Game, a.Market, game, market)
	}
	if !marketKey.MatchString(a.Market) {
		return Prices{}, fmt.Errorf("market key %q is not one", a.Market)
	}
	if a.Items == nil {
		return Prices{}, errors.New("no items list")
	}
	p := Prices{Market: a.Market}
	var err error
	if p.At, err = integer(a.At, 0, maxExact); err != nil {
		return Prices{}, fmt.Errorf("at: %w", err)
	}
	seen := map[int64]bool{}
	for i, raw := range a.Items {
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
			if *f.to, err = integer(f.num, f.min, maxExact); err != nil {
				return Prices{}, fmt.Errorf("item %d %s: %w", i, f.name, err)
			}
		}
		if seen[it.ID] {
			return Prices{}, fmt.Errorf("item %d named twice", it.ID)
		}
		seen[it.ID] = true
		p.Items = append(p.Items, it)
	}
	slices.SortFunc(p.Items, func(a, b Item) int { return cmp.Compare(a.ID, b.ID) })
	return p, nil
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
