package addondata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/server"
)

// The real client lists the names wanted, so the Refresher the app builds with it asks for them.
var _ NamesFetcher = (*server.Client)(nil)

const goodNames = `{"locale":"frFR","items":[2589,2592],"recipes":[2963],"quests":[],"factions":[76],"maps":[1411]}`

var wantNames = NamesWanted{Locale: "frFR", Items: []int64{2589, 2592}, Recipes: []int64{2963}, Factions: []int64{76}, Maps: []int64{1411}}

func TestParseNamesWantedReadsTheContractList(t *testing.T) {
	got, err := ParseNamesWanted([]byte(goodNames), "frFR")
	if err != nil || !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("ParseNamesWanted = %+v, %v", got, err)
	}
}

func TestParseNamesWantedTakesAKindLeftOutAsNoneAndIgnoresOneItDoesNotKnow(t *testing.T) {
	got, err := ParseNamesWanted([]byte(`{"locale":"deDE","items":[7],"npcs":[1]}`), "deDE")
	if err != nil || !reflect.DeepEqual(got, NamesWanted{Locale: "deDE", Items: []int64{7}}) {
		t.Fatalf("ParseNamesWanted = %+v, %v", got, err)
	}
}

func TestParseNamesWantedKeepsAnIDNamedTwiceOnceInItsPlace(t *testing.T) {
	got, err := ParseNamesWanted([]byte(`{"locale":"deDE","items":[9,7,9,3]}`), "deDE")
	if err != nil || !reflect.DeepEqual(got.Items, []int64{9, 7, 3}) {
		t.Fatalf("ParseNamesWanted = %+v, %v", got, err)
	}
}

