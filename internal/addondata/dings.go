package addondata

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
// server.ErrNotModified when etag is still current, or server.ErrNoDings from a server without it;
// server.Client is the real one.
type DingsFetcher interface {
	DingsState(ctx context.Context, etag string) ([]byte, string, error)
}

// Dings is the site's side of the switch, the same for every game of the account: when it last changed
// (epoch seconds, which the addon compares its own choices against), whether a level-up webhook is
// connected, and each character's choice by its guid; Skipped counts the characters left out, the
// server naming them by a key that is not a player's guid.
type Dings struct {
	At         int64
	Webhook    bool
	Characters map[string]bool
	Skipped    int
}

// A player's guid as the game names one (Player-4620-006C0FC1): nothing a Lua string needs escaped.
var guidKey = regexp.MustCompile(`^Player-[0-9]{1,10}-[0-9A-Fa-f]{1,16}$`)

type rawDings struct {
	V          json.RawMessage `json:"v"`
	At         json.RawMessage `json:"at"`
	Webhook    json.RawMessage `json:"webhook"`
	Characters json.RawMessage `json:"characters"`
}

// ParseDings checks the server's dings state: version 1, a plain integer at, booleans only. Any field
// out of shape refuses all of it, an older server's state without at too; a character keyed by
// anything but a player's guid is only skipped.
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
	characters, skipped, err := choices(raw.Characters)
	if err != nil {
		return Dings{}, fmt.Errorf("characters: %w", err)
	}
	return Dings{At: at, Webhook: webhook, Characters: characters, Skipped: skipped}, nil
}

// choices reads an object of player guids to booleans, none named twice, and counts the choices
// skipped for a key that is not a player's guid: one the Lua string could not hold safely.
func choices(raw json.RawMessage) (map[string]bool, int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, 0, errors.New("not an object")
	}
	out, skipped := map[string]bool{}, 0
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, 0, err
		}
		guid, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, 0, err
		}
		choice, err := boolean(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("%q: %w", guid, err)
		}
		if !guidKey.MatchString(guid) {
			skipped++
			continue
		}
		if _, seen := out[guid]; seen {
			return nil, 0, fmt.Errorf("%s named twice", guid)
		}
		out[guid] = choice
	}
	return out, skipped, nil
}

// note is what the companion's log says once of a state received with characters skipped; empty
// when none were.
func (d Dings) note() string {
	if d.Skipped == 0 {
		return ""
	}
	return fmt.Sprintf("left out %d character(s) whose key is not a player's guid", d.Skipped)
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
	fmt.Fprintf(&b, "  %s = {\n    %s = %d,\n    %s = %t,\n    %s = {\n",
		fieldDings, fieldAt, d.At, fieldWebhook, d.Webhook, fieldCharacters)
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
