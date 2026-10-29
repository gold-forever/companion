package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gold-forever/companion/internal/server"
)

// goodCharacters is the contract list the tests write: two Forever characters, one with a quote in its
// realm and letters outside ASCII, and one retail character.
const goodCharacters = `{"characters":[` +
	`{"game":"forever","realm":"Soulseeker","name":"Goldsmith"},` +
	`{"game":"retail","realm":"Argent Dawn","name":"Tradealt"},` +
	`{"game":"forever","realm":"Zul'jin \"EU\"","name":"Ærendil"}]}`

var wantCharacters = Characters{List: []Character{
	{Game: "forever", Realm: "Soulseeker", Name: "Goldsmith"},
	{Game: "retail", Realm: "Argent Dawn", Name: "Tradealt"},
	{Game: "forever", Realm: `Zul'jin "EU"`, Name: "Ærendil"},
}}

func TestParseCharactersReadsTheContractList(t *testing.T) {
	c, err := ParseCharacters([]byte(goodCharacters))
	if err != nil || !reflect.DeepEqual(c, wantCharacters) {
		t.Fatalf("ParseCharacters = %+v, %v", c, err)
	}
	c, err = ParseCharacters([]byte(`{"characters":[]}`))
	if err != nil || len(c.List) != 0 || c.Skipped != 0 {
		t.Fatalf("no characters: %+v, %v", c, err)
	}
}

func TestParseCharactersIgnoresAFieldItDoesNotKnow(t *testing.T) {
	c, err := ParseCharacters([]byte(`{"v":2,"characters":[{"game":"forever","realm":"R","name":"N","class":"MAGE"}]}`))
	if err != nil || !reflect.DeepEqual(c.List, []Character{{Game: "forever", Realm: "R", Name: "N"}}) {
		t.Fatalf("ParseCharacters = %+v, %v", c, err)
	}
}

func character(game, realm, name string) string {
	return `{"characters":[{"game":` + game + `,"realm":` + realm + `,"name":` + name + `}]}`
}

var malformedCharacters = map[string]string{
	"not JSON":             `{"characters":[`,
	"an array":             `[]`,
	"no characters":        `{}`,
	"characters null":      `{"characters":null}`,
	"characters an object": `{"characters":{}}`,
	"a character a string": `{"characters":["Goldsmith"]}`,
	"a character null":     `{"characters":[null]}`,
	"no game":              `{"characters":[{"realm":"R","name":"N"}]}`,
	"no realm":             `{"characters":[{"game":"forever","name":"N"}]}`,
	"no name":              `{"characters":[{"game":"forever","realm":"R"}]}`,
	"game a number":        character(`1`, `"R"`, `"N"`),
	"realm a number":       character(`"forever"`, `1`, `"N"`),
	"name null":            character(`"forever"`, `"R"`, `null`),
	"over the most":        `{"characters":[` + strings.TrimSuffix(strings.Repeat(`{"game":"forever","realm":"R","name":"N"},`, maxCharacters+1), ",") + `]}`,
	"trailing data":        goodCharacters + `{}`,
}

func TestParseCharactersRefusesAMalformedList(t *testing.T) {
	for name, body := range malformedCharacters {
		if c, err := ParseCharacters([]byte(body)); err == nil {
			t.Errorf("%s: accepted as %+v", name, c)
		}
	}
}

func TestParseCharactersTakesTheMost(t *testing.T) {
	var list []string
	for i := range maxCharacters {
		list = append(list, fmt.Sprintf(`{"game":"forever","realm":"R","name":"N%d"}`, i))
	}
	c, err := ParseCharacters([]byte(`{"characters":[` + strings.Join(list, ",") + `]}`))
	if err != nil || len(c.List) != maxCharacters {
		t.Fatalf("%d characters: %d read, %v", maxCharacters, len(c.List), err)
	}
}

