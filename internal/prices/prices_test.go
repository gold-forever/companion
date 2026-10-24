package prices

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/find"
)

var update = flag.Bool("update", false, "rewrite testdata/Data.lua from testdata/answer.json")

const market = "forever-BETA-normal-horde"

func canned(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "answer.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseReadsTheContractAnswer(t *testing.T) {
	p, err := Parse(canned(t), "forever", market)

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.At != 1761148800 || len(p.Markets) != 1 || p.Markets[0].Market != market || len(p.Markets[0].Items) != 3 {
		t.Fatalf("Parse = %+v", p)
	}
	if p.Markets[0].Items[0] != (Item{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000}) {
		t.Errorf("first item = %+v (items are sorted by id)", p.Markets[0].Items[0])
	}
}

func cannedMarkets(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "markets.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseReadsEveryMarketOfTheAccount(t *testing.T) {
	a, err := Parse(cannedMarkets(t), "forever", "")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if a.At != 1761148800 || len(a.Markets) != 2 {
		t.Fatalf("Parse = %+v", a)
	}
	if a.Markets[0].Market != "forever-BETA-normal-alliance" || a.Markets[1].Market != market {
		t.Errorf("markets %q, %q: want them sorted by key", a.Markets[0].Market, a.Markets[1].Market)
	}
	if a.Markets[0].Items[0].ID != 2589 || a.Markets[0].Items[1].ID != 14047 {
		t.Errorf("alliance items = %+v (sorted by id)", a.Markets[0].Items)
	}
}

func TestParseReadsAnAccountWithoutMarkets(t *testing.T) {
	a, err := Parse([]byte(`{"v":1,"game":"forever","at":5,"markets":[]}`), "forever", "")
	if err != nil || a.At != 5 || len(a.Markets) != 0 {
		t.Fatalf("Parse = %+v, %v", a, err)
	}
}

func accountWith(markets string) string {
	return `{"v":1,"game":"forever","at":1761148800,"markets":[` + markets + `]}`
}

const goodItems = `[{"id":1,"value":1,"minBuyout":1,"players":1,"scannedAt":1}]`

var malformedAccount = map[string]string{
	"version 2":               strings.Replace(accountWith(""), `"v":1`, `"v":2`, 1),
	"another game":            strings.Replace(accountWith(""), `"forever"`, `"retail"`, 1),
	"no markets list":         `{"v":1,"game":"forever","at":1}`,
	"one market's answer":     answerWith("v", `1`),
	"a market without items":  accountWith(`{"market":"` + market + `"}`),
	"a market without a key":  accountWith(`{"items":` + goodItems + `}`),
	"a market key with quote": accountWith(`{"market":"a"]b","items":` + goodItems + `}`),
	"a market named twice":    accountWith(`{"market":"` + market + `","items":` + goodItems + `},{"market":"` + market + `","items":[]}`),
	"a float price":           accountWith(`{"market":"` + market + `","items":[{"id":1,"value":1.5,"minBuyout":1,"players":1,"scannedAt":1}]}`),
	"a duplicated item":       accountWith(`{"market":"` + market + `","items":[` + goodItems[1:len(goodItems)-1] + `,` + goodItems[1:len(goodItems)-1] + `]}`),
	"trailing data":           accountWith("") + `{}`,
}

