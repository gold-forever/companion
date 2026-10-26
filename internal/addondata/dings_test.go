package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
)

const (
	me    = "Player-4620-006C0FC1"
	alt   = "Player-4620-006C0FC2"
	dings = `{"v":1,"at":1761150000,"webhook":true,"characters":{"` + alt + `":false,"` + me + `":true}}`
)

var wantDings = Dings{At: 1761150000, Webhook: true, Characters: map[string]bool{me: true, alt: false}}

func TestParseDingsReadsTheContractState(t *testing.T) {
	d, err := ParseDings([]byte(dings))
	if err != nil || !reflect.DeepEqual(d, wantDings) {
		t.Fatalf("ParseDings = %+v, %v", d, err)
	}
	d, err = ParseDings([]byte(`{"v":1,"at":0,"webhook":false,"characters":{}}`))
	if err != nil || d.At != 0 || d.Webhook || len(d.Characters) != 0 {
		t.Fatalf("no webhook, no characters: %+v, %v", d, err)
	}
}

func dingsWith(field, value string) string {
	fields := map[string]string{"v": `1`, "at": `1761150000`, "webhook": `true`, "characters": `{"` + me + `":true}`}
	var parts []string
	for _, k := range []string{"v", "at", "webhook", "characters"} {
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

var malformedDings = map[string]string{
	"not JSON":                `{"v":1,`,
	"an array":                `[]`,
	"version 2":               dingsWith("v", `2`),
	"version as a string":     dingsWith("v", `"1"`),
	"no version":              dingsWith("v", ``),
	"no at (an older server)": dingsWith("at", ``),
	"at as a string":          dingsWith("at", `"1761150000"`),
	"at as a float":           dingsWith("at", `1761150000.5`),
	"at negative":             dingsWith("at", `-1`),
	"at past 2^53":            dingsWith("at", `9007199254740992`),
	"at null":                 dingsWith("at", `null`),
	"no webhook":              dingsWith("webhook", ``),
	"webhook as a string":     dingsWith("webhook", `"true"`),
	"webhook as a number":     dingsWith("webhook", `1`),
	"webhook null":            dingsWith("webhook", `null`),
	"no characters":           dingsWith("characters", ``),
	"characters null":         dingsWith("characters", `null`),
	"characters a list":       dingsWith("characters", `["`+me+`"]`),
	"a choice as a number":    dingsWith("characters", `{"`+me+`":1}`),
	"a choice as a string":    dingsWith("characters", `{"`+me+`":"true"}`),
	"a choice null":           dingsWith("characters", `{"`+me+`":null}`),
	"a character twice":       dingsWith("characters", `{"`+me+`":true,"`+me+`":false}`),
	"trailing data":           dings + `{}`,
	"a guid with a quote":     dingsWith("characters", `{"Player-1-0A\"]":true}`),
	"a guid with a newline":   dingsWith("characters", `{"Player-1-0A\n":true}`),
	"a creature guid":         dingsWith("characters", `{"Creature-0-4620-0-12-8661-000A":true}`),
	"a lower-case prefix":     dingsWith("characters", `{"player-4620-006C0FC1":true}`),
	"no realm digits":         dingsWith("characters", `{"Player--006C0FC1":true}`),
	"no hex":                  dingsWith("characters", `{"Player-4620-":true}`),
	"not hex":                 dingsWith("characters", `{"Player-4620-006C0FCG":true}`),
	"a realm too long":        dingsWith("characters", `{"Player-12345678901-0A":true}`),
	"hex too long":            dingsWith("characters", `{"Player-1-0123456789ABCDEF0":true}`),
}

func TestParseDingsRefusesAMalformedState(t *testing.T) {
	if _, err := ParseDings([]byte(dingsWith("", ""))); err != nil {
		t.Fatalf("the well-formed base state must pass: %v", err)
	}
	for name, body := range malformedDings {
		if d, err := ParseDings([]byte(body)); err == nil {
			t.Errorf("%s: accepted as %+v", name, d)
		}
	}
}

func withDingsAnswer(t *testing.T) Answer {
	t.Helper()
	a := mustParse(t, canned(t))
	a.Dings = &wantDings
	return a
}

func TestLuaWritesTheDingsBesideAt(t *testing.T) {
	got := Lua(withDingsAnswer(t))
	golden := filepath.Join("testdata", "dings.lua")
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
		t.Fatalf("Lua differs from testdata/dings.lua (go test -update rewrites it):\n%s", got)
	}
}

var luaDingsChoice = regexp.MustCompile(`^      \[("(?:[^"\\]|\\.)*")\] = (true|false),$`)

// readDings reads the dings table of a Data.lua of the shape Lua writes back, or nil without one.
func readDings(t *testing.T, lua []byte) *Dings {
	t.Helper()
	_, rest, found := strings.Cut(string(lua), "\n  dings = {\n")
	if !found {
		return nil
	}
	block, _, found := strings.Cut(rest, "\n  },\n  markets = {\n")
	lines := strings.Split(block, "\n")
	if !found || len(lines) < 4 || !strings.HasPrefix(lines[0], "    at = ") || lines[2] != "    characters = {" || lines[len(lines)-1] != "    }," {
		t.Fatalf("dings table out of shape:\n%s", lua)
	}
	d := &Dings{Characters: map[string]bool{}}
	d.At = atoi([]byte(strings.TrimSuffix(strings.TrimPrefix(lines[0], "    at = "), ",")))
	switch lines[1] {
	case "    webhook = true,":
		d.Webhook = true
	case "    webhook = false,":
	default:
		t.Fatalf("webhook line %q", lines[1])
	}
	for _, line := range lines[3 : len(lines)-1] {
		m := luaDingsChoice.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("character line %q", line)
		}
		guid, ok := unquote(m[1])
		if !ok {
			t.Fatalf("guid %s does not read back", m[1])
		}
		d.Characters[guid] = m[2] == "true"
	}
	return d
}