var unreadableCharacters = map[string]string{
	"an unknown game":        character(`"classic"`, `"R"`, `"N"`),
	"an empty realm":         character(`"forever"`, `""`, `"N"`),
	"an empty name":          character(`"forever"`, `"R"`, `""`),
	"a newline in a name":    character(`"forever"`, `"R"`, `"N\n"`),
	"a NUL in a realm":       character(`"forever"`, `"R\u0000"`, `"N"`),
	"a DEL in a name":        character(`"forever"`, `"R"`, `"N\u007f"`),
	"a C1 control in a name": character(`"forever"`, `"R"`, `"N\u0085"`),
	"a realm over 64 bytes":  character(`"forever"`, `"`+strings.Repeat("r", maxNameBytes+1)+`"`, `"N"`),
	"a name over 64 bytes":   character(`"forever"`, `"R"`, `"`+strings.Repeat("é", maxNameBytes/2+1)+`"`),
}

func TestParseCharactersSkipsOnlyACharacterItCannotWrite(t *testing.T) {
	for name, one := range unreadableCharacters {
		entry := strings.TrimSuffix(strings.TrimPrefix(one, `{"characters":[`), `]}`)
		body := `{"characters":[{"game":"forever","realm":"Soulseeker","name":"Goldsmith"},` + entry + `]}`
		c, err := ParseCharacters([]byte(body))
		want := Characters{List: []Character{{Game: "forever", Realm: "Soulseeker", Name: "Goldsmith"}}, Skipped: 1}
		if err != nil || !reflect.DeepEqual(c, want) {
			t.Errorf("%s: ParseCharacters = %+v, %v; want %+v", name, c, err, want)
		}
	}
}

func TestParseCharactersKeepsACharacterNamedTwiceOnce(t *testing.T) {
	one := `{"game":"forever","realm":"R","name":"N"}`
	c, err := ParseCharacters([]byte(`{"characters":[` + one + `,{"game":"retail","realm":"R","name":"N"},` + one + `]}`))
	want := Characters{List: []Character{{Game: "forever", Realm: "R", Name: "N"}, {Game: "retail", Realm: "R", Name: "N"}}}
	if err != nil || !reflect.DeepEqual(c, want) {
		t.Fatalf("ParseCharacters = %+v, %v", c, err)
	}
}

// LuaJIT runs testdata/check_characters.lua, the addon reading the table, and testdata/check.lua, an
// addon from before it that reads only the prices, on the same file.
func TestDataWithCharactersLoadsInLuaJITForANewAndAnOlderAddon(t *testing.T) {
	luajit, err := exec.LookPath("luajit")
	if err != nil {
		t.Skip("no luajit here: run testdata/check_characters.lua and testdata/check.lua on testdata/characters.lua")
	}
	path := filepath.Join(t.TempDir(), "Data.lua")
	if err := os.WriteFile(path, Lua(withCharactersAnswer(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, check := range []string{"check_characters.lua", "check.lua"} {
		out, err := exec.Command(luajit, filepath.Join("testdata", check), path).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("luajit %s: %v\n%s", check, err, out)
		}
	}
}

func withCharactersAnswer(t *testing.T) Answer {
	t.Helper()
	a := mustParse(t, canned(t))
	a.Characters = wantCharacters.in("forever")
	return a
}

func TestLuaWritesTheGamesCharactersBesideThePrices(t *testing.T) {
	got := Lua(withCharactersAnswer(t))
	golden := filepath.Join("testdata", "characters.lua")
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
		t.Fatalf("Lua differs from testdata/characters.lua (go test -update rewrites it):\n%s", got)
	}
}

func TestEachGameGetsOnlyItsOwnCharacters(t *testing.T) {
	if got := wantCharacters.in("retail"); !reflect.DeepEqual(got, &Characters{List: []Character{wantCharacters.List[1]}}) {
		t.Errorf("retail: %+v", got)
	}
	if got := (&Characters{}).in("forever"); got == nil || got.List != nil {
		t.Errorf("a list without the game's characters is an empty one, not none: %+v", got)
	}
	if got := (*Characters)(nil).in("forever"); got != nil {
		t.Errorf("no list is none in every game: %+v", got)
	}
}

var luaCharacter = regexp.MustCompile(`^    \{ realm = ("(?:[^"\\]|\\.)*"), name = ("(?:[^"\\]|\\.)*") \},$`)

// readCharacters reads the characters table of a Data.lua of the shape Lua writes back as game's, or
// nil without one.
func readCharacters(t *testing.T, lua []byte, game string) *Characters {
	t.Helper()
	_, rest, found := strings.Cut(string(lua), "\n  characters = {\n")
	if !found {
		return nil
	}
	block, _, found := strings.Cut(rest, "  },\n")
	if !found {
		t.Fatalf("characters table out of shape:\n%s", lua)
	}
	c := &Characters{}
	for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
		if line == "" {
			continue
		}
		m := luaCharacter.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("character line %q", line)
		}
		realm, ok1 := unquote(m[1])
		name, ok2 := unquote(m[2])
		if !ok1 || !ok2 {
			t.Fatalf("character %q does not read back", line)
		}
		c.List = append(c.List, Character{Game: game, Realm: realm, Name: name})
	}
	return c
}