func TestParseRefusesAMalformedAccountAnswer(t *testing.T) {
	if _, err := Parse([]byte(accountWith(`{"market":"`+market+`","items":`+goodItems+`}`)), "forever", ""); err != nil {
		t.Fatalf("the well-formed base answer must pass: %v", err)
	}
	for name, body := range malformedAccount {
		if _, err := Parse([]byte(body), "forever", ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOneMarketsAnswerRefusesTheAccountsShape(t *testing.T) {
	if _, err := Parse(cannedMarkets(t), "forever", market); err == nil {
		t.Fatal("an answer of several markets accepted for one market")
	}
}

func TestLuaHoldsEveryMarketOfTheAccount(t *testing.T) {
	a, err := Parse(cannedMarkets(t), "forever", "")
	if err != nil {
		t.Fatal(err)
	}
	got := Lua(a)
	golden := filepath.Join("testdata", "markets.lua")
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
		t.Fatalf("Lua differs from testdata/markets.lua (go test -update rewrites it):\n%s", got)
	}
	if back, ok := readBack(got); !ok || !same(back, a) {
		t.Fatalf("read back %+v, want %+v", back, a)
	}
}

func answerWith(field, value string) string {
	fields := map[string]string{
		"v": `1`, "game": `"forever"`, "market": `"` + market + `"`, "at": `1761148800`,
		"id": `2589`, "value": `1234`, "minBuyout": `1100`, "players": `1`, "scannedAt": `1761140000`,
	}
	fields[field] = value
	return `{"v":` + fields["v"] + `,"game":` + fields["game"] + `,"market":` + fields["market"] + `,"at":` + fields["at"] +
		`,"items":[{"id":` + fields["id"] + `,"value":` + fields["value"] + `,"minBuyout":` + fields["minBuyout"] +
		`,"players":` + fields["players"] + `,"scannedAt":` + fields["scannedAt"] + `}]}`
}

var malformed = map[string]string{
	"version 2":                  answerWith("v", `2`),
	"version as a string":        answerWith("v", `"1"`),
	"another game":               answerWith("game", `"retail"`),
	"another market":             answerWith("market", `"forever-EU-normal-horde"`),
	"a float price":              answerWith("value", `12.5`),
	"a whole float price":        answerWith("value", `12.0`),
	"an exponent price":          answerWith("value", `1e3`),
	"a negative price":           answerWith("value", `-1`),
	"a negative minimum buyout":  answerWith("minBuyout", `-5`),
	"a price at 2^53":            answerWith("value", `9007199254740992`),
	"a price as a string":        answerWith("value", `"1234"`),
	"a null price":               answerWith("value", `null`),
	"item id zero":               answerWith("id", `0`),
	"a negative item id":         answerWith("id", `-2589`),
	"a float item id":            answerWith("id", `2589.5`),
	"negative players":           answerWith("players", `-1`),
	"a negative scan time":       answerWith("scannedAt", `-1`),
	"a float build time":         answerWith("at", `1761148800.5`),
	"a market key with a quote":  answerWith("market", `"forever\"] = os.exit() --"`),
	"a market key with newlines": answerWith("market", `"forever\nos.exit()"`),
	"a market key with brackets": answerWith("market", `"forever]]..os.exit()..[["`),
	"an empty market key":        answerWith("market", `""`),
	"a missing price":            `{"v":1,"game":"forever","market":"` + market + `","at":1,"items":[{"id":1,"minBuyout":1,"players":1,"scannedAt":1}]}`,
	"no items list":              `{"v":1,"game":"forever","market":"` + market + `","at":1}`,
	"a duplicated item":          `{"v":1,"game":"forever","market":"` + market + `","at":1,"items":[{"id":1,"value":1,"minBuyout":1,"players":1,"scannedAt":1},{"id":1,"value":2,"minBuyout":1,"players":1,"scannedAt":1}]}`,
	"trailing data":              answerWith("v", `1`) + `{}`,
	"not JSON":                   `GoldForever_Prices = { os.exit() }`,
	"empty":                      ``,
}

func TestParseRefusesAMalformedAnswer(t *testing.T) {
	if _, err := Parse([]byte(answerWith("v", `1`)), "forever", market); err != nil {
		t.Fatalf("the well-formed base answer must pass: %v", err)
	}
	for name, body := range malformed {
		if _, err := Parse([]byte(body), "forever", market); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseRefusesAMarketKeyOutsideTheCharset(t *testing.T) {
	for _, key := range []string{`a"b`, "a\nb", "a]b", "a[b", `a\b`, "a b", "a\x00b", strings.Repeat("a", 81)} {
		if _, err := Parse([]byte(answerWith("market", strconv.Quote(key))), "forever", key); err == nil {
			t.Errorf("market %q accepted", key)
		}
	}
}

func TestLuaIsTheContractTable(t *testing.T) {
	p, err := Parse(canned(t), "forever", market)
	if err != nil {
		t.Fatal(err)
	}
	got := Lua(p)
	golden := filepath.Join("testdata", "Data.lua")
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
		t.Fatalf("Lua differs from testdata/Data.lua (go test -update rewrites it):\n%s", got)
	}
}

// LuaJIT is not in the build container; the host runs testdata/check.lua on testdata/Data.lua and
// testdata/check_markets.lua on testdata/markets.lua, testdata/check_dings.lua on
// testdata/dings.lua and testdata/check_shared.lua on testdata/shared.lua, which the golden tests keep equal to the writer's output.
func TestDataLoadsInLuaJIT(t *testing.T) {
	luajit, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("no luajit here: run testdata/check*.lua with luajit on the host")
	}
	for check, a := range map[string]Answer{
		"check.lua":         mustParse(t, canned(t)),
		"check_markets.lua": mustParseAccount(t, cannedMarkets(t)),
		"check_dings.lua":   withDingsAnswer(t),
		"check_shared.lua":  withSharedAnswer(t),
	} {
		path := filepath.Join(t.TempDir(), "Data.lua")
		if err := os.WriteFile(path, Lua(a), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(luajit, filepath.Join("testdata", check), path).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("luajit %s: %v\n%s", check, err, out)
		}
	}
}

func TestLuaValuesRoundTrip(t *testing.T) {
	p, _ := Parse(canned(t), "forever", market)
	back, ok := readBack(Lua(p))
	if !ok || !same(back, p) {
		t.Fatalf("read back %+v, want %+v", back, p)
	}
}

func TestTOCCarriesTheFlavoursInterface(t *testing.T) {
	want := "## Interface: 16001\n## Title: Gold Forever Data\n## Notes: Prices written by the Gold Forever companion\n\nData.lua\n"
	if got := string(TOC(16001)); got != want {
		t.Fatalf("TOC =\n%s", got)
	}
}

// fetcher is the server: every market answers body under etag, 304 to a request holding that etag,
// or err. It records each ask and the ETag it carried.
type fetcher struct {
	body  []byte
	etag  string
	err   error
	asked []string
	sent  []string
}

func (f *fetcher) Prices(_ context.Context, game, market, etag string) ([]byte, string, error) {
	f.asked = append(f.asked, game+" "+market)
	f.sent = append(f.sent, etag)
	switch {
	case f.err != nil:
		return nil, "", f.err
	case etag != "" && etag == f.etag:
		return nil, etag, api.ErrNotModified
	}
	return f.body, f.etag, nil
}

// refresher keeps market's prices from f in saves, its ETags in state.
func refresher(f Fetcher, saves []find.Save, market, state string) *Refresher {
	return &Refresher{Fetcher: f, Saves: func() []find.Save { return saves }, Market: market, State: state, Out: io.Discard}
}

// refresh is one refresh with a fresh Refresher, as at the companion's start.
func refresh(t *testing.T, f Fetcher, saves []find.Save, market string) {
	t.Helper()
	_ = refresher(f, saves, market, filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background())
}

// tree makes a WoW install with one save per flavour, each with its interface number.
func tree(t *testing.T, flavours map[string]int) (string, []find.Save) {
	t.Helper()
	install := filepath.Join(t.TempDir(), "World of Warcraft")
	for flavour, iface := range flavours {
		dir := filepath.Join(install, flavour, "WTF", "Account", "ACC", "SavedVariables")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		save := "GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = " + strconv.Itoa(iface) + ",\n},\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte(save), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return install, find.Saves([]string{install})
}

func dataDir(install, flavour string) string {
	return filepath.Join(install, flavour, "Interface", "AddOns", "GoldForever_Data")
}

func TestRefreshWritesTheDataAddonIntoEachForeverFlavour(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_classic_": 11507})
	f := &fetcher{body: canned(t)}

	refresh(t, f, saves, market)

	if len(f.asked) != 1 || f.asked[0] != "forever "+market {
		t.Fatalf("asked %v, want one fetch of the market", f.asked)
	}
	p, _ := Parse(canned(t), "forever", market)
	data, err := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if err != nil || !bytes.Equal(data, Lua(p)) {
		t.Fatalf("Data.lua = %q, %v", data, err)
	}
	toc, err := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "GoldForever_Data.toc"))
	if err != nil || !bytes.Equal(toc, TOC(16001)) {
		t.Fatalf("toc = %q, %v", toc, err)
	}
	if _, err := os.Stat(dataDir(install, "_classic_")); !os.IsNotExist(err) {
		t.Errorf("a game the server has no prices for got a data folder: %v", err)
	}
	assertOnly(t, dataDir(install, "_forever_"), "Data.lua", "GoldForever_Data.toc")
}

func TestRefreshFetchesOnceForSeveralForeverFlavours(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	f := &fetcher{body: canned(t)}

	refresh(t, f, saves, market)

	if len(f.asked) != 1 {
		t.Errorf("asked %d times, want once", len(f.asked))
	}
	toc, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_ptr_"), "GoldForever_Data.toc"))
	if !bytes.Equal(toc, TOC(16002)) {
		t.Errorf("each flavour's TOC carries its own interface: %q", toc)
	}
}

func TestRefreshWithoutAForeverOrRetailFlavourFetchesNothing(t *testing.T) {
	_, classic := tree(t, map[string]int{"_classic_era_": 11507})
	f := &fetcher{body: canned(t)}

	refresh(t, f, classic, market)

	if len(f.asked) != 0 {
		t.Errorf("asked %v", f.asked)
	}
}

func TestRefreshWithoutAMarketWritesEveryMarketOfTheAccount(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: cannedMarkets(t)}

	refresh(t, f, saves, "")

	if strings.Join(f.asked, ",") != "forever " {
		t.Fatalf("asked %q, want one fetch of the game without a market", f.asked)
	}
	data, err := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if err != nil || !bytes.Equal(data, Lua(mustParseAccount(t, cannedMarkets(t)))) {
		t.Fatalf("Data.lua = %q, %v", data, err)
	}
}