func FuzzDingsGuidKeys(f *testing.F) {
	for _, s := range []string{me, "Player-1-0", `Player-1-0"]`, "Player-1-0\n", "Player-1-0\\", "Player-1-0\x00", "Player-١-0", "Player-1-0A]] .. os.exit() --"} {
		f.Add(s, true)
	}
	guid := regexp.MustCompile(`^Player-[0-9]+-[0-9A-Fa-f]+$`)
	f.Fuzz(func(t *testing.T, key string, on bool) {
		const at = 1761150000
		quoted, _ := json.Marshal(key)
		choice, _ := json.Marshal(on)
		d, err := ParseDings([]byte(`{"v":1,"at":1761150000,"webhook":true,"characters":{` + string(quoted) + `:` + string(choice) + `}}`))
		if err != nil {
			return
		}
		if !guid.MatchString(key) || d.At != at {
			t.Fatalf("guid %q accepted", key)
		}
		a := Answer{At: 1, Dings: &d}
		lua := Lua(a)
		for _, b := range lua {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("byte %#x in Data.lua", b)
			}
		}
		if back := readDings(t, lua); back == nil || !reflect.DeepEqual(*back, d) {
			t.Fatalf("Data.lua does not read back as the state:\n%s", lua)
		}
	})
}

// dingsServer is the server's dings state: body under etag, 304 to a request holding that etag, or
// err. It records the ETag each ask carried.
type dingsServer struct {
	body string
	etag string
	err  error
	sent []string
}

func (s *dingsServer) DingsState(_ context.Context, etag string) ([]byte, string, error) {
	s.sent = append(s.sent, etag)
	switch {
	case s.err != nil:
		return nil, "", s.err
	case etag != "" && etag == s.etag:
		return nil, etag, api.ErrNotModified
	}
	return []byte(s.body), s.etag, nil
}

// withDings is the Data.lua of answer body with the dings state d.
func withDings(t *testing.T, body []byte, d *Dings) []byte {
	t.Helper()
	a := mustParse(t, body)
	a.Dings = d
	return Lua(a)
}

func dataLua(t *testing.T, install string) []byte {
	t.Helper()
	lua, err := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if err != nil {
		t.Fatal(err)
	}
	return lua
}

