package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
)

// goodShared is the contract state the tests write: two lists and a profile, all printable.
const goodShared = `{"v":1,"at":1761200000,"lists":[` +
	`{"id":"L-a1_b2","edited":1761190000,"string":"GFL1:Herbs 2449 20"},` +
	`{"id":"L2","edited":1761190001,"string":"GFL1:Ore \"best\" \\ 2770 40"}],` +
	`"profiles":[{"id":"p9","edited":1761190002,"string":"GFP1:c2VsbCAyNTky"}]}`

var wantGood = Shared{
	At: 1761200000,
	Lists: []SharedItem{
		{ID: "L-a1_b2", Edited: 1761190000, String: "GFL1:Herbs 2449 20"},
		{ID: "L2", Edited: 1761190001, String: `GFL1:Ore "best" \ 2770 40`},
	},
	Profiles: []SharedItem{{ID: "p9", Edited: 1761190002, String: "GFP1:c2VsbCAyNTky"}},
}

func TestParseSharedReadsTheContractState(t *testing.T) {
	s, err := ParseShared([]byte(goodShared))
	if err != nil || !reflect.DeepEqual(s, wantGood) {
		t.Fatalf("ParseShared = %+v, %v", s, err)
	}
	s, err = ParseShared([]byte(`{"v":1,"at":0,"lists":[],"profiles":[]}`))
	if err != nil || s.At != 0 || len(s.Lists) != 0 || len(s.Profiles) != 0 {
		t.Fatalf("no lists, no profiles: %+v, %v", s, err)
	}
}