func TestTheAccountsAnswerIsAskedForAgainUnderItsOneETag(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: cannedMarkets(t), etag: `"all-1"`}
	_ = refresher(f, saves, "", state).Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}

	err := refresher(f, saves, "", state).Refresh(context.Background())

	if err != nil || strings.Join(f.sent, " ") != ` "all-1"` {
		t.Fatalf("sent %q, err %v; want nothing, then the answer's ETag", f.sent, err)
	}
	if info, _ := os.Stat(lua); !info.ModTime().Equal(old) {
		t.Errorf("Data.lua was written on a 304")
	}
}

func TestOneMarketsETagIsNotSentForTheAccountsAnswerNorTheOtherWayRound(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	same := `"x"` // a server could tag both alike; what the file holds still differs
	_ = refresher(&fetcher{body: cannedMarkets(t), etag: same}, saves, "", state).Refresh(context.Background())

	one := &fetcher{body: canned(t), etag: same}
	_ = refresher(one, saves, market, state).Refresh(context.Background())
	all := &fetcher{body: cannedMarkets(t), etag: same}
	_ = refresher(all, saves, "", state).Refresh(context.Background())

	if one.sent[0] != "" || all.sent[0] != "" {
		t.Errorf("sent %q for one market after the account's answer, %q for the account's after one market", one.sent[0], all.sent[0])
	}
}