func TestRefreshWritesTheDingsStateBesideThePrices(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	d := &dingsServer{body: dings, etag: `"d1"`}
	r.Dings = d

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := dataLua(t, install); !bytes.Equal(got, withDings(t, canned(t), &wantDings)) {
		t.Fatalf("Data.lua =\n%s", got)
	}
	if len(d.sent) != 1 || d.sent[0] != "" {
		t.Errorf("dings asked with %q, want once without an ETag", d.sent)
	}
}

func TestEveryGamesFileGetsTheAccountsDingsState(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110207})
	g := servers{"forever": {body: cannedMarkets(t)}, "retail": {body: []byte(retailAnswer)}}
	r := refresher(g, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	d := &dingsServer{body: dings, etag: `"d1"`}
	r.Dings = d

	_ = r.Refresh(context.Background())

	for _, flavour := range []string{"_forever_", "_retail_"} {
		lua, _ := os.ReadFile(filepath.Join(dataDir(install, flavour), "Data.lua"))
		if back := readDings(t, lua); back == nil || !reflect.DeepEqual(*back, wantDings) {
			t.Errorf("%s holds dings %+v", flavour, back)
		}
	}
	if len(d.sent) != 1 {
		t.Errorf("dings asked %d times in one refresh, want once for the account", len(d.sent))
	}
}

const changedDings = `{"v":1,"at":1761160000,"webhook":true,"characters":{"` + me + `":false}}`

func TestAChangedDingsStateRewritesDataLuaWhileThePricesAre304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}
	_ = r.Refresh(context.Background())
	r.Dings = &dingsServer{body: changedDings, etag: `"d2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != `"e1"` {
		t.Fatalf("prices asked with %q; want the second under the file's ETag", f.sent)
	}
	want := Dings{At: 1761160000, Webhook: true, Characters: map[string]bool{me: false}}
	if got := dataLua(t, install); !bytes.Equal(got, withDings(t, canned(t), &want)) {
		t.Fatalf("Data.lua after a 304 and a changed dings state =\n%s", got)
	}
}

func TestAChangedDingsStateAfterARestartFetchesThePricesInFull(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}
	_ = r.Refresh(context.Background())
	r = refresher(f, saves, market, state) // a restart: the prices it wrote are no longer in memory
	r.Dings = &dingsServer{body: changedDings, etag: `"d2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != "" {
		t.Fatalf("prices asked with %q; want the second in full, to write the new dings state", f.sent)
	}
	want := Dings{At: 1761160000, Webhook: true, Characters: map[string]bool{me: false}}
	if got := dataLua(t, install); !bytes.Equal(got, withDings(t, canned(t), &want)) {
		t.Fatalf("Data.lua =\n%s", got)
	}
}

func TestChangedPricesKeepTheDingsStateHeldWhileItIs304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	d := &dingsServer{body: dings, etag: `"d1"`}
	r := refresher(f, saves, market, state)
	r.Dings = d
	_ = r.Refresh(context.Background())
	changed := bytes.Replace(canned(t), []byte(`"value":1234`), []byte(`"value":4321`), 1)
	f.body, f.etag = changed, `"e2"`
	r = refresher(f, saves, market, state) // across a restart: the state held is read back
	r.Dings = d

	_ = r.Refresh(context.Background())

	if len(d.sent) != 2 || d.sent[1] != `"d1"` {
		t.Fatalf("dings asked with %q; want the second under its ETag", d.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, withDings(t, changed, &wantDings)) {
		t.Fatalf("Data.lua after new prices and a 304 dings state =\n%s", got)
	}
}

func TestBoth304LeaveDataLuaAsItIs(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	d := &dingsServer{body: dings, etag: `"d1"`}
	r := refresher(f, saves, market, state)
	r.Dings = d
	_ = r.Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}
	r = refresher(f, saves, market, state)
	r.Dings = d

	err := r.Refresh(context.Background())

	if err != nil || f.sent[1] != `"e1"` || d.sent[1] != `"d1"` {
		t.Fatalf("sent prices %q and dings %q, err %v", f.sent, d.sent, err)
	}
	if info, err := os.Stat(lua); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("Data.lua was written on two 304s: %v, %v", info.ModTime(), err)
	}
}