func TestParseNamesWantedRefusesAMalformedList(t *testing.T) {
	over := "[" + strings.TrimSuffix(strings.Repeat("1,", maxWanted+1), ",") + "]"
	for name, body := range map[string]string{
		"not json":           `{`,
		"data after it":      goodNames + `{}`,
		"another locale":     strings.Replace(goodNames, "frFR", "deDE", 1),
		"no locale":          `{"items":[]}`,
		"a kind not a list":  `{"locale":"frFR","items":{}}`,
		"an id not a number": `{"locale":"frFR","items":["1"]}`,
		"a fraction":         `{"locale":"frFR","items":[1.5]}`,
		"id 0":               `{"locale":"frFR","items":[0]}`,
		"over 2^53":          `{"locale":"frFR","items":[9007199254740993]}`,
		"over the cap":       fmt.Sprintf(`{"locale":"frFR","items":%s}`, over),
	} {
		if _, err := ParseNamesWanted([]byte(body), "frFR"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLocaleOfReadsTheClientsLocaleFromASave(t *testing.T) {
	for save, want := range map[string]string{
		"GoldForeverDB = {\n[\"client\"] = {\n[\"locale\"] = \"frFR\",\n[\"interface\"] = 16001,\n},\n}\n": "frFR",
		"GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n":                           "",
		"GoldForeverDB = {\n[\"client\"] = {\n[\"locale\"] = \"fr\\\"FR\",\n},\n}\n":                       "",
	} {
		if got := localeOf([]byte(save)); got != want {
			t.Errorf("localeOf(%q) = %q, want %q", save, got, want)
		}
	}
}

func withNamesAnswer(t *testing.T) Answer {
	t.Helper()
	a := mustParse(t, canned(t))
	n := wantNames
	a.Names = &n
	return a
}

func TestLuaWritesTheNamesWantedBesideThePrices(t *testing.T) {
	got := Lua(withNamesAnswer(t))
	golden := filepath.Join("testdata", "names.lua")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Lua differs from testdata/names.lua (go test -update rewrites it):\n%s", got)
	}
}

func TestLuaWithoutNamesWantedWritesNoTable(t *testing.T) {
	if lua := Lua(mustParse(t, canned(t))); bytes.Contains(lua, []byte(fieldNamesWanted)) {
		t.Fatalf("a file without a list holds %s:\n%s", fieldNamesWanted, lua)
	}
}

// LuaJIT runs testdata/check_names.lua, the addon reading the list, and testdata/check.lua, an addon
// from before it that reads only the prices, on the same file.
func TestDataWithNamesWantedLoadsInLuaJITForANewAndAnOlderAddon(t *testing.T) {
	luajit, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("no luajit here: run testdata/check_names.lua and testdata/check.lua on testdata/names.lua")
	}
	path := filepath.Join(t.TempDir(), "Data.lua")
	if err := os.WriteFile(path, Lua(withNamesAnswer(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, check := range []string{"check_names.lua", "check.lua"} {
		out, err := exec.Command(luajit, filepath.Join("testdata", check), path).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("luajit %s: %v\n%s", check, err, out)
		}
	}
}

// namesServer is the server with the names wanted: each locale's list answers under its ETag, 304 to a
// request holding that ETag; a locale without a list is server.ErrNoNamesWanted, and err fails all.
type namesServer struct {
	*fetcher
	lists map[string]string // by locale
	etags map[string]string // by locale
	err   error
	asked []string // game, locale and the ETag sent
}

func (s *namesServer) NamesWanted(_ context.Context, game, locale, etag string) ([]byte, string, error) {
	s.asked = append(s.asked, game+" "+locale+" "+etag)
	body, ok := s.lists[locale]
	switch {
	case s.err != nil:
		return nil, "", s.err
	case !ok:
		return nil, "", server.ErrNoNamesWanted
	case etag != "" && etag == s.etags[locale]:
		return nil, etag, server.ErrNotModified
	}
	return []byte(body), s.etags[locale], nil
}

const deNames = `{"locale":"deDE","items":[7,8],"recipes":[],"quests":[11],"factions":[],"maps":[]}`

func newNamesServer(t *testing.T) *namesServer {
	return &namesServer{
		fetcher: &fetcher{body: canned(t), etag: `"e1"`},
		lists:   map[string]string{"frFR": goodNames, "deDE": deNames},
		etags:   map[string]string{"frFR": `"n-fr-1"`, "deDE": `"n-de-1"`},
	}
}

// speaks rewrites the save of flavour in install to name locale as its client's.
func speaks(t *testing.T, install, flavour, locale string) {
	t.Helper()
	path := filepath.Join(install, flavour, "WTF", "Account", "ACC", "SavedVariables", "GoldForever.lua")
	save, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	save = bytes.Replace(save, []byte("[\"client\"] = {\n"), []byte("[\"client\"] = {\n[\"locale\"] = \""+locale+"\",\n"), 1)
	if err := os.WriteFile(path, save, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readNames reads back the namesWanted table of a flavour's Data.lua: its locale and items line, ""
// without one.
func readNames(t *testing.T, install, flavour string) string {
	t.Helper()
	lua, err := os.ReadFile(filepath.Join(dataDir(install, flavour), "Data.lua"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(lua), "\n  namesWanted = {\n")
	if !found {
		return ""
	}
	block, _, _ := strings.Cut(rest, "\n  },\n")
	lines := strings.Split(block, "\n")
	return strings.TrimSpace(lines[0]) + " " + strings.TrimSpace(lines[1])
}

func TestRefreshWritesEachFlavoursNamesWantedInItsClientsLocale(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	speaks(t, install, "_forever_", "frFR")
	speaks(t, install, "_forever_ptr_", "deDE")
	s := newNamesServer(t)

	if err := refresher(s, saves, market, filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := readNames(t, install, "_forever_"); got != `locale = "frFR", items = {2589,2592},` {
		t.Errorf("_forever_ holds %q", got)
	}
	if got := readNames(t, install, "_forever_ptr_"); got != `locale = "deDE", items = {7,8},` {
		t.Errorf("_forever_ptr_ holds %q", got)
	}
	slices.Sort(s.asked)
	if want := []string{"forever deDE ", "forever frFR "}; !reflect.DeepEqual(s.asked, want) {
		t.Errorf("asked %q, want %q", s.asked, want)
	}
	if len(s.fetcher.asked) != 1 {
		t.Errorf("prices asked %q, want once for both flavours", s.fetcher.asked)
	}
}

func TestFlavoursOfOneLocaleAskForItsNamesOnce(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	speaks(t, install, "_forever_", "frFR")
	speaks(t, install, "_forever_ptr_", "frFR")
	s := newNamesServer(t)

	refresh(t, s, saves, market)

	if len(s.asked) != 1 {
		t.Errorf("asked %q, want once", s.asked)
	}
	for _, flavour := range []string{"_forever_", "_forever_ptr_"} {
		if got := readNames(t, install, flavour); !strings.HasPrefix(got, `locale = "frFR"`) {
			t.Errorf("%s holds %q", flavour, got)
		}
	}
}

func TestASaveWithoutALocaleAsksForNoNames(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	s := newNamesServer(t)

	refresh(t, s, saves, market)

	if len(s.asked) != 0 || readNames(t, install, "_forever_") != "" {
		t.Errorf("asked %q; Data.lua holds %q", s.asked, readNames(t, install, "_forever_"))
	}
}

func TestAServerWithoutTheLocalesNamesWritesNoTableAndNoError(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "enUS")
	s := newNamesServer(t)

	if err := refresher(s, saves, market, filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := readNames(t, install, "_forever_"); got != "" || len(s.asked) != 1 {
		t.Errorf("asked %q; Data.lua holds %q", s.asked, got)
	}
}

func TestNamesWantedAreAskedUnderTheirETagAfterARestartAndAll304LeaveDataLuaAsItIs(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "frFR")
	state := filepath.Join(t.TempDir(), "prices.json")
	s := newNamesServer(t)
	_ = refresher(s, saves, market, state).Refresh(context.Background())
	before := dataLua(t, install)
	stamp := time.Unix(1, 0)
	data := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	if err := os.Chtimes(data, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	_ = refresher(s, saves, market, state).Refresh(context.Background())

	if want := []string{"forever frFR ", `forever frFR "n-fr-1"`}; !reflect.DeepEqual(s.asked, want) {
		t.Errorf("names asked %q, want %q", s.asked, want)
	}
	if s.fetcher.sent[1] != `"e1"` {
		t.Errorf("prices asked with %q; want the second under its ETag", s.fetcher.sent)
	}
	if info, _ := os.Stat(data); !info.ModTime().Equal(stamp) || !bytes.Equal(dataLua(t, install), before) {
		t.Error("an all-304 refresh rewrote Data.lua")
	}
}

func TestChangedNamesWantedRewriteDataLuaWhileThePricesAre304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "frFR")
	s := newNamesServer(t)
	r := refresher(s, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	_ = r.Refresh(context.Background())
	s.lists["frFR"], s.etags["frFR"] = `{"locale":"frFR","items":[2592]}`, `"n-fr-2"`

	_ = r.Refresh(context.Background())

	if s.fetcher.sent[1] != `"e1"` {
		t.Errorf("prices asked with %q; want the second under its ETag", s.fetcher.sent)
	}
	if got := readNames(t, install, "_forever_"); got != `locale = "frFR", items = {2592},` {
		t.Errorf("Data.lua holds %q after a change", got)
	}
}

func TestAFailedOrMalformedNamesFetchKeepsTheLastGoodList(t *testing.T) {
	for name, broken := range map[string]func(*namesServer){
		"failed":    func(s *namesServer) { s.err = errors.New("down") },
		"malformed": func(s *namesServer) { s.lists["frFR"], s.etags["frFR"] = `{"locale":"frFR","items":{}}`, `"n-fr-2"` },
	} {
		t.Run(name, func(t *testing.T) {
			install, saves := tree(t, map[string]int{"_forever_": 16001})
			speaks(t, install, "_forever_", "frFR")
			state := filepath.Join(t.TempDir(), "prices.json")
			s := newNamesServer(t)
			_ = refresher(s, saves, market, state).Refresh(context.Background())
			broken(s)

			_ = refresher(s, saves, market, state).Refresh(context.Background())

			if got := readNames(t, install, "_forever_"); got != `locale = "frFR", items = {2589,2592},` {
				t.Errorf("Data.lua holds %q", got)
			}
		})
	}
}

func TestTheNewestSavesLocaleIsTheFlavours(t *testing.T) {
	install, _ := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "frFR")
	old := filepath.Join(install, "_forever_", "WTF", "Account", "ACC", "SavedVariables", "GoldForever.lua")
	if err := os.Chtimes(old, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(install, "_forever_", "WTF", "Account", "NEW", "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := "GoldForeverDB = {\n[\"client\"] = {\n[\"locale\"] = \"deDE\",\n[\"interface\"] = 16001,\n},\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte(save), 0o644); err != nil {
		t.Fatal(err)
	}

	got := flavoursByGame(find.Saves([]string{install}))["forever"][filepath.Join(install, "_forever_")]

	if got.locale != "deDE" {
		t.Errorf("locale = %q, want the newest save's", got.locale)
	}
}

func TestAFetcherWithoutNamesWantedWritesNoTable(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "frFR")

	if err := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := readNames(t, install, "_forever_"); got != "" {
		t.Errorf("Data.lua holds %q", got)
	}
}

func TestTheListOfALocaleNoSaveRunsAnyMoreIsForgotten(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	speaks(t, install, "_forever_", "frFR")
	state := filepath.Join(t.TempDir(), "prices.json")
	s := newNamesServer(t)
	r := refresher(s, saves, market, state)
	_ = r.Refresh(context.Background())
	speaks(t, install, "_forever_", "deDE") // the client's locale is now the first one the save names

	_ = r.Refresh(context.Background())

	held := r.state()
	if _, ok := held[namesKey("forever", "frFR")]; ok {
		t.Errorf("the frFR list is still held: %v", slices.Collect(maps.Keys(held)))
	}
	if _, ok := held[namesKey("forever", "deDE")]; !ok {
		t.Errorf("the deDE list is not held: %v", slices.Collect(maps.Keys(held)))
	}
	if got := readNames(t, install, "_forever_"); !strings.HasPrefix(got, `locale = "deDE"`) {
		t.Errorf("Data.lua holds %q", got)
	}
}

// fullNames is a list at the server's cap in every kind, of IDs as long as ids gives.
func fullNames(ids func(kind, i int) int64) *NamesWanted {
	n := &NamesWanted{Locale: "frFR"}
	for k, to := range []*[]int64{&n.Items, &n.Recipes, &n.Quests, &n.Factions, &n.Maps} {
		for i := range maxWanted {
			*to = append(*to, ids(k, i))
		}
	}
	return n
}

// The list adds at most about 70 KB, and the largest Forever file expected (4 markets of 4,000 items
// with every figure) stays under its limit with it.
func TestAFullListOfNamesWantedFitsTheDataFile(t *testing.T) {
	fixture := mustParse(t, canned(t))
	base := len(Lua(fixture))
	worst := fixture
	worst.Names = fullNames(func(_, i int) int64 { return 100000 + int64(i) }) // 6 digits each
	real := fixture
	real.Names = fullNames(func(k, i int) int64 { // items, recipes, quests 5 digits; factions 3; maps 4
		return []int64{10000, 10000, 1000, 100, 1000}[k] + int64(i)
	})
	growth, realGrowth := len(Lua(worst))-base, len(Lua(real))-base
	t.Logf("testdata/answer.json: %d bytes; a full list adds %d (6-digit IDs) or %d (IDs as long as Forever's)", base, growth, realGrowth)
	if growth > 72<<10 {
		t.Errorf("a full list adds %d bytes, over 72 KB", growth)
	}

	big := Answer{At: 1761148800, Names: worst.Names}
	for m := range 4 {
		p := Prices{Market: fmt.Sprintf("forever-EU-m%d-horde", m)}
		for i := range 4000 {
			p.Items = append(p.Items, Item{ID: int64(100000 + i), Value: 123456, MinBuyout: 123456, Players: 9, ScannedAt: 1761148800,
				Week: 123456, Units: 9, Thin: true, Sold: 999, HasSold: true, Listed: 999, HasListed: true, DayPrice: 123456, HasDayPrice: true})
		}
		big.Markets = append(big.Markets, p)
	}
	size := len(Lua(big))
	t.Logf("4 Forever markets of 4,000 items with every figure and a full list: %d bytes, limit %d", size, limits["forever"].data)
	if size > limits["forever"].data {
		t.Errorf("%d bytes, over the %d-byte limit", size, limits["forever"].data)
	}
}