// account is a Forever answer of the given markets, each of n items with the widest numbers.
func account(markets []string, n int) []byte { return accountOf("forever", markets, n) }

// accountOf is game's answer of the given markets, each of n items with the widest numbers.
func accountOf(game string, markets []string, n int) []byte {
	var b strings.Builder
	b.WriteString(`{"v":1,"game":"` + game + `","at":1761148800,"markets":[`)
	for k, key := range markets {
		if k > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"market":"` + key + `","items":[`)
		for i := 1; i <= n; i++ {
			if i > 1 {
				b.WriteByte(',')
			}
			b.WriteString(`{"id":` + strconv.Itoa(i) + `,"value":9007199254740991,"minBuyout":9007199254740991,"players":9007199254740991,"scannedAt":9007199254740991}`)
		}
		b.WriteString(`]}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func TestMarketsEachWithinBudgetButTogetherOverTheFileLimitAreRefused(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	keys := []string{"forever-EU-normal-horde", "forever-EU-normal-alliance", "forever-NA-normal-horde",
		"forever-NA-normal-alliance", "forever-EU-hardcore-horde", "forever-NA-hardcore-horde"}
	body := account(keys, 2000)
	a := mustParseAccount(t, body)
	if part := len(marketPart(a.Markets[0])); part > limits["forever"].market || len(Lua(a)) <= limits["forever"].data {
		t.Fatalf("fixture: markets of %d bytes in a %d-byte file", part, len(Lua(a)))
	}
	var out bytes.Buffer
	r := refresher(&fetcher{body: body, etag: `"e1"`}, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	r.Out = &out

	err := r.Refresh(context.Background())

	if err == nil || strings.Contains(out.String(), "warning") {
		t.Fatalf("err = %v, out = %q; want a refusal and no market warned of", err, out.String())
	}
	if _, err := os.Stat(dataDir(install, "_forever_")); !os.IsNotExist(err) {
		t.Errorf("a file over the limit was written: %v", err)
	}
}

func TestAMarketOverItsBudgetAmongSeveralIsWarnedOfByName(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	var out bytes.Buffer
	body := strings.Replace(string(account([]string{"forever-EU-normal-horde"}, 3000)), `"markets":[`,
		`"markets":[{"market":"forever-EU-normal-alliance","items":[]},`, 1)
	r := refresher(&fetcher{body: []byte(body)}, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	r.Out = &out

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "warning: the prices for forever-EU-normal-horde") || strings.Contains(out.String(), "forever-EU-normal-alliance take") {
		t.Errorf("out = %q, want the Horde market warned of, the Alliance one not", out.String())
	}
}

func TestAMalformedAnswerWritesNoFile(t *testing.T) {
	for name, body := range malformed {
		install, saves := tree(t, map[string]int{"_forever_": 16001})
		refresh(t, &fetcher{body: []byte(body)}, saves, market)
		if _, err := os.Stat(dataDir(install, "_forever_")); !os.IsNotExist(err) {
			t.Errorf("%s: wrote %s", name, dataDir(install, "_forever_"))
		}
	}
}

func TestAMalformedAnswerOrAFailedFetchLeavesThePreviousFiles(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	_ = refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, state).Refresh(context.Background())
	dir := dataDir(install, "_forever_")
	before, _ := os.ReadFile(filepath.Join(dir, "Data.lua"))

	for _, f := range []*fetcher{
		{body: []byte(malformed["a float price"]), etag: `"e2"`},
		{err: errors.New("offline")},
		{err: errors.New("status 500")},
	} {
		_ = refresher(f, saves, market, state).Refresh(context.Background())
	}

	after, _ := os.ReadFile(filepath.Join(dir, "Data.lua"))
	if len(before) == 0 || !bytes.Equal(before, after) {
		t.Fatalf("the last good Data.lua changed")
	}
	assertOnly(t, dir, "Data.lua", "GoldForever_Data.toc")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())
	if f.sent[0] != `"e1"` {
		t.Errorf("after the failures the ETag sent is %q, want that of the file kept", f.sent[0])
	}
}