func TestAServerWithoutDingsStateWritesNoDingsTable(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}
	_ = r.Refresh(context.Background())
	r.Dings = &dingsServer{err: api.ErrNoDings} // the server went back to a version without it

	err := r.Refresh(context.Background())

	if got := dataLua(t, install); err != nil || !bytes.Equal(got, Lua(mustParse(t, canned(t)))) || bytes.Contains(got, []byte("dings")) {
		t.Fatalf("Data.lua from a server without dings state (err %v) =\n%s", err, got)
	}
}

func TestAStateWithoutAtAndNoneHeldWritesNoDingsTable(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Dings = &dingsServer{body: strings.Replace(dings, `"at":1761150000,`, "", 1), etag: `"d1"`}

	_ = r.Refresh(context.Background())

	if got := dataLua(t, install); !bytes.Equal(got, Lua(mustParse(t, canned(t)))) {
		t.Fatalf("Data.lua from a server sending no at =\n%s", got)
	}
}

func TestAMalformedOrFailedDingsStateKeepsTheLastGoodOne(t *testing.T) {
	for name, server := range map[string]*dingsServer{
		"malformed":     {body: strings.Replace(dings, me, `Player-1-0A\"]`, 1), etag: `"d2"`},
		"without at":    {body: strings.Replace(dings, `"at":1761150000,`, "", 1), etag: `"d2"`},
		"unauthorized":  {err: api.ErrUnauthorized},
		"server failed": {err: errors.New("status 500")},
	} {
		t.Run(name, func(t *testing.T) {
			install, saves := tree(t, map[string]int{"_forever_": 16001})
			state := filepath.Join(t.TempDir(), "prices.json")
			f := &fetcher{body: canned(t), etag: `"e1"`}
			r := refresher(f, saves, market, state)
			r.Dings = &dingsServer{body: dings, etag: `"d1"`}
			_ = r.Refresh(context.Background())
			changed := bytes.Replace(canned(t), []byte(`"value":1234`), []byte(`"value":4321`), 1)
			f.body, f.etag = changed, `"e2"`
			r = refresher(f, saves, market, state)
			r.Dings = server

			_ = r.Refresh(context.Background())

			if got := dataLua(t, install); !bytes.Equal(got, withDings(t, changed, &wantDings)) {
				t.Fatalf("Data.lua =\n%s", got)
			}
			server.err, server.body, server.etag = nil, dings, `"d1"`
			_ = r.Refresh(context.Background())
			if last := server.sent[len(server.sent)-1]; last != `"d1"` {
				t.Errorf("the next ask sent %q, want the last good state's ETag", last)
			}
		})
	}
}

func TestRunAsksForTheDingsStateWithEveryRefresh(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	d := &dingsServer{body: dings, etag: `"d1"`}
	r.Dings = d
	clk, imported := newClock(), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, imported); close(done) }()

	clk.next(t)
	clk.tick <- time.Time{}
	clk.next(t)
	imported <- struct{}{}
	clk.next(t)

	if len(d.sent) != 3 {
		t.Errorf("dings asked %d times at start, on the hour and after an import; want 3", len(d.sent))
	}
	cancel()
	<-done
}

func TestTheReportShowsTheDingsStateEachFileHolds(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	now := t0
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)
	if got := report.Files(saves)[0].Dings; got != nil {
		t.Fatalf("before a file: dings %+v", got)
	}
	_ = r.Refresh(context.Background())
	if got := report.Files(saves)[0]; got.Dings != nil || got.Items != 3 {
		t.Fatalf("a file without dings: %+v", got)
	}
	r.Dings = &dingsServer{body: dings, etag: `"d1"`}

	_ = r.Refresh(context.Background())

	got := report.Files(saves)[0]
	if got.Dings == nil || !reflect.DeepEqual(*got.Dings, wantDings) || got.Items != 3 || len(got.Markets) != 1 {
		t.Fatalf("Files = %+v, dings %+v", got, got.Dings)
	}
}