func TestCharactersLuaReadsBackAsTheList(t *testing.T) {
	if back := readCharacters(t, Lua(withCharactersAnswer(t)), "forever"); !reflect.DeepEqual(back, wantCharacters.in("forever")) {
		t.Fatalf("read back %+v", back)
	}
	if back := readCharacters(t, Lua(Answer{At: 1, Characters: &Characters{}}), "forever"); back == nil || back.List != nil {
		t.Fatalf("an empty list reads back as %+v", back)
	}
}

func FuzzCharacterNames(f *testing.F) {
	for _, s := range []string{"Goldsmith", "Ærendil", `"]] .. os.exit() --`, "N\\", "N\n", "N\x00", "N\x7f", "\xff", "Zul'jin", "Aggra (Português)", ""} {
		f.Add(s, "Soulseeker")
	}
	f.Fuzz(func(t *testing.T, name, realm string) {
		qn, _ := json.Marshal(name)
		qr, _ := json.Marshal(realm)
		c, err := ParseCharacters([]byte(`{"characters":[{"game":"forever","realm":` + string(qr) + `,"name":` + string(qn) + `}]}`))
		if err != nil {
			t.Fatalf("a string realm and name refused the list: %v", err)
		}
		if len(c.List)+c.Skipped != 1 {
			t.Fatalf("read as %+v", c)
		}
		for _, ch := range c.List {
			if !utf8.ValidString(ch.Name) || !utf8.ValidString(ch.Realm) || ch.Name == "" || ch.Realm == "" {
				t.Fatalf("kept %+v", ch)
			}
		}
		lua := Lua(Answer{At: 1, Characters: c.in("forever")})
		for _, b := range lua {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("byte %#x in Data.lua", b)
			}
		}
		if back := readCharacters(t, lua, "forever"); !reflect.DeepEqual(back.List, c.List) {
			t.Fatalf("Data.lua does not read back as the list:\n%s", lua)
		}
	})
}

// charactersServer is the server's list of characters: body under etag, 304 to a request holding that
// etag, or err. It records the ETag each ask carried.
type charactersServer struct {
	body string
	etag string
	err  error
	sent []string
}

func (s *charactersServer) Characters(_ context.Context, etag string) ([]byte, string, error) {
	s.sent = append(s.sent, etag)
	switch {
	case s.err != nil:
		return nil, "", s.err
	case etag != "" && etag == s.etag:
		return nil, etag, server.ErrNotModified
	}
	return []byte(s.body), s.etag, nil
}