func TestAnUnchangedMarketCostsA304AndTheFileIsNotWritten(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}

	err := refresher(f, saves, market, state).Refresh(context.Background()) // a restart: the ETag was kept

	if err != nil || len(f.sent) != 2 || f.sent[1] != `"e1"` {
		t.Fatalf("second ask sent %q, err %v; want the file's ETag", f.sent, err)
	}
	if info, err := os.Stat(lua); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("Data.lua was written on a 304: %v, %v", info.ModTime(), err)
	}
	assertOnly(t, dataDir(install, "_forever_"), "Data.lua", "GoldForever_Data.toc")
}

func TestAChangedMarketIsWrittenAgainUnderItsNewETag(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(nil, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Fetcher = &fetcher{body: canned(t), etag: `"e1"`}
	_ = r.Refresh(context.Background())

	changed := answerWith("at", `1761300000`)
	f := &fetcher{body: []byte(changed), etag: `"e2"`}
	r.Fetcher = f
	_ = r.Refresh(context.Background())
	_ = r.Refresh(context.Background())

	p, _ := Parse([]byte(changed), "forever", market)
	data, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if !bytes.Equal(data, Lua(p)) {
		t.Fatalf("Data.lua is not the changed answer:\n%s", data)
	}
	if strings.Join(f.sent, " ") != `"e1" "e2"` {
		t.Errorf("sent %q, want the old ETag, then the new one", f.sent)
	}
}

func TestADataFileGoneIsFetchedInFull(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	if err := os.Remove(lua); err != nil {
		t.Fatal(err)
	}

	_ = refresher(f, saves, market, state).Refresh(context.Background())

	if f.sent[1] != "" {
		t.Errorf("sent %q for a missing file: a 304 would leave it missing", f.sent[1])
	}
	if _, err := os.Stat(lua); err != nil {
		t.Errorf("Data.lua not written again: %v", err)
	}
}

func TestAnotherMarketsETagIsNotSentForTheFile(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	_ = refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, state).Refresh(context.Background())
	const other = "forever-BETA-normal-alliance"
	_ = refresher(&fetcher{body: []byte(strings.Replace(string(canned(t)), market, other, 1)), etag: `"o1"`}, saves, other, state).Refresh(context.Background())

	f := &fetcher{body: canned(t), etag: `"e1"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())

	if f.sent[0] != "" {
		t.Errorf("sent %q though Data.lua now holds %s", f.sent[0], other)
	}
}

// clock is the injected timer: each wait Run asks for is reported, and fires when the test ticks.
type clock struct {
	waits chan time.Duration
	tick  chan time.Time
}

func newClock() *clock { return &clock{waits: make(chan time.Duration), tick: make(chan time.Time)} }

func (c *clock) after(d time.Duration) <-chan time.Time {
	c.waits <- d
	return c.tick
}

func (c *clock) next(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.waits:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("Run never waited")
		return 0
	}
}

func TestRunRefreshesAtStartHourlyAndAfterAnImportedUpload(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	clk, imported := newClock(), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, imported); close(done) }()

	if d := clk.next(t); d != time.Hour || len(f.asked) != 1 {
		t.Fatalf("at start: asked %d times, then waits %s", len(f.asked), d)
	}
	clk.tick <- time.Time{}
	if d := clk.next(t); d != time.Hour || len(f.asked) != 2 {
		t.Fatalf("on the hour: asked %d times, then waits %s", len(f.asked), d)
	}
	select {
	case imported <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("Run does not listen for imported uploads")
	}
	if d := clk.next(t); d != time.Hour || len(f.asked) != 3 {
		t.Fatalf("after an import: asked %d times, then waits %s", len(f.asked), d)
	}
	cancel()
	<-done
}

func TestRunAfterItsContextEndedNeitherAsksNorWrites(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: canned(t), etag: `"e1"`}
	state := filepath.Join(t.TempDir(), "prices.json")
	imported := make(chan struct{}, 1)
	imported <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	refresher(f, saves, market, state).Run(ctx, time.Hour, time.After, imported)

	if _, err := os.Stat(state); len(f.asked) != 0 || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("asked %q and the state file is %v; want no ask and no write once stopped", f.asked, err)
	}
}

func TestRunWaitsOutAFailingServerAsUploadsDo(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	r := refresher(&api.Client{BaseURL: srv.URL}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	clk := newClock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, nil); close(done) }()

	var waits []time.Duration
	for range 3 {
		waits = append(waits, clk.next(t))
		if len(waits) < 3 {
			clk.tick <- time.Time{}
		}
	}
	cancel()
	<-done
	if waits[0] != time.Minute || waits[1] != 2*time.Minute || waits[2] != 4*time.Minute {
		t.Fatalf("waits = %v, want 1m 2m 4m", waits)
	}
}

func TestRunWaitsForRetryAfterWhenRateLimited(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{err: &api.RateLimitedError{RetryAfter: 90 * time.Second}}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	clk := newClock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, nil); close(done) }()

	d := clk.next(t)
	cancel()
	<-done
	if d != 90*time.Second {
		t.Fatalf("waits %s, want Retry-After", d)
	}
}

func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Errorf("%s holds %v, want %v (no temporary files left)", dir, got, names)
	}
}

func FuzzParseAndLua(f *testing.F) {
	f.Add([]byte(answerWith("v", `1`)), market)
	f.Add([]byte(answerWith("market", `"a\"]b"`)), `a"]b`)
	for _, body := range malformed {
		f.Add([]byte(body), market)
	}
	f.Fuzz(func(t *testing.T, body []byte, key string) {
		p, err := Parse(body, "forever", key)
		if err != nil {
			return
		}
		lua := Lua(p)
		for _, b := range lua {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("byte %#x in Data.lua", b)
			}
		}
		back, ok := readBack(lua)
		if !ok || !same(back, p) {
			t.Fatalf("Data.lua does not read back as the answer:\n%s", lua)
		}
	})
}

