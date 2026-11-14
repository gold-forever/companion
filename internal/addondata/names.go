package addondata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gold-forever/companion/internal/server"
)

// NamesFetcher gets the IDs whose names in locale the server lacks for game, and their ETag:
// server.ErrNotModified when etag is still current, server.ErrNoNamesWanted when it lists none for
// locale. server.Client is the real one; the Refresher asks its Fetcher when it is one too.
type NamesFetcher interface {
	NamesWanted(ctx context.Context, game, locale, etag string) ([]byte, string, error)
}

// NamesWanted is, for one client locale, the IDs of each kind the server knows no name for in it, those
// to ask first first (gf#397): the addon asks its own client for them and records the names it gets.
type NamesWanted struct {
	Locale                                 string
	Items, Recipes, Quests, Factions, Maps []int64
}

// The most IDs of a kind to take: the server caps each at 2,000.
const maxWanted = 2000

// A client locale as GetLocale() gives it: nothing a Lua string needs escaped.
var clientLocale = regexp.MustCompile(`^[a-z]{2}[A-Z]{2}$`)

// The save's client table, which the addon rewrites at every load, names the locale once.
var localeLine = regexp.MustCompile(`\["locale"\]\s*=\s*"([^"]*)"`)

// localeOf reads the locale of the client that wrote a save, "" when it names none a client has.
func localeOf(save []byte) string {
	m := localeLine.FindSubmatch(save)
	if m == nil || !clientLocale.Match(m[1]) {
		return ""
	}
	return string(m[1])
}

// The state file's entries for a game's list of names wanted in a locale, beside the games'.
const namesPrefix = "names-wanted "

func namesKey(game, locale string) string { return namesPrefix + game + " " + locale }

type rawNames struct {
	Locale   *string         `json:"locale"`
	Items    json.RawMessage `json:"items"`
	Recipes  json.RawMessage `json:"recipes"`
	Quests   json.RawMessage `json:"quests"`
	Factions json.RawMessage `json:"factions"`
	Maps     json.RawMessage `json:"maps"`
}

// ParseNamesWanted checks the server's list for locale: the locale asked for and, per kind, at most
// maxWanted integer IDs. A kind left out is none and a field it does not know is ignored, so a newer
// server's list still reads; any kind out of shape refuses all of it. An ID named twice is kept once,
// where it first stands.
func ParseNamesWanted(body []byte, locale string) (NamesWanted, error) {
	var raw rawNames
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&raw); err != nil {
		return NamesWanted{}, fmt.Errorf("not a list of names wanted: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return NamesWanted{}, errors.New("not a list of names wanted: data after it")
	}
	if raw.Locale == nil || *raw.Locale != locale {
		return NamesWanted{}, fmt.Errorf("a list for another locale than %q", locale)
	}
	n := NamesWanted{Locale: locale}
	for _, k := range []struct {
		name string
		raw  json.RawMessage
		to   *[]int64
	}{
		{"items", raw.Items, &n.Items},
		{"recipes", raw.Recipes, &n.Recipes},
		{"quests", raw.Quests, &n.Quests},
		{"factions", raw.Factions, &n.Factions},
		{"maps", raw.Maps, &n.Maps},
	} {
		ids, err := wantedIDs(k.raw)
		if err != nil {
			return NamesWanted{}, fmt.Errorf("%s: %w", k.name, err)
		}
		*k.to = ids
	}
	return n, nil
}

// wantedIDs reads one kind's array of IDs; nil when the kind was left out or is empty.
func wantedIDs(raw json.RawMessage) ([]int64, error) {
	if raw == nil {
		return nil, nil
	}
	var items []json.RawMessage
	if !bytes.HasPrefix(raw, []byte("[")) || json.Unmarshal(raw, &items) != nil {
		return nil, errors.New("not an array")
	}
	if len(items) > maxWanted {
		return nil, fmt.Errorf("%d IDs, over %d", len(items), maxWanted)
	}
	var ids []int64
	seen := map[int64]bool{}
	for i, item := range items {
		id, err := integer(item, 1, maxExact)
		if err != nil {
			return nil, fmt.Errorf("ID %d: %w", i, err)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// namesPart is the namesWanted entry of Data.lua: the locale, then each kind's IDs in the server's
// order on one line without spaces, a kind with none as {}. Empty for no list.
func namesPart(n *NamesWanted) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s = {\n    %s = %s,\n", fieldNamesWanted, fieldLocale, quote(n.Locale))
	for _, k := range []struct {
		name string
		ids  []int64
	}{
		{fieldItems, n.Items}, {fieldRecipes, n.Recipes}, {fieldQuests, n.Quests}, {fieldFactions, n.Factions}, {fieldMaps, n.Maps},
	} {
		fmt.Fprintf(&b, "    %s = {", k.name)
		for i, id := range k.ids {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatInt(id, 10))
		}
		b.WriteString("},\n")
	}
	b.WriteString("  },\n")
	return b.String()
}

// count is how many IDs the list asks for, of every kind.
func (n *NamesWanted) count() int {
	return len(n.Items) + len(n.Recipes) + len(n.Quests) + len(n.Factions) + len(n.Maps)
}

// namesWanted fetches game's names wanted once per locale its flavours' clients run, each under the
// ETag of the list held, marking each key asked. A failed fetch or a list out of shape keeps the one
// held; a locale the server lists none for has none. Nil when the Fetcher lists no names wanted.
func (r *Refresher) namesWanted(ctx context.Context, game string, flavours map[string]flavour, state map[string]held, asked map[string]bool) (map[string]*NamesWanted, error) {
	names, ok := r.Fetcher.(NamesFetcher)
	if !ok {
		return nil, nil
	}
	out := map[string]*NamesWanted{}
	var errs []error
	for _, f := range flavours {
		locale := f.locale
		if _, done := out[locale]; done || locale == "" {
			continue
		}
		key := namesKey(game, locale)
		asked[key] = true
		n, err := accountState(ctx, r, state, key, "names wanted in "+locale+" for "+game,
			func(ctx context.Context, etag string) ([]byte, string, error) {
				return names.NamesWanted(ctx, game, locale, etag)
			},
			func(body []byte) (NamesWanted, error) { return ParseNamesWanted(body, locale) },
			server.ErrNoNamesWanted)
		out[locale] = n
		errs = append(errs, err)
	}
	return out, errors.Join(errs...)
}

// forgetNames drops the lists held for a game and locale no save runs any more.
func forgetNames(state map[string]held, asked map[string]bool) {
	for key := range state {
		if strings.HasPrefix(key, namesPrefix) && !asked[key] {
			delete(state, key)
		}
	}
}

// namesDigest stands for the names wanted each flavour's file is written with, so a change to any list
// or to a flavour's locale writes the files again; "" when no file carries one, as an older companion's.
func namesDigest(flavours map[string]flavour, wanted map[string]*NamesWanted) string {
	dirs := slices.Sorted(maps.Keys(flavours))
	h := sha256.New()
	carried := false
	for _, dir := range dirs {
		n := wanted[flavours[dir].locale]
		carried = carried || n != nil
		fmt.Fprintf(h, "%s\x00%s\x00", dir, namesPart(n))
	}
	if !carried {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
