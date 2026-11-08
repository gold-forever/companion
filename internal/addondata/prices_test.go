package addondata

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
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
// testdata/check_markets.lua on testdata/markets.lua, testdata/check_shared.lua on testdata/shared.lua, testdata/check_week.lua on
// testdata/week.lua, testdata/check_units.lua on testdata/units.lua and testdata/check_supply.lua on testdata/supply.lua, which the golden tests keep equal to the writer's output.
func TestDataLoadsInLuaJIT(t *testing.T) {
	luajit, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("no luajit here: run testdata/check*.lua with luajit on the host")
	}
	for check, a := range map[string]Answer{
		"check.lua":         mustParse(t, canned(t)),
		"check_markets.lua": mustParseAccount(t, cannedMarkets(t)),
		"check_shared.lua":  withSharedAnswer(t),
		"check_week.lua":    weekAnswer(t),
		"check_units.lua":   unitsAnswer(t),
		"check_supply.lua":  supplyAnswer(t),
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

func weekAnswer(t *testing.T) Answer {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "week.json"))
	if err != nil {
		t.Fatal(err)
	}
	return mustParseGame(t, body, "retail")
}

func TestParseReadsTheWeekMeanWhereTheServerSentOne(t *testing.T) {
	a := weekAnswer(t)

	want := []Item{
		{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000, Week: 1250},
		{ID: 2592, Value: 9007199254740991, MinBuyout: 5, Players: 12, ScannedAt: 1761148799, Week: 9007199254740991},
		{ID: 2996, Value: 40, MinBuyout: 38, Players: 2, ScannedAt: 1761148000},
		{ID: 14047, Value: 0, MinBuyout: 0, Players: 3, ScannedAt: 1761100000},
	}
	if !slices.Equal(a.Markets[0].Items, want) {
		t.Fatalf("items = %+v, want %+v", a.Markets[0].Items, want)
	}
}

// One odd week mean must not cost the market its prices: it is read as no mean for that item.
func TestAnOddWeekMeanIsReadAsNone(t *testing.T) {
	for _, week := range []string{`0`, `-1`, `12.5`, `12.0`, `1e3`, `"1250"`, `null`, `true`, `{}`, `[]`, `9007199254740992`} {
		body := strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"week":`+week, 1)
		a, err := Parse([]byte(body), "forever", market)
		if err != nil {
			t.Errorf("week %s: refused: %v", week, err)
			continue
		}
		if a.Markets[0].Items[0] != (Item{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000}) {
			t.Errorf("week %s: item = %+v", week, a.Markets[0].Items[0])
		}
	}
}

func TestLuaWritesTheWeekMeanFifthAndOnlyWhereThereIsOne(t *testing.T) {
	got := Lua(weekAnswer(t))
	golden := filepath.Join("testdata", "week.lua")
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
		t.Fatalf("Lua differs from testdata/week.lua (go test -update rewrites it):\n%s", got)
	}
}

func unitsAnswer(t *testing.T) Answer {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "units.json"))
	if err != nil {
		t.Fatal(err)
	}
	return mustParseGame(t, body, "retail")
}

func TestParseReadsAThinPricesUnitsWhereTheServerSentThem(t *testing.T) {
	a := unitsAnswer(t)

	want := []Item{
		{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000, Week: 1250, Units: 3, Thin: true},
		{ID: 2592, Value: 9007199254740991, MinBuyout: 5, Players: 12, ScannedAt: 1761148799, Week: 9007199254740991},
		{ID: 2996, Value: 40, MinBuyout: 38, Players: 2, ScannedAt: 1761148000, Units: 5, Thin: true},
		{ID: 14047, Value: 0, MinBuyout: 0, Players: 3, ScannedAt: 1761100000, Units: 0, Thin: true},
	}
	if !slices.Equal(a.Markets[0].Items, want) {
		t.Fatalf("items = %+v, want %+v", a.Markets[0].Items, want)
	}
}

// One odd units count must not cost the market its prices: it is read as a price that is not thin.
func TestOddUnitsAreReadAsNone(t *testing.T) {
	for _, units := range []string{`-1`, `2.5`, `2.0`, `1e3`, `"3"`, `null`, `true`, `{}`, `[]`, `9007199254740992`} {
		body := strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"units":`+units, 1)
		a, err := Parse([]byte(body), "forever", market)
		if err != nil {
			t.Errorf("units %s: refused: %v", units, err)
			continue
		}
		if a.Markets[0].Items[0] != (Item{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000}) {
			t.Errorf("units %s: item = %+v", units, a.Markets[0].Items[0])
		}
	}
}