func sharedWith(field, value string) string {
	fields := map[string]string{"v": `1`, "at": `1761200000`, "lists": `[{"id":"a","edited":1,"string":"GFL1:x"}]`, "profiles": `[{"id":"b","edited":2,"string":"GFP1:y"}]`}
	var parts []string
	for _, k := range []string{"v", "at", "lists", "profiles"} {
		v, ok := fields[k]
		if k == field {
			v, ok = value, value != ""
		}
		if ok {
			parts = append(parts, `"`+k+`":`+v)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func list(entry string) string { return sharedWith("lists", "["+entry+"]") }

// entries is n entries of a kind with distinct ids.
func entries(n int, prefix string) string {
	var parts []string
	for i := range n {
		parts = append(parts, fmt.Sprintf(`{"id":"i%d","edited":1,"string":"%sx"}`, i, prefix))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

var malformedShared = map[string]string{
	"not JSON":               `{"v":1,`,
	"an array":               `[]`,
	"trailing data":          goodShared + `{}`,
	"version 2":              sharedWith("v", `2`),
	"version as a string":    sharedWith("v", `"1"`),
	"no version":             sharedWith("v", ``),
	"no at":                  sharedWith("at", ``),
	"at as a string":         sharedWith("at", `"1"`),
	"at as a float":          sharedWith("at", `1.5`),
	"at negative":            sharedWith("at", `-1`),
	"at past 2^53":           sharedWith("at", `9007199254740992`),
	"no lists":               sharedWith("lists", ``),
	"lists null":             sharedWith("lists", `null`),
	"lists an object":        sharedWith("lists", `{}`),
	"no profiles":            sharedWith("profiles", ``),
	"profiles null":          sharedWith("profiles", `null`),
	"an entry not an object": list(`"GFL1:x"`),
	"an entry null":          list(`null`),
	"no id":                  list(`{"edited":1,"string":"GFL1:x"}`),
	"an empty id":            list(`{"id":"","edited":1,"string":"GFL1:x"}`),
	"an id as a number":      list(`{"id":7,"edited":1,"string":"GFL1:x"}`),
	"an id with a quote":     list(`{"id":"a\"","edited":1,"string":"GFL1:x"}`),
	"an id with a space":     list(`{"id":"a b","edited":1,"string":"GFL1:x"}`),
	"an id with a dot":       list(`{"id":"a.b","edited":1,"string":"GFL1:x"}`),
	"an id of 65 characters": list(`{"id":"` + strings.Repeat("a", 65) + `","edited":1,"string":"GFL1:x"}`),
	"an id twice":            sharedWith("lists", `[{"id":"a","edited":1,"string":"GFL1:x"},{"id":"a","edited":2,"string":"GFL1:y"}]`),
	"no edited":              list(`{"id":"a","string":"GFL1:x"}`),
	"edited as a string":     list(`{"id":"a","edited":"1","string":"GFL1:x"}`),
	"edited as a float":      list(`{"id":"a","edited":1.0,"string":"GFL1:x"}`),
	"edited negative":        list(`{"id":"a","edited":-1,"string":"GFL1:x"}`),
	"no string":              list(`{"id":"a","edited":1}`),
	"a string as a number":   list(`{"id":"a","edited":1,"string":1}`),
	"a list as a profile":    list(`{"id":"a","edited":1,"string":"GFP1:x"}`),
	"a profile as a list":    sharedWith("profiles", `[{"id":"a","edited":1,"string":"GFL1:x"}]`),
	"a lower-case prefix":    list(`{"id":"a","edited":1,"string":"gfl1:x"}`),
	"no prefix":              list(`{"id":"a","edited":1,"string":"x"}`),
	"a newline":              list(`{"id":"a","edited":1,"string":"GFL1:x\ny"}`),
	"a NUL":                  list(`{"id":"a","edited":1,"string":"GFL1:x\u0000"}`),
	"a DEL":                  list(`{"id":"a","edited":1,"string":"GFL1:x\u007f"}`),
	"a non-ASCII letter":     list(`{"id":"a","edited":1,"string":"GFL1:Ærlig"}`),
	"51 lists":               sharedWith("lists", entries(51, "GFL1:")),
	"51 profiles":            sharedWith("profiles", entries(51, "GFP1:")),
}

func TestParseSharedRefusesAMalformedState(t *testing.T) {
	if _, err := ParseShared([]byte(sharedWith("", ""))); err != nil {
		t.Fatalf("the well-formed base state must pass: %v", err)
	}
	for name, body := range malformedShared {
		if s, err := ParseShared([]byte(body)); err == nil {
			t.Errorf("%s: accepted as %+v", name, s)
		}
	}
}

func TestParseSharedTakesFiftyOfEach(t *testing.T) {
	body := `{"v":1,"at":1,"lists":` + entries(50, "GFL1:") + `,"profiles":` + entries(50, "GFP1:") + `}`
	s, err := ParseShared([]byte(body))
	if err != nil || len(s.Lists) != 50 || len(s.Profiles) != 50 {
		t.Fatalf("50 and 50: %d lists, %d profiles, %v", len(s.Lists), len(s.Profiles), err)
	}
	id := strings.Repeat("Az09_-", 10) + "abcd"
	if _, err := ParseShared([]byte(list(`{"id":"` + id + `","edited":9007199254740991,"string":"GFL1: !~"}`))); err != nil {
		t.Fatalf("a 64-character id of every class, the largest edited and the printable edges: %v", err)
	}
}

func withSharedAnswer(t *testing.T) Answer {
	t.Helper()
	a := withDingsAnswer(t)
	a.Shared = &wantGood
	return a
}

func TestLuaWritesTheSharedListsAndProfilesBesideTheDings(t *testing.T) {
	got := Lua(withSharedAnswer(t))
	golden := filepath.Join("testdata", "shared.lua")
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
		t.Fatalf("Lua differs from testdata/shared.lua (go test -update rewrites it):\n%s", got)
	}
}

var luaSharedEntry = regexp.MustCompile(`^      \{ id = ("(?:[^"\\]|\\.)*"), edited = (\d+), string = ("(?:[^"\\]|\\.)*") \},$`)

// readShared reads the shared table of a Data.lua of the shape Lua writes back, or nil without one.
func readShared(t *testing.T, lua []byte) *Shared {
	t.Helper()
	_, rest, found := strings.Cut(string(lua), "\n  shared = {\n")
	if !found {
		return nil
	}
	block, _, found := strings.Cut(rest, "\n  },\n")
	lines := strings.Split(block, "\n")
	if !found || len(lines) < 5 || !strings.HasPrefix(lines[0], "    at = ") || lines[1] != "    lists = {" || lines[len(lines)-1] != "    }," {
		t.Fatalf("shared table out of shape:\n%s", lua)
	}
	s := &Shared{At: atoi([]byte(strings.TrimSuffix(strings.TrimPrefix(lines[0], "    at = "), ",")))}
	into := &s.Lists
	for _, line := range lines[2 : len(lines)-1] {
		if line == "    }," {
			continue
		}
		if line == "    profiles = {" {
			into = &s.Profiles
			continue
		}
		m := luaSharedEntry.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("entry line %q", line)
		}
		id, ok1 := unquote(m[1])
		str, ok2 := unquote(m[3])
		if !ok1 || !ok2 {
			t.Fatalf("entry %q does not read back", line)
		}
		*into = append(*into, SharedItem{ID: id, Edited: atoi([]byte(m[2])), String: str})
	}
	return s
}

func TestSharedLuaReadsBackAsTheState(t *testing.T) {
	if back := readShared(t, Lua(withSharedAnswer(t))); back == nil || !reflect.DeepEqual(*back, wantGood) {
		t.Fatalf("read back %+v", back)
	}
	if back := readShared(t, Lua(Answer{At: 1, Shared: &Shared{At: 3}})); back == nil || back.At != 3 || back.Lists != nil || back.Profiles != nil {
		t.Fatalf("an empty state reads back as %+v", back)
	}
}

func FuzzSharedStrings(f *testing.F) {
	for _, s := range []string{"GFL1:x", "GFL1:", `GFL1:"]] .. os.exit() --`, "GFL1:\\", "GFL1:\n", "GFL1:\x00", "GFL1:\x7f", "GFL1:Ærlig", "GFP1:x", "x"} {
		f.Add(s, "a-1_B")
	}
	f.Add("GFL1:x", `a"`)
	printable := regexp.MustCompile(`^GFL1:[ -~]*$`)
	safeID := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	f.Fuzz(func(t *testing.T, str, id string) {
		qs, _ := json.Marshal(str)
		qid, _ := json.Marshal(id)
		s, err := ParseShared([]byte(`{"v":1,"at":5,"lists":[{"id":` + string(qid) + `,"edited":7,"string":` + string(qs) + `}],"profiles":[]}`))
		if err != nil {
			return
		}
		if !printable.MatchString(str) || !safeID.MatchString(id) {
			t.Fatalf("string %q, id %q accepted", str, id)
		}
		lua := Lua(Answer{At: 1, Shared: &s})
		for _, b := range lua {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("byte %#x in Data.lua", b)
			}
		}
		if back := readShared(t, lua); back == nil || !reflect.DeepEqual(*back, s) {
			t.Fatalf("Data.lua does not read back as the state:\n%s", lua)
		}
	})
}

// sharedServer is the server's shared state: body under etag, 304 to a request holding that etag, or
// err. It records the ETag each ask carried.
type sharedServer struct {
	body string
	etag string
	err  error
	sent []string
}

func (s *sharedServer) SharedState(_ context.Context, etag string) ([]byte, string, error) {
	s.sent = append(s.sent, etag)
	switch {
	case s.err != nil:
		return nil, "", s.err
	case etag != "" && etag == s.etag:
		return nil, etag, api.ErrNotModified
	}
	return []byte(s.body), s.etag, nil
}

// withShared is the Data.lua of answer body with the shared state s.
func withShared(t *testing.T, body []byte, s *Shared) []byte {
	t.Helper()
	a := mustParse(t, body)
	a.Shared = s
	return Lua(a)
}

func TestRefreshWritesTheSharedStateBesideThePricesAndDings(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110207})
	g := servers{"forever": {body: cannedMarkets(t)}, "retail": {body: []byte(retailAnswer)}}
	r := refresher(g, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}
	s := &sharedServer{body: goodShared, etag: `"s1"`}
	r.Shared = s

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, flavour := range []string{"_forever_", "_retail_"} {
		lua, _ := os.ReadFile(filepath.Join(dataDir(install, flavour), "Data.lua"))
		if back := readShared(t, lua); back == nil || !reflect.DeepEqual(*back, wantGood) {
			t.Errorf("%s holds shared %+v", flavour, back)
		}
		if back := readDings(t, lua); back == nil || !reflect.DeepEqual(*back, wantDings) {
			t.Errorf("%s holds dings %+v", flavour, back)
		}
	}
	if len(s.sent) != 1 || s.sent[0] != "" {
		t.Errorf("shared asked with %q, want once for the account without an ETag", s.sent)
	}
}

