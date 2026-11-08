package addondata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/server"
)

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
		return nil, etag, server.ErrNotModified
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

func dataLua(t *testing.T, install string) []byte {
	t.Helper()
	lua, err := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	if err != nil {
		t.Fatal(err)
	}
	return lua
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
	body := account(keys, 4000)
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
	body := strings.Replace(string(account([]string{"forever-EU-normal-horde"}, 5000)), `"markets":[`,
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
	r := refresher(&server.Client{BaseURL: srv.URL}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
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
	r := refresher(&fetcher{err: &server.RateLimitedError{RetryAfter: 90 * time.Second}}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
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
	body, big := sized(t, 24000)
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
	}{{4000, false}, {5000, true}} {
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

// suppliedMarkets is n items of each market with a week mean, sold and listed of the given digits, and
// units when thin: the shape the limits were measured on.
func suppliedMarkets(keys []string, n int, firstID, soldTo, listedTo int64, thin bool) Answer {
	a := Answer{At: 1761148800}
	for _, key := range keys {
		p := Prices{Market: key}
		for i := range int64(n) {
			it := Item{ID: firstID + i, Value: 123456, MinBuyout: 123456, Players: 10, ScannedAt: 1761140000, Week: 123456,
				Sold: soldTo, HasSold: true, Listed: listedTo, HasListed: true}
			if thin {
				it.Units, it.Thin = 5, true
			}
			p.Items = append(p.Items, it)
		}
		a.Markets = append(a.Markets, p)
	}
	return a
}

// A warning on every refresh or a refused file would be for nothing: the markets the limits were set
// for fit them with every field at its widest likely size.
func TestTheLimitsHoldMarketsWithSoldAndListed(t *testing.T) {
	forever := []string{"forever-EU-normal-horde", "forever-EU-normal-alliance", "forever-NA-normal-horde", "forever-NA-normal-alliance"}
	fourMarkets := suppliedMarkets(forever, 3012, 10000, 9999, 99999, false)
	if size := len(Lua(fourMarkets)); size > limits["forever"].data {
		t.Errorf("four Forever markets of 3,012 items take %d bytes, over the %d-byte file limit", size, limits["forever"].data)
	}
	big := suppliedMarkets(forever[:1], 4000, 10000, 9999, 99999, false)
	if part := len(marketPart(big.Markets[0])); part > limits["forever"].market {
		t.Errorf("a Forever market of 4,000 items takes %d bytes, over the %d-byte budget", part, limits["forever"].market)
	}
	realmGroup := suppliedMarkets([]string{"retail-EU-Draenor"}, 13000, 100000, 9999, 99999, true)
	if part := len(marketPart(realmGroup.Markets[0])); part > limits["retail"].market {
		t.Errorf("a retail market of 13,000 items takes %d bytes, over the %d-byte budget", part, limits["retail"].market)
	}
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
	g := servers{"forever": {err: &server.RateLimitedError{RetryAfter: time.Minute}}, "retail": {body: []byte(retailAnswer)}}

	err := refresher(g, saves, "", filepath.Join(t.TempDir(), "prices.json")).Refresh(context.Background())

	var limited *server.RateLimitedError
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
		{"three markets in 2.2 MB, each over Forever's budget", 3, 8000, true, false},
		{"a market over its 1.25 MiB", 1, 24000, true, true},
		{"four markets over budget, over 4 MiB", 4, 15000, false, true},
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

func TestTwoAccountsInOneFlavourGetTheirPriceFileWrittenOnce(t *testing.T) {
	install, _ := tree(t, map[string]int{"_forever_": 16001})
	second := filepath.Join(install, "_forever_", "WTF", "Account", "987654321#1", "SavedVariables")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	save := "GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n"
	if err := os.WriteFile(filepath.Join(second, "GoldForever.lua"), []byte(save), 0o644); err != nil {
		t.Fatal(err)
	}
	saves := find.Saves([]string{install})
	if len(saves) != 2 {
		t.Fatalf("found %d saves, want both accounts'", len(saves))
	}
	var out strings.Builder
	r := refresher(&fetcher{body: canned(t)}, saves, market, filepath.Join(t.TempDir(), "prices.json"))
	r.Out = &out

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if n := strings.Count(out.String(), "written into"); n != 1 {
		t.Fatalf("the price file was written %d times, want once for the install:\n%s", n, out.String())
	}
	assertOnly(t, dataDir(install, "_forever_"), "Data.lua", "GoldForever_Data.toc")
}

func TestAGameReadingThePriceFileWhileItIsWrittenNeverSeesHalfAFile(t *testing.T) {
	flavour := filepath.Join(t.TempDir(), "_forever_")
	versions := [][]byte{bytes.Repeat([]byte("a"), 4<<20), bytes.Repeat([]byte("b"), 4<<20)}
	if err := Write(flavour, 16001, versions[0]); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var torn, missing, reads int
	go func() {
		defer close(done)
		for i := range 20 {
			if err := Write(flavour, 16001, versions[(i+1)%2]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		data, err := os.ReadFile(filepath.Join(addonDir(flavour), "Data.lua"))
		reads++
		if err != nil {
			missing++
			continue
		}
		if !bytes.Equal(data, versions[0]) && !bytes.Equal(data, versions[1]) {
			torn++
		}
	}
	if torn > 0 || missing > 0 {
		t.Fatalf("of %d reads, %d saw a half-written price file and %d none", reads, torn, missing)
	}
}
