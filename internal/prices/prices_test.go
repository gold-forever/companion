package prices

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

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
	if p.At != 1761148800 || p.Market != market || len(p.Items) != 3 {
		t.Fatalf("Parse = %+v", p)
	}
	if p.Items[0] != (Item{ID: 2589, Value: 1234, MinBuyout: 1100, Players: 1, ScannedAt: 1761140000}) {
		t.Errorf("first item = %+v (items are sorted by id)", p.Items[0])
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

// LuaJIT is not in the build container; the host runs testdata/check.lua on testdata/Data.lua, which
// TestLuaIsTheContractTable keeps equal to the writer's output.
func TestDataLoadsInLuaJIT(t *testing.T) {
	luajit, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("no luajit here: run `luajit testdata/check.lua testdata/Data.lua` on the host")
	}
	p, _ := Parse(canned(t), "forever", market)
	path := filepath.Join(t.TempDir(), "Data.lua")
	if err := os.WriteFile(path, Lua(p), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(luajit, filepath.Join("testdata", "check.lua"), path).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("luajit: %v\n%s", err, out)
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

// fetcher answers every market with body, and counts the asks.
type fetcher struct {
	body  []byte
	err   error
	asked []string
}

func (f *fetcher) Prices(_ context.Context, game, market string) ([]byte, error) {
	f.asked = append(f.asked, game+" "+market)
	return f.body, f.err
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
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 120001})
	f := &fetcher{body: canned(t)}

	Refresh(context.Background(), f, saves, market, &bytes.Buffer{})

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
	if _, err := os.Stat(dataDir(install, "_retail_")); !os.IsNotExist(err) {
		t.Errorf("retail got a data folder: %v", err)
	}
	assertOnly(t, dataDir(install, "_forever_"), "Data.lua", "GoldForever_Data.toc")
}

func TestRefreshFetchesOnceForSeveralForeverFlavours(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	f := &fetcher{body: canned(t)}

	Refresh(context.Background(), f, saves, market, &bytes.Buffer{})

	if len(f.asked) != 1 {
		t.Errorf("asked %d times, want once", len(f.asked))
	}
	toc, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_ptr_"), "GoldForever_Data.toc"))
	if !bytes.Equal(toc, TOC(16002)) {
		t.Errorf("each flavour's TOC carries its own interface: %q", toc)
	}
}

func TestRefreshWithoutAMarketOrAForeverFlavourFetchesNothing(t *testing.T) {
	_, forever := tree(t, map[string]int{"_forever_": 16001})
	_, retail := tree(t, map[string]int{"_retail_": 120001})
	f := &fetcher{body: canned(t)}

	Refresh(context.Background(), f, forever, "", &bytes.Buffer{})
	Refresh(context.Background(), f, retail, market, &bytes.Buffer{})

	if len(f.asked) != 0 {
		t.Errorf("asked %v", f.asked)
	}
}

func TestAMalformedAnswerWritesNoFile(t *testing.T) {
	for name, body := range malformed {
		install, saves := tree(t, map[string]int{"_forever_": 16001})
		Refresh(context.Background(), &fetcher{body: []byte(body)}, saves, market, &bytes.Buffer{})
		if _, err := os.Stat(dataDir(install, "_forever_")); !os.IsNotExist(err) {
			t.Errorf("%s: wrote %s", name, dataDir(install, "_forever_"))
		}
	}
}

func TestAMalformedAnswerOrAFailedFetchLeavesThePreviousFiles(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	Refresh(context.Background(), &fetcher{body: canned(t)}, saves, market, &bytes.Buffer{})
	dir := dataDir(install, "_forever_")
	before, _ := os.ReadFile(filepath.Join(dir, "Data.lua"))

	Refresh(context.Background(), &fetcher{body: []byte(malformed["a float price"])}, saves, market, &bytes.Buffer{})
	Refresh(context.Background(), &fetcher{err: errors.New("offline")}, saves, market, &bytes.Buffer{})

	after, _ := os.ReadFile(filepath.Join(dir, "Data.lua"))
	if len(before) == 0 || !bytes.Equal(before, after) {
		t.Fatalf("the last good Data.lua changed")
	}
	assertOnly(t, dir, "Data.lua", "GoldForever_Data.toc")
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
	luaHeader = regexp.MustCompile(`(?s)^-- [^\n]*\nGoldForever_Prices = \{\n  v = 1,\n  at = (\d+),\n  markets = \{\n    \[("(?:[^"\\]|\\.)*")\] = \{\n(.*)    \},\n  \},\n\}\n$`)
	luaItem   = regexp.MustCompile(`^      \[(\d+)\] = \{ (\d+), (\d+), (\d+), (\d+) \},$`)
)

// readBack reads a Data.lua of the shape Lua writes back into Prices, or says it is not one.
func readBack(lua []byte) (Prices, bool) {
	m := luaHeader.FindSubmatch(lua)
	if m == nil {
		return Prices{}, false
	}
	key, ok := unquote(string(m[2]))
	if !ok {
		return Prices{}, false
	}
	p := Prices{At: atoi(m[1]), Market: key}
	for _, line := range strings.Split(strings.TrimSuffix(string(m[3]), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := luaItem.FindStringSubmatch(line)
		if f == nil {
			return Prices{}, false
		}
		p.Items = append(p.Items, Item{atoi([]byte(f[1])), atoi([]byte(f[2])), atoi([]byte(f[3])), atoi([]byte(f[4])), atoi([]byte(f[5]))})
	}
	return p, true
}

func atoi(b []byte) int64 { n, _ := strconv.ParseInt(string(b), 10, 64); return n }

func same(a, b Prices) bool {
	if a.At != b.At || a.Market != b.Market || len(a.Items) != len(b.Items) {
		return false
	}
	for i := range a.Items {
		if a.Items[i] != b.Items[i] {
			return false
		}
	}
	return true
}