const changedShared = `{"v":1,"at":1761210000,"lists":[],"profiles":[{"id":"p9","edited":1761209999,"string":"GFP1:bmV3"}]}`

var wantChanged = Shared{At: 1761210000, Profiles: []SharedItem{{ID: "p9", Edited: 1761209999, String: "GFP1:bmV3"}}}

func TestAChangedSharedStateRewritesDataLuaWhileThePricesAndDingsAre304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: canned(t), etag: `"e1"`}
	d := &dingsServer{body: dings, etag: `"d1"`}
	r := refresher(f, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Dings, r.Shared = d, &sharedServer{body: goodShared, etag: `"s1"`}
	_ = r.Refresh(context.Background())
	r.Shared = &sharedServer{body: changedShared, etag: `"s2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != `"e1"` || d.sent[1] != `"d1"` {
		t.Fatalf("prices asked with %q, dings with %q; want the second of each under its ETag", f.sent, d.sent)
	}
	a := mustParse(t, canned(t))
	a.Dings, a.Shared = &wantDings, &wantChanged
	if got := dataLua(t, install); !bytes.Equal(got, Lua(a)) {
		t.Fatalf("Data.lua after 304s and a changed shared state =\n%s", got)
	}
}

func TestAChangedSharedStateAfterARestartFetchesThePricesInFull(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Shared = &sharedServer{body: goodShared, etag: `"s1"`}
	_ = r.Refresh(context.Background())
	r = refresher(f, saves, market, state) // a restart: the prices it wrote are no longer in memory
	r.Shared = &sharedServer{body: changedShared, etag: `"s2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != "" {
		t.Fatalf("prices asked with %q; want the second in full, to write the new shared state", f.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, withShared(t, canned(t), &wantChanged)) {
		t.Fatalf("Data.lua =\n%s", got)
	}
}

func TestChangedPricesKeepTheSharedStateHeldWhileItIs304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	s := &sharedServer{body: goodShared, etag: `"s1"`}
	r := refresher(f, saves, market, state)
	r.Shared = s
	_ = r.Refresh(context.Background())
	changed := bytes.Replace(canned(t), []byte(`"value":1234`), []byte(`"value":4321`), 1)
	f.body, f.etag = changed, `"e2"`
	r = refresher(f, saves, market, state) // across a restart: the state held is read back
	r.Shared = s

	_ = r.Refresh(context.Background())

	if len(s.sent) != 2 || s.sent[1] != `"s1"` {
		t.Fatalf("shared asked with %q; want the second under its ETag", s.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, withShared(t, changed, &wantGood)) {
		t.Fatalf("Data.lua after new prices and a 304 shared state =\n%s", got)
	}
}

func TestAll304LeaveDataLuaAsItIs(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	d := &dingsServer{body: dings, etag: `"d1"`}
	s := &sharedServer{body: goodShared, etag: `"s1"`}
	r := refresher(f, saves, market, state)
	r.Dings, r.Shared = d, s
	_ = r.Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}
	r = refresher(f, saves, market, state)
	r.Dings, r.Shared = d, s

	err := r.Refresh(context.Background())

	if err != nil || f.sent[1] != `"e1"` || d.sent[1] != `"d1"` || s.sent[1] != `"s1"` {
		t.Fatalf("sent prices %q, dings %q, shared %q, err %v", f.sent, d.sent, s.sent, err)
	}
	if info, err := os.Stat(lua); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("Data.lua was written on three 304s: %v, %v", info.ModTime(), err)
	}
}