func FuzzQuote(f *testing.F) {
	for _, s := range []string{"", market, `"]] .. os.exit() --`, "a\nb\r\x00\\\xff", "\\\"\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		q := quote(s)
		for _, b := range []byte(q) {
			if b < 0x20 || b > 0x7e {
				t.Fatalf("quote(%q) holds byte %#x", s, b)
			}
		}
		if got, ok := unquote(q); !ok || got != s {
			t.Fatalf("quote(%q) = %s reads back as %q", s, q, got)
		}
	})
}

// unquote reads a Lua string literal of the kind quote writes: "…" with \\, \" and \ddd escapes.
func unquote(q string) (string, bool) {
	if len(q) < 2 || q[0] != '"' || q[len(q)-1] != '"' {
		return "", false
	}
	var out []byte
	for i := 1; i < len(q)-1; i++ {
		c := q[i]
		switch {
		case c == '"':
			return "", false // an unescaped quote ends the string early
		case c != '\\':
			out = append(out, c)
		case i+1 < len(q)-1 && (q[i+1] == '\\' || q[i+1] == '"'):
			out = append(out, q[i+1])
			i++
		case i+3 < len(q)-1:
			n, err := strconv.Atoi(q[i+1 : i+4])
			if err != nil || n > 255 {
				return "", false
			}
			out = append(out, byte(n))
			i += 3
		default:
			return "", false
		}
	}
	return string(out), true
}

var (
	luaHeader = regexp.MustCompile(`(?s)^-- [^\n]*\nGoldForever_Prices = \{\n  v = 1,\n  at = (\d+),\n  markets = \{\n(.*)  \},\n\}\n$`)
	luaMarket = regexp.MustCompile(`^    \[("(?:[^"\\]|\\.)*")\] = \{$`)
	luaItem   = regexp.MustCompile(`^      \[(\d+)\] = \{ (\d+), (\d+), (\d+), (\d+) \},$`)
)

// readBack reads a Data.lua of the shape Lua writes back into Prices, or says it is not one.
func readBack(lua []byte) (Answer, bool) {
	m := luaHeader.FindSubmatch(lua)
	if m == nil {
		return Answer{}, false
	}
	a := Answer{At: atoi(m[1])}
	for _, line := range strings.Split(strings.TrimSuffix(string(m[2]), "\n"), "\n") {
		if line == "" || line == "    }," {
			continue
		}
		if k := luaMarket.FindStringSubmatch(line); k != nil {
			key, ok := unquote(k[1])
			if !ok {
				return Answer{}, false
			}
			a.Markets = append(a.Markets, Prices{Market: key})
			continue
		}
		f := luaItem.FindStringSubmatch(line)
		if f == nil || len(a.Markets) == 0 {
			return Answer{}, false
		}
		p := &a.Markets[len(a.Markets)-1]
		p.Items = append(p.Items, Item{atoi([]byte(f[1])), atoi([]byte(f[2])), atoi([]byte(f[3])), atoi([]byte(f[4])), atoi([]byte(f[5]))})
	}
	return a, true
}

func atoi(b []byte) int64 { n, _ := strconv.ParseInt(string(b), 10, 64); return n }

func same(a, b Answer) bool {
	if a.At != b.At || len(a.Markets) != len(b.Markets) {
		return false
	}
	for i := range a.Markets {
		if a.Markets[i].Market != b.Markets[i].Market || !slices.Equal(a.Markets[i].Items, b.Markets[i].Items) {
			return false
		}
	}
	return true
}