// withCharacters is the Data.lua of answer body with the Forever characters of c.
func withCharacters(t *testing.T, body []byte, c *Characters) []byte {
	t.Helper()
	a := mustParse(t, body)
	a.Characters = c.in("forever")
	return Lua(a)
}

func TestRefreshWritesEachGamesCharactersIntoItsFiles(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_retail_": 110207})
	g := servers{"forever": {body: cannedMarkets(t)}, "retail": {body: []byte(retailAnswer)}}
	r := refresher(g, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	c := &charactersServer{body: goodCharacters, etag: `"c1"`}
	r.Characters = c

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	for flavour, game := range map[string]string{"_forever_": "forever", "_retail_": "retail"} {
		lua, _ := os.ReadFile(filepath.Join(dataDir(install, flavour), "Data.lua"))
		if back := readCharacters(t, lua, game); !reflect.DeepEqual(back, wantCharacters.in(game)) {
			t.Errorf("%s holds characters %+v", flavour, back)
		}
	}
	if len(c.sent) != 1 || c.sent[0] != "" {
		t.Errorf("characters asked with %q, want once for the account without an ETag", c.sent)
	}
}

const changedCharacters = `{"characters":[{"game":"forever","realm":"Soulseeker","name":"Newalt"}]}`

var wantChangedCharacters = Characters{List: []Character{{Game: "forever", Realm: "Soulseeker", Name: "Newalt"}}}

func TestChangedCharactersRewriteDataLuaWhileThePricesAre304(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}
	_ = r.Refresh(context.Background())
	r.Characters = &charactersServer{body: changedCharacters, etag: `"c2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != `"e1"` {
		t.Fatalf("prices asked with %q; want the second under its ETag", f.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, withCharacters(t, canned(t), &wantChangedCharacters)) {
		t.Fatalf("Data.lua after a 304 and changed characters =\n%s", got)
	}
}

func TestChangedCharactersAfterARestartFetchThePricesInFull(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}
	_ = r.Refresh(context.Background())
	r = refresher(f, saves, market, state) // a restart: the prices it wrote are no longer in memory
	r.Characters = &charactersServer{body: changedCharacters, etag: `"c2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != "" {
		t.Fatalf("prices asked with %q; want the second in full, to write the new characters", f.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, withCharacters(t, canned(t), &wantChangedCharacters)) {
		t.Fatalf("Data.lua =\n%s", got)
	}
}

func TestAChangeInAnotherGamesCharactersLeavesThisGamesFileAsItIs(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}
	_ = r.Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}
	r = refresher(f, saves, market, state)
	r.Characters = &charactersServer{body: strings.Replace(goodCharacters, "Tradealt", "Otheralt", 1), etag: `"c2"`}

	_ = r.Refresh(context.Background())

	if len(f.sent) != 2 || f.sent[1] != `"e1"` {
		t.Fatalf("prices asked with %q; want the second under its ETag", f.sent)
	}
	if info, err := os.Stat(lua); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("the Forever file was written for a retail character's change: %v, %v", info.ModTime(), err)
	}
}

func TestAll304WithCharactersLeaveDataLuaAsItIs(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	c := &charactersServer{body: goodCharacters, etag: `"c1"`}
	r := refresher(f, saves, market, state)
	r.Characters = c
	_ = r.Refresh(context.Background())
	lua := filepath.Join(dataDir(install, "_forever_"), "Data.lua")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(lua, old, old); err != nil {
		t.Fatal(err)
	}
	r = refresher(f, saves, market, state)
	r.Characters = c

	err := r.Refresh(context.Background())

	if err != nil || f.sent[1] != `"e1"` || c.sent[1] != `"c1"` {
		t.Fatalf("sent prices %q, characters %q, err %v", f.sent, c.sent, err)
	}
	if info, err := os.Stat(lua); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("Data.lua was written on two 304s: %v, %v", info.ModTime(), err)
	}
}