func TestAServerWithoutSharedStateWritesNoSharedTable(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Shared = &sharedServer{body: goodShared, etag: `"s1"`}
	_ = r.Refresh(context.Background())
	r.Shared = &sharedServer{err: api.ErrNoShared} // the server went back to a version without it

	err := r.Refresh(context.Background())

	if got := dataLua(t, install); err != nil || !bytes.Equal(got, Lua(mustParse(t, canned(t)))) || bytes.Contains(got, []byte("shared")) {
		t.Fatalf("Data.lua from a server without shared state (err %v) =\n%s", err, got)
	}
	if held := r.state(); held[sharedKey].State != nil {
		t.Errorf("the state file still holds %s", held[sharedKey].State)
	}
}

func TestAMalformedOrFailedSharedStateKeepsTheLastGoodOne(t *testing.T) {
	for name, server := range map[string]*sharedServer{
		"malformed":     {body: strings.Replace(goodShared, `"id":"L2"`, `"id":"L\"2"`, 1), etag: `"s2"`},
		"a bad string":  {body: strings.Replace(goodShared, `GFP1:`, `GFL1:`, 1), etag: `"s2"`},
		"unauthorized":  {err: api.ErrUnauthorized},
		"server failed": {err: errors.New("status 500")},
	} {
		t.Run(name, func(t *testing.T) {
			install, saves := tree(t, map[string]int{"_forever_": 16001})
			state := filepath.Join(t.TempDir(), "prices.json")
			f := &fetcher{body: canned(t), etag: `"e1"`}
			r := refresher(f, saves, market, state)
			r.Shared = &sharedServer{body: goodShared, etag: `"s1"`}
			_ = r.Refresh(context.Background())
			changed := bytes.Replace(canned(t), []byte(`"value":1234`), []byte(`"value":4321`), 1)
			f.body, f.etag = changed, `"e2"`
			r = refresher(f, saves, market, state)
			r.Shared = server

			_ = r.Refresh(context.Background())

			if got := dataLua(t, install); !bytes.Equal(got, withShared(t, changed, &wantGood)) {
				t.Fatalf("Data.lua =\n%s", got)
			}
			server.err, server.body, server.etag = nil, goodShared, `"s1"`
			_ = r.Refresh(context.Background())
			if last := server.sent[len(server.sent)-1]; last != `"s1"` {
				t.Errorf("the next ask sent %q, want the last good state's ETag", last)
			}
		})
	}
}