func TestLuaWritesAThinPricesUnitsUnderTheirKeyAndOnlyThere(t *testing.T) {
	got := Lua(unitsAnswer(t))
	golden := filepath.Join("testdata", "units.lua")
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
		t.Fatalf("Lua differs from testdata/units.lua (go test -update rewrites it):\n%s", got)
	}
}

func supplyAnswer(t *testing.T) Answer {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "supply.json"))
	if err != nil {
		t.Fatal(err)
	}
	return mustParseGame(t, body, "retail")
}

func TestParseReadsSoldAndListedWhereTheServerSentThem(t *testing.T) {
	a := supplyAnswer(t)

	want := []Item{
		{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000, Week: 1250, Units: 3, Thin: true, Sold: 42, HasSold: true, Listed: 5133, HasListed: true},
		{ID: 2592, Value: 50, MinBuyout: 5, Players: 12, ScannedAt: 1761148799},
		{ID: 2996, Value: 40, MinBuyout: 38, Players: 2, ScannedAt: 1761148000, Listed: 7, HasListed: true},
		{ID: 3356, Value: 60, MinBuyout: 55, Players: 4, ScannedAt: 1761148000, Sold: 9007199254740991, HasSold: true},
		{ID: 14047, Value: 0, MinBuyout: 0, Players: 3, ScannedAt: 1761100000, HasSold: true, HasListed: true},
	}
	if !slices.Equal(a.Markets[0].Items, want) {
		t.Fatalf("items = %+v, want %+v", a.Markets[0].Items, want)
	}
}

// One odd count must not cost the market its prices: it is read as a figure the server did not send.
func TestOddSoldAndListedAreReadAsAbsent(t *testing.T) {
	for _, field := range []string{"sold", "listed"} {
		for _, n := range []string{`-1`, `2.5`, `2.0`, `1e3`, `"3"`, `null`, `true`, `{}`, `[]`, `9007199254740992`} {
			body := strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"`+field+`":`+n, 1)
			a, err := Parse([]byte(body), "forever", market)
			if err != nil {
				t.Errorf("%s %s: refused: %v", field, n, err)
				continue
			}
			if a.Markets[0].Items[0] != (Item{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000}) {
				t.Errorf("%s %s: item = %+v", field, n, a.Markets[0].Items[0])
			}
		}
	}
}

func TestLuaWritesSoldAndListedUnderTheirKeysAndOnlyWhereSent(t *testing.T) {
	a := supplyAnswer(t)
	got := Lua(a)
	want, err := os.ReadFile(filepath.Join("testdata", "supply.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Lua differs from testdata/supply.lua:\n%s", got)
	}
	if back, ok := readBack(got); !ok || !same(back, a) {
		t.Fatalf("read back %+v, want %+v", back, a)
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

func FuzzParseAndLua(f *testing.F) {
	f.Add([]byte(answerWith("v", `1`)), market)
	f.Add([]byte(answerWith("market", `"a\"]b"`)), `a"]b`)
	f.Add([]byte(strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"week":1250`, 1)), market)
	f.Add([]byte(strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"units":3`, 1)), market)
	f.Add([]byte(strings.Replace(answerWith("v", `1`), `"scannedAt":1761140000`, `"scannedAt":1761140000,"sold":0,"listed":12`, 1)), market)
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
	luaItem   = regexp.MustCompile(`^      \[(\d+)\] = \{ (\d+), (\d+), (\d+), (\d+)(?:, ([1-9]\d*))?(?:, units = (\d+))?(?:, sold = (\d+))?(?:, listed = (\d+))? \},$`)
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
		p.Items = append(p.Items, Item{
			ID: atoi([]byte(f[1])), Value: atoi([]byte(f[2])), MinBuyout: atoi([]byte(f[3])), Players: atoi([]byte(f[4])),
			ScannedAt: atoi([]byte(f[5])), Week: atoi([]byte(f[6])), Units: atoi([]byte(f[7])), Thin: f[7] != "",
			Sold: atoi([]byte(f[8])), HasSold: f[8] != "", Listed: atoi([]byte(f[9])), HasListed: f[9] != "",
		})
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