func TestAServerWithoutTheCharactersWritesNoCharactersTable(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	f := &fetcher{body: canned(t), etag: `"e1"`}
	r := refresher(f, saves, market, state)
	r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}
	_ = r.Refresh(context.Background())
	r.Characters = &charactersServer{err: server.ErrNoCharacters} // an older server, or one gone back to it

	err := r.Refresh(context.Background())

	if got := dataLua(t, install); err != nil || !bytes.Equal(got, Lua(mustParse(t, canned(t)))) || bytes.Contains(got, []byte("characters")) {
		t.Fatalf("Data.lua from a server without the characters (err %v) =\n%s", err, got)
	}
	if held := r.state(); held[charactersKey].State != nil {
		t.Errorf("the state file still holds %s", held[charactersKey].State)
	}
}

func TestMalformedOrFailedCharactersKeepTheLastGoodList(t *testing.T) {
	for name, server := range map[string]*charactersServer{
		"malformed":     {body: strings.Replace(goodCharacters, `"name":"Goldsmith"`, `"name":7`, 1), etag: `"c2"`},
		"unauthorized":  {err: server.ErrUnauthorized},
		"server failed": {err: errors.New("status 500")},
	} {
		t.Run(name, func(t *testing.T) {
			install, saves := tree(t, map[string]int{"_forever_": 16001})
			state := filepath.Join(t.TempDir(), "prices.json")
			f := &fetcher{body: canned(t), etag: `"e1"`}
			r := refresher(f, saves, market, state)
			r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}
			_ = r.Refresh(context.Background())
			changed := bytes.Replace(canned(t), []byte(`"value":1234`), []byte(`"value":4321`), 1)
			f.body, f.etag = changed, `"e2"`
			r = refresher(f, saves, market, state)
			r.Characters = server

			_ = r.Refresh(context.Background())

			if got := dataLua(t, install); !bytes.Equal(got, withCharacters(t, changed, &wantCharacters)) {
				t.Fatalf("Data.lua =\n%s", got)
			}
			server.err, server.body, server.etag = nil, goodCharacters, `"c1"`
			_ = r.Refresh(context.Background())
			if last := server.sent[len(server.sent)-1]; last != `"c1"` {
				t.Errorf("the next ask sent %q, want the last good list's ETag", last)
			}
		})
	}
}

func TestACharacterLeftOutIsNotedOnce(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	var log bytes.Buffer
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Out = &log
	r.Characters = &charactersServer{body: strings.Replace(goodCharacters, `"game":"retail"`, `"game":"classic"`, 1), etag: `"c1"`}

	_ = r.Refresh(context.Background())
	_ = r.Refresh(context.Background())

	if n := strings.Count(log.String(), "characters: left out 1"); n != 1 {
		t.Errorf("the skip noted %d times over two refreshes, want once:\n%s", n, log.String())
	}
}

func TestRunAsksForTheCharactersWithEveryRefresh(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	c := &charactersServer{body: goodCharacters, etag: `"c1"`}
	r.Characters = c
	clk, imported := newClock(), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, time.Hour, clk.after, imported); close(done) }()

	clk.next(t)
	clk.tick <- time.Time{}
	clk.next(t)
	imported <- struct{}{}
	clk.next(t)

	if len(c.sent) != 3 {
		t.Errorf("characters asked %d times at start, on the hour and after an import; want 3", len(c.sent))
	}
	cancel()
	<-done
}

func TestTheReportStillCountsTheMarketsBesideTheCharacters(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	now := t0
	r := refresher(&fetcher{body: canned(t), etag: `"e1"`}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)
	r.Characters = &charactersServer{body: goodCharacters, etag: `"c1"`}

	_ = r.Refresh(context.Background())

	if got := report.Files(saves)[0]; got.Items != 3 || len(got.Markets) != 1 || got.Markets[0].Items != 3 {
		t.Fatalf("Files = %+v", got)
	}
}