func TestAFlavourNotWrittenKeepsItsNewETagFromBeingSent(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	state := filepath.Join(t.TempDir(), "prices.json")
	_ = refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, state).Refresh(context.Background())
	toc := filepath.Join(dataDir(install, "_forever_ptr_"), "GoldForever_Data.toc")
	if err := os.Remove(toc); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(toc, "blocker"), 0o755); err != nil { // nothing renames over it
		t.Fatal(err)
	}
	changed := []byte(answerWith("at", `1761300000`))
	_ = refresher(&fetcher{body: changed, etag: `"e2"`}, saves, market, state).Refresh(context.Background())

	f := &fetcher{body: changed, etag: `"e2"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())

	if f.sent[0] == `"e2"` {
		t.Fatal(`sent "e2" though _forever_ptr_ still holds the older prices: a 304 would keep them`)
	}
}

// sized is an answer of n items with the widest numbers, and the Data.lua it renders.
func sized(t *testing.T, n int) ([]byte, []byte) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"v":1,"game":"forever","market":"` + market + `","at":1761148800,"items":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":` + strconv.Itoa(i) + `,"value":9007199254740991,"minBuyout":9007199254740991,"players":9007199254740991,"scannedAt":9007199254740991}`)
	}
	b.WriteString(`]}`)
	p, err := Parse([]byte(b.String()), "forever", market)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(b.String()), Lua(p)
}

func TestADataFileOverItsLimitIsRefusedAndTheLastGoodOneKept(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	_ = refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, state).Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	before, _ := os.ReadFile(lua)
	body, big := sized(t, 12000)
	if len(big) <= limits["forever"].data {
		t.Fatalf("fixture: %d bytes is not over the limit", len(big))
	}
	var out bytes.Buffer
	r := refresher(&fetcher{body: body, etag: `"e2"`}, saves, market, state)
	r.Out = &out

	err := r.Refresh(context.Background())

	size := strconv.Itoa(len(big))
	if err == nil || !strings.Contains(err.Error(), size) || !strings.Contains(out.String(), size) {
		t.Fatalf("err = %v, out = %q; want both to name the size %s", err, out.String(), size)
	}
	if after, _ := os.ReadFile(lua); !bytes.Equal(before, after) {
		t.Fatal("the last good Data.lua was replaced")
	}
	assertOnly(t, dataDir(install, "_forever_"), "Data.lua", "GoldForever_Data.toc")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	_ = refresher(f, saves, market, state).Refresh(context.Background())
	if f.sent[0] != `"e1"` {
		t.Errorf("after the refusal the ETag sent is %q, want that of the file kept", f.sent[0])
	}
}

