package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
)

// CharactersFetcher gets the account's characters and their ETag, server.ErrNotModified when etag is
// still current, or server.ErrNoCharacters from a server without the list; server.Client is the real
// one.
type CharactersFetcher interface {
	Characters(ctx context.Context, etag string) ([]byte, string, error)
}

// Characters is the account's own characters on every WoW account linked to it, in the server's order,
// so the addon knows mail to any of them as mail to its own. Skipped counts those left out: of a game
// the companion does not know, or with a realm or name no character has.
type Characters struct {
	List    []Character
	Skipped int
}

// Character is one of the account's characters: its game (as find.Game names it), realm and name.
type Character struct {
	Game, Realm, Name string
}

// The state file's entry for the account's characters, beside the games'.
const charactersKey = "characters"

// The most characters a list may hold: far over the 50 of each of a few WoW accounts.
const maxCharacters = 2000

// The longest realm or name to write, in bytes: a game's are a dozen or two letters, up to 4 bytes each.
const maxNameBytes = 64

type rawCharacters struct {
	Characters json.RawMessage `json:"characters"`
}

type rawCharacter struct {
	Game  *string `json:"game"`
	Realm *string `json:"realm"`
	Name  *string `json:"name"`
}

// ParseCharacters checks the server's list of characters: an array of at most maxCharacters, each with
// a string game, realm and name. Any field out of shape refuses all of it; a character of another game
// or with a realm or name no character has is only skipped, and one named twice is kept once.
func ParseCharacters(body []byte) (Characters, error) {
	var raw rawCharacters
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&raw); err != nil {
		return Characters{}, fmt.Errorf("not a list of characters: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Characters{}, errors.New("not a list of characters: data after it")
	}
	var items []json.RawMessage
	if !bytes.HasPrefix(raw.Characters, []byte("[")) || json.Unmarshal(raw.Characters, &items) != nil {
		return Characters{}, errors.New("characters: not an array")
	}
	if len(items) > maxCharacters {
		return Characters{}, fmt.Errorf("characters: %d of them, over %d", len(items), maxCharacters)
	}
	var c Characters
	for i, r := range items {
		var raw rawCharacter
		if !bytes.HasPrefix(r, []byte("{")) || json.Unmarshal(r, &raw) != nil || raw.Game == nil || raw.Realm == nil || raw.Name == nil {
			return Characters{}, fmt.Errorf("character %d: not an object of a game, realm and name", i)
		}
		ch := Character{Game: *raw.Game, Realm: *raw.Realm, Name: *raw.Name}
		switch {
		case !slices.Contains(games, ch.Game) || !writable(ch.Realm) || !writable(ch.Name):
			c.Skipped++
		case !slices.Contains(c.List, ch):
			c.List = append(c.List, ch)
		}
	}
	return c, nil
}

// writable says whether s can be a realm or a character's name: not empty, at most maxNameBytes, and
// no control characters.
func writable(s string) bool {
	return s != "" && len(s) <= maxNameBytes && !strings.ContainsFunc(s, unicode.IsControl)
}

// note is what the companion's log says once of a list received with characters left out; empty when
// none were.
func (c Characters) note() string {
	if c.Skipped == 0 {
		return ""
	}
	return fmt.Sprintf("left out %d character(s) of another game or with an unusable realm or name", c.Skipped)
}

// in is the list of game's characters only, empty when it has none; nil for no list.
func (c *Characters) in(game string) *Characters {
	if c == nil {
		return nil
	}
	out := &Characters{}
	for _, ch := range c.List {
		if ch.Game == game {
			out.List = append(out.List, ch)
		}
	}
	return out
}

// charactersPart is the characters entry of Data.lua, in the list's order; empty for none.
func charactersPart(c *Characters) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s = {\n", fieldCharacters)
	for _, ch := range c.List {
		fmt.Fprintf(&b, "    { %s = %s, %s = %s },\n", fieldRealm, quote(ch.Realm), fieldName, quote(ch.Name))
	}
	b.WriteString("  },\n")
	return b.String()
}