func TestASharedStateCountsTowardsTheGamesFileLimit(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	body, lua := sized(t, 9000)
	if len(lua) > limits["forever"].data {
		t.Fatalf("fixture: the prices alone are %d bytes, over the limit", len(lua))
	}
	r := refresher(&fetcher{body: body, etag: `"e1"`}, saves, market, state)
	r.Shared = &sharedServer{body: goodShared, etag: `"s1"`}
	_ = r.Refresh(context.Background())
	before := dataLua(t, install)
	big := `{"v":1,"at":1,"lists":` + strings.Replace(entries(50, "GFL1:"), `GFL1:x`, "GFL1:"+strings.Repeat("x", 8000), -1) + `,"profiles":[]}`
	r.Shared = &sharedServer{body: big, etag: `"s2"`}

	err := r.Refresh(context.Background())

	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v; want the file refused as over the limit", err)
	}
	if after := dataLua(t, install); !bytes.Equal(before, after) {
		t.Fatal("the last good Data.lua was replaced")
	}
}

func TestRunAsksForTheSharedStateWithEveryRefresh(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	s := &sharedServer{body: goodShared, etag: `"s1"`}
	r.Shared = s
	clk, imported := newClock(), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, imported); close(done) }()

	clk.next(t)
	clk.tick <- time.Time{}
	clk.next(t)
	imported <- struct{}{}
	clk.next(t)

	if len(s.sent) != 3 {
		t.Errorf("shared asked %d times at start, on the hour and after an import; want 3", len(s.sent))
	}
	cancel()
	<-done
}

func TestTheReportCountsTheListsAndProfilesEachFileHolds(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	now := t0
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)
	_ = r.Refresh(context.Background())
	if got := report.Files(saves)[0]; got.Shared != nil {
		t.Fatalf("a file without shared state: %+v", got.Shared)
	}
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}
	r.Shared = &sharedServer{body: goodShared, etag: `"s1"`}

	_ = r.Refresh(context.Background())

	got := report.Files(saves)[0]
	if got.Shared == nil || *got.Shared != (SharedCount{Lists: 2, Profiles: 1}) || got.Items != 3 || len(got.Markets) != 1 {
		t.Fatalf("Files = %+v, shared %+v", got, got.Shared)
	}
	if got.Dings == nil || !reflect.DeepEqual(*got.Dings, wantDings) {
		t.Fatalf("the dings beside it read as %+v", got.Dings)
	}
}
