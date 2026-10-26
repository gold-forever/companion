package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// SharedFetcher gets the account's shopping lists and sale profiles and their ETag,
// api.ErrNotModified when etag is still current, or api.ErrNoShared from a server without them;
// api.Client is the real one.
type SharedFetcher interface {
	SharedState(ctx context.Context, etag string) ([]byte, string, error)
}

// Shared is the account's shopping lists (GFL1 strings) and sale profiles (GFP1 strings) as the site
// holds them, the same for every game: when the set last changed (epoch seconds) and each item in the
// server's order. The addon merges them into its own by id, the newer edited winning.
type Shared struct {
	At       int64
	Lists    []SharedItem
	Profiles []SharedItem
}

// SharedItem is one list or profile: its id, when it was last edited (epoch seconds), and its string.
type SharedItem struct {
	ID     string
	Edited int64
	String string
}

// SharedCount is how many lists and profiles a Data.lua holds.
type SharedCount struct{ Lists, Profiles int }

// The state file's entry for the shared state, beside the games'.
const sharedKey = "shared"

// The most lists, and the most profiles, a state may hold.
const maxShared = 50

// An item's id as the site and the addon make one: nothing a Lua string needs escaped.
var sharedID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type rawShared struct {
	V        json.RawMessage `json:"v"`
	At       json.RawMessage `json:"at"`
	Lists    json.RawMessage `json:"lists"`
	Profiles json.RawMessage `json:"profiles"`
}

type rawItem struct {
	ID     json.RawMessage `json:"id"`
	Edited json.RawMessage `json:"edited"`
	String json.RawMessage `json:"string"`
}

// ParseShared checks the server's shared state: version 1, plain integers, at most 50 lists and 50
// profiles, safe ids none twice in a kind, and strings of printable ASCII under their kind's prefix.
// Any field out of shape refuses all of it.
func ParseShared(body []byte) (Shared, error) {
	var raw rawShared
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&raw); err != nil {
		return Shared{}, fmt.Errorf("not a shared state: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Shared{}, errors.New("not a shared state: data after it")
	}
	if v, err := integer(raw.V, version, version); err != nil || v != version {
		return Shared{}, fmt.Errorf("shared state version: want %d", version)
	}
	at, err := integer(raw.At, 0, maxExact)
	if err != nil {
		return Shared{}, fmt.Errorf("at: %w", err)
	}
	lists, err := sharedItems(raw.Lists, "GFL1:")
	if err != nil {
		return Shared{}, fmt.Errorf("lists: %w", err)
	}
	profiles, err := sharedItems(raw.Profiles, "GFP1:")
	if err != nil {
		return Shared{}, fmt.Errorf("profiles: %w", err)
	}
	return Shared{At: at, Lists: lists, Profiles: profiles}, nil
}

// sharedItems reads an array of at most maxShared items whose strings start with prefix.
func sharedItems(raw json.RawMessage, prefix string) ([]SharedItem, error) {
	var items []json.RawMessage
	if !bytes.HasPrefix(raw, []byte("[")) || json.Unmarshal(raw, &items) != nil {
		return nil, errors.New("not an array")
	}
	if len(items) > maxShared {
		return nil, fmt.Errorf("%d of them, over %d", len(items), maxShared)
	}
	var out []SharedItem
	seen := map[string]bool{}
	for i, r := range items {
		it, err := sharedItem(r, prefix)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		if seen[it.ID] {
			return nil, fmt.Errorf("%s named twice", it.ID)
		}
		seen[it.ID] = true
		out = append(out, it)
	}
	return out, nil
}

func sharedItem(r json.RawMessage, prefix string) (SharedItem, error) {
	var raw rawItem
	if !bytes.HasPrefix(r, []byte("{")) || json.Unmarshal(r, &raw) != nil {
		return SharedItem{}, errors.New("not an object")
	}
	var it SharedItem
	if json.Unmarshal(raw.ID, &it.ID) != nil || !bytes.HasPrefix(raw.ID, []byte(`"`)) || !sharedID.MatchString(it.ID) {
		return SharedItem{}, fmt.Errorf("id %s is not 1 to 64 of A-Z a-z 0-9 _ -", raw.ID)
	}
	edited, err := integer(raw.Edited, 0, maxExact)
	if err != nil {
		return SharedItem{}, fmt.Errorf("%s edited: %w", it.ID, err)
	}
	it.Edited = edited
	if json.Unmarshal(raw.String, &it.String) != nil || !bytes.HasPrefix(raw.String, []byte(`"`)) {
		return SharedItem{}, fmt.Errorf("%s: the string is not a string", it.ID)
	}
	if !strings.HasPrefix(it.String, prefix) || !printable(it.String) {
		return SharedItem{}, fmt.Errorf("%s: the string is not %s and printable ASCII", it.ID, prefix)
	}
	return it, nil
}

// printable says whether s is only bytes space..~.
func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// sharedPart is the shared entry of Data.lua, items in the server's order; empty for none.
func sharedPart(s *Shared) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s = {\n    %s = %d,\n", fieldShared, fieldAt, s.At)
	for _, kind := range []struct {
		name  string
		items []SharedItem
	}{{fieldLists, s.Lists}, {fieldProfiles, s.Profiles}} {
		fmt.Fprintf(&b, "    %s = {\n", kind.name)
		for _, it := range kind.items {
			fmt.Fprintf(&b, "      { %s = %s, %s = %d, %s = %s },\n",
				fieldID, quote(it.ID), fieldEdited, it.Edited, fieldString, quote(it.String))
		}
		b.WriteString("    },\n")
	}
	b.WriteString("  },\n")
	return b.String()
}
