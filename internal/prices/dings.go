package prices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// DingsFetcher gets the site's side of the in-game "Announce dings on Discord" switch and its ETag,
// api.ErrNotModified when etag is still current, or api.ErrNoDings from a server without it;
// api.Client is the real one.
type DingsFetcher interface {
	DingsState(ctx context.Context, etag string) ([]byte, string, error)
}

// Dings is the site's side of the switch, the same for every game of the account: when it last changed
// (epoch seconds, which the addon compares its own choices against), whether a level-up webhook is
// connected, and each character's choice by its guid.
type Dings struct {
	At         int64
	Webhook    bool
	Characters map[string]bool
}

// A player's guid as the game names one (Player-4620-006C0FC1): nothing a Lua string needs escaped.
var guidKey = regexp.MustCompile(`^Player-[0-9]{1,10}-[0-9A-Fa-f]{1,16}$`)

type rawDings struct {
	V          json.RawMessage `json:"v"`
	At         json.RawMessage `json:"at"`
	Webhook    json.RawMessage `json:"webhook"`
	Characters json.RawMessage `json:"characters"`
}

// ParseDings checks the server's dings state: version 1, a plain integer at, booleans only, guids of a
// player. Any field out of shape refuses all of it; an older server's state without at too.
func ParseDings(body []byte) (Dings, error) {
	var raw rawDings
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&raw); err != nil {
		return Dings{}, fmt.Errorf("not a dings state: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Dings{}, errors.New("not a dings state: data after it")
	}
	if v, err := integer(raw.V, version, version); err != nil || v != version {
		return Dings{}, fmt.Errorf("dings state version: want %d", version)
	}
	at, err := integer(raw.At, 0, maxExact)
	if err != nil {
		return Dings{}, fmt.Errorf("at: %w", err)
	}
	webhook, err := boolean(raw.Webhook)
	if err != nil {
		return Dings{}, fmt.Errorf("webhook: %w", err)
	}
	characters, err := choices(raw.Characters)
	if err != nil {
		return Dings{}, fmt.Errorf("characters: %w", err)
	}
	return Dings{At: at, Webhook: webhook, Characters: characters}, nil
}

// choices reads an object of player guids to booleans, none named twice.
func choices(raw json.RawMessage) (map[string]bool, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	out := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		guid, _ := tok.(string)
		if !guidKey.MatchString(guid) {
			return nil, fmt.Errorf("%q is not a player's guid", guid)
		}
		if _, seen := out[guid]; seen {
			return nil, fmt.Errorf("%s named twice", guid)
		}
		var choice json.RawMessage
		if err := dec.Decode(&choice); err != nil {
			return nil, err
		}
		if out[guid], err = boolean(choice); err != nil {
			return nil, fmt.Errorf("%s: %w", guid, err)
		}
	}
	return out, nil
}

// boolean reads a JSON true or false: 1, "true" and null are not.
func boolean(raw json.RawMessage) (bool, error) {
	switch string(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%q is not true or false", raw)
}

// dingsPart is the dings entry of Data.lua, characters sorted by guid; empty for none.
func dingsPart(d *Dings) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  dings = {\n    at = %d,\n    webhook = %t,\n    characters = {\n", d.At, d.Webhook)
	guids := make([]string, 0, len(d.Characters))
	for guid := range d.Characters {
		guids = append(guids, guid)
	}
	slices.Sort(guids)
	for _, guid := range guids {
		fmt.Fprintf(&b, "      [%s] = %t,\n", quote(guid), d.Characters[guid])
	}
	b.WriteString("    },\n  },\n")
	return b.String()
}