func TestAMarketOverItsBudgetIsWrittenWithAWarning(t *testing.T) {
	for _, c := range []struct {
		items int
		warn  bool
	}{{2000, false}, {3000, true}} {
		install, saves := tree(t, map[string]int{"_forever_": 16001})
		body, lua := sized(t, c.items)
		part := len(marketPart(mustParse(t, body).Markets[0]))
		if (part > limits["forever"].market) != c.warn || len(lua) > limits["forever"].data {
			t.Fatalf("fixture: %d items render a %d-byte market in %d bytes", c.items, part, len(lua))
		}
		var out bytes.Buffer
		r := refresher(&fetcher{body: body, etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
		r.Out = &out

		err := r.Refresh(context.Background())

		data, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
		if err != nil || !bytes.Equal(data, lua) {
			t.Fatalf("%d items: err %v, Data.lua not written as rendered", c.items, err)
		}
		warned := strings.Contains(out.String(), "warning") && strings.Contains(out.String(), strconv.Itoa(part))
		if warned != c.warn {
			t.Errorf("%d items (%d bytes for the market): warned %v, want %v; out = %q", c.items, part, warned, c.warn, out.String())
		}
	}
}

func mustParse(t *testing.T, body []byte) Answer {
	t.Helper()
	p, err := Parse(body, "forever", market)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustParseAccount(t *testing.T, body []byte) Answer {
	t.Helper()
	a, err := Parse(body, "forever", "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// servers is the server answering each game with its own fetcher.
type servers map[string]*fetcher

func (g servers) Prices(ctx context.Context, game, market, etag string) ([]byte, string, error) {
	return g[game].Prices(ctx, game, market, etag)
}

const retailAnswer = `{"v":1,"game":"retail","at":1761148800,"markets":[` +
	`{"market":"retail-EU","items":[{"id":2589,"value":10,"minBuyout":9,"players":5,"scannedAt":1761140000}]},` +
	`{"market":"retail-EU-Argentdawn","items":[{"id":2589,"value":12,"minBuyout":11,"players":2,"scannedAt":1761140000}]}]}`

func TestRetailFlavoursGetTheAccountsRetailMarketsUnderTheirOwnInterface(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110002, "_ptr_": 120001})
	g := servers{"forever": {body: cannedMarkets(t)}, "retail": {body: []byte(retailAnswer)}}

	refresh(t, g, saves, "")

	if strings.Join(g["forever"].asked, ",") != "forever " || strings.Join(g["retail"].asked, ",") != "retail " {
		t.Fatalf("asked %q and %q, want one fetch per game without a market", g["forever"].asked, g["retail"].asked)
	}
	want := Lua(mustParseGame(t, []byte(retailAnswer), "retail"))
	for flavour, iface := range map[string]int{"_retail_": 110002, "_ptr_": 120001} {
		data, err := os.ReadFile(filepath.Join(dataDir(install, flavour), "Data.lua"))
		if err != nil || !bytes.Equal(data, want) {
			t.Errorf("%s Data.lua = %q, %v", flavour, data, err)
		}
		toc, _ := os.ReadFile(filepath.Join(dataDir(install, flavour), "GoldForever_Data.toc"))
		if !bytes.Equal(toc, TOC(iface)) {
			t.Errorf("%s TOC = %q, want interface %d", flavour, toc, iface)
		}
	}
	forever, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if !bytes.Equal(forever, Lua(mustParseAccount(t, cannedMarkets(t)))) {
		t.Errorf("the Forever flavour holds %q", forever)
	}
}

func TestEachGameKeepsItsOwnETag(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110002})
	state := filepath.Join(t.TempDir(), "prices.json")
	g := servers{"forever": {body: cannedMarkets(t), etag: `"f1"`}, "retail": {body: []byte(retailAnswer), etag: `"r1"`}}
	_ = refresher(g, saves, "", state).Refresh(context.Background())

	_ = refresher(g, saves, "", state).Refresh(context.Background())

	if g["forever"].sent[1] != `"f1"` || g["retail"].sent[1] != `"r1"` {
		t.Errorf("sent %q and %q, want each game's own ETag", g["forever"].sent, g["retail"].sent)
	}
}

func TestOneMarketToTryOutIsAskedForInItsOwnGameOnly(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110002})
	g := servers{"forever": {body: canned(t)}, "retail": {body: []byte(retailAnswer)}}

	refresh(t, g, saves, market)

	if strings.Join(g["forever"].asked, ",") != "forever "+market || strings.Join(g["retail"].asked, ",") != "retail " {
		t.Fatalf("asked %q and %q, want the market in Forever and the account's markets in retail", g["forever"].asked, g["retail"].asked)
	}
	if _, err := os.Stat(filepath.Join(dataDir(install, "_retail_"), "Data.lua")); err != nil {
		t.Errorf("retail not written: %v", err)
	}
}

func TestAFailingGameLeavesTheOtherWrittenAndIsReported(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110002})
	g := servers{"forever": {err: &api.RateLimitedError{RetryAfter: time.Minute}}, "retail": {body: []byte(retailAnswer)}}

	err := refresher(g, saves, "", filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background())

	var limited *api.RateLimitedError
	if !errors.As(err, &limited) {
		t.Errorf("err = %v, want Forever's rate limit for Run to wait out", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir(install, "_retail_"), "Data.lua")); err != nil {
		t.Errorf("retail not written: %v", err)
	}
}

func mustParseGame(t *testing.T, body []byte, game string) Answer {
	t.Helper()
	a, err := Parse(body, game, "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Retail's markets are realm groups of 8–13k items: its file and market limits are its own.
func TestRetailHasItsOwnFileLimitAndMarketBudget(t *testing.T) {
	for _, c := range []struct {
		name          string
		markets, n    int
		write, warned bool
	}{
		{"three markets in 1.6 MB, each over Forever's budget", 3, 6000, true, false},
		{"a market over 1 MB", 1, 12000, true, true},
		{"four such markets, over 4 MB", 4, 12000, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			install, saves := tree(t, map[string]int{"_retail_": 110207})
			keys := []string{"retail-EU", "retail-EU-Argentdawn", "retail-EU-Draenor", "retail-EU-Kazzak"}[:c.markets]
			body := accountOf("retail", keys, c.n)
			lua := Lua(mustParseGame(t, body, "retail"))
			if c.write == (len(lua) > limits["retail"].data) || len(lua) <= limits["forever"].data {
				t.Fatalf("fixture: %d bytes", len(lua))
			}
			var out bytes.Buffer
			r := refresher(servers{"retail": {body: body}}, saves, "", filepath.Join(t.TempDir(), "prices.json"))
			r.Out = &out

			err := r.Refresh(context.Background())

			data, _ := os.ReadFile(filepath.Join(dataDir(install, "_retail_"), "Data.lua"))
			if written := err == nil && bytes.Equal(data, lua); written != c.write {
				t.Errorf("written %v (err %v), want %v", written, err, c.write)
			}
			if warned := strings.Contains(out.String(), "warning: the prices for retail-EU take"); warned != c.warned {
				t.Errorf("warned %v, want %v; out = %q", warned, c.warned, out.String())
			}
		})
	}
}
