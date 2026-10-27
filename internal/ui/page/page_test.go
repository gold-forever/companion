package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/platform/instance"
)

const (
	secret = "0123456789abcdef"
	port   = 50123
	host   = "127.0.0.1:50123"
	origin = "http://127.0.0.1:50123"
)

type fakeApp struct {
	err         error // what AddFolder answers
	restored    string
	uninstalls  int
	linked      string
	deviceLinks int
	prices      []PriceFile
	account     string
	quits       atomic.Int32
	checks      int
	page        int // the page of the log last asked for
	updates     int
	notice      *Notice
}

func (a *fakeApp) State() State {
	return State{Version: "v0.1.0", Linked: a.linked != "", Prices: a.prices, Account: a.account, Notice: a.notice}
}
func (a *fakeApp) Link(_ context.Context, token string) error { a.linked = token; return nil }
func (a *fakeApp) Unlink(context.Context) error               { a.linked = ""; return nil }
func (a *fakeApp) StartDeviceLink(context.Context) error      { a.deviceLinks++; return nil }
func (a *fakeApp) SetSaveOn(context.Context, string, bool) error {
	return nil
}
func (a *fakeApp) AddFolder(context.Context, string) error    { return a.err }
func (a *fakeApp) SetAutostart(context.Context, bool) error   { return nil }
func (a *fakeApp) Welcomed(context.Context) error             { return nil }
func (a *fakeApp) History(page int) History                   { a.page = page; return History{Page: page} }
func (a *fakeApp) Restore(_ context.Context, id string) error { a.restored = id; return nil }
func (a *fakeApp) Uninstall(context.Context, bool) error      { a.uninstalls++; return nil }
func (a *fakeApp) Quit()                                      { a.quits.Add(1) }
func (a *fakeApp) CheckForUpdate(context.Context) error       { a.checks++; return nil }
func (a *fakeApp) Update(context.Context) error               { a.updates++; return a.err }
func (a *fakeApp) CopyBytes(id string) ([]byte, bool) {
	if id == "known" {
		return []byte("GoldForeverDB = {\n}\n"), true
	}
	return nil, false
}

func serve(app App, method, target, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+host+target, strings.NewReader(body))
	req.Host = host
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: cookieName(port), Value: secret})
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	New(app, secret, port).ServeHTTP(rec, req)
	return rec
}

func TestTheStateNeedsTheSecret(t *testing.T) {
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("with the secret: %d", rec.Code)
	}
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", noCookie); rec.Code != http.StatusForbidden {
		t.Errorf("without the secret: %d", rec.Code)
	}
	wrong := func(r *http.Request) {
		r.Header.Del("Cookie")
		r.AddCookie(&http.Cookie{Name: cookieName(port), Value: "0123456789abcdeX"})
	}
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", wrong); rec.Code != http.StatusForbidden {
		t.Errorf("with a wrong secret: %d", rec.Code)
	}
}

func TestAnotherCopyOfTheCompanionMaySendTheSecretAsAHeader(t *testing.T) {
	header := func(r *http.Request) {
		r.Header.Del("Cookie")
		r.Header.Set(instance.SecretHeader, secret)
	}
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", header); rec.Code != http.StatusOK {
		t.Errorf("the state with the secret header: %d", rec.Code)
	}
}

func TestADNSRebindingHostIsRefused(t *testing.T) {
	rebound := func(r *http.Request) { r.Host = "evil.example:50123" }
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", rebound); rec.Code != http.StatusForbidden {
		t.Errorf("a page reaching 127.0.0.1 under another name: %d", rec.Code)
	}
	localhost := func(r *http.Request) { r.Host = "localhost:50123" }
	if rec := serve(&fakeApp{}, "GET", "/api/state", "", localhost); rec.Code != http.StatusForbidden {
		t.Errorf("only the exact address the companion opened is accepted: %d", rec.Code)
	}
}

func TestAForeignOriginIsRefused(t *testing.T) {
	foreign := func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }
	app := &fakeApp{}
	if rec := serve(app, "POST", "/api/uninstall", `{"keepData":false}`, foreign); rec.Code != http.StatusForbidden {
		t.Errorf("another site asking to uninstall: %d", rec.Code)
	}
	if app.uninstalls != 0 {
		t.Error("the foreign request reached the app")
	}
	own := func(r *http.Request) { r.Header.Set("Origin", origin) }
	if rec := serve(app, "POST", "/api/uninstall", `{"keepData":false}`, own); rec.Code != http.StatusOK {
		t.Errorf("the page itself: %d", rec.Code)
	}
}

func TestTheSecretInTheLinkBecomesACookieAndLeavesTheAddressBar(t *testing.T) {
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }
	rec := serve(&fakeApp{}, "GET", "/?s="+secret, "", noCookie)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("%d to %q: the secret must leave the address bar", rec.Code, rec.Header().Get("Location"))
	}
	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Value != secret || !cookie[0].HttpOnly || cookie[0].SameSite != http.SameSiteStrictMode {
		t.Errorf("the cookie must hold the secret, HttpOnly and SameSite=Strict: %+v", cookie)
	}
	if rec := serve(&fakeApp{}, "GET", "/?s=wrong-secret-abcd", "", noCookie); rec.Code != http.StatusForbidden {
		t.Errorf("a link with a wrong secret: %d", rec.Code)
	}
}

func TestPostsMustBeSmallJSON(t *testing.T) {
	form := func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }
	if rec := serve(&fakeApp{}, "POST", "/api/link", `token=gf_x`, form); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("a form post, which any page can send without asking: %d", rec.Code)
	}
	big := `{"token":"` + strings.Repeat("x", 5000) + `"}`
	if rec := serve(&fakeApp{}, "POST", "/api/link", big, nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over the limit: %d", rec.Code)
	}
}

func TestRestoreWaitsUntilThePlayerSaysWoWIsClosed(t *testing.T) {
	app := &fakeApp{}
	if rec := serve(app, "POST", "/api/restore", `{"id":"known"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("without the confirmation: %d", rec.Code)
	}
	if app.restored != "" {
		t.Fatal("restored without the confirmation: the game would overwrite it at logout")
	}
	if rec := serve(app, "POST", "/api/restore", `{"id":"known","wowClosed":true}`, nil); rec.Code != http.StatusOK || app.restored != "known" {
		t.Errorf("with it: %d, restored %q", rec.Code, app.restored)
	}
}

func TestACopyIsShownAsPlainTextAndOnlyByItsID(t *testing.T) {
	rec := serve(&fakeApp{}, "GET", "/api/copy?id=known", "", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := serve(&fakeApp{}, "GET", "/api/copy?id=../../config.json", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("anything but a listed copy: %d", rec.Code)
	}
}

func TestEveryAnswerCarriesTheSecurityHeadersAndNoCORS(t *testing.T) {
	for _, path := range []string{"/", "/api/state", "/api/nothing"} {
		rec := serve(&fakeApp{}, "GET", path, "", func(r *http.Request) { r.Header.Set("Origin", origin) })
		h := rec.Header()
		if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: headers %v", path, h)
		}
		if h.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s answers CORS", path)
		}
	}
}

func TestThePageItselfIsServed(t *testing.T) {
	rec := serve(&fakeApp{}, "GET", "/", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Gold Forever Companion</title>") {
		t.Errorf("%d: %.200s", rec.Code, rec.Body.String())
	}
}

func TestThePageHasAPlaceForWhatEveryUploadShares(t *testing.T) {
	page := serve(&fakeApp{}, "GET", "/", "", nil).Body.String()
	script := serve(&fakeApp{}, "GET", "/app.js", "", nil).Body.String()
	if !strings.Contains(page, `id="sharing"`) || !strings.Contains(script, `$("sharing").textContent = s.sharing`) {
		t.Errorf("the page does not show the state's sharing line where the player links")
	}
}

func TestThePageSaysWhichAccountTheSavesGoToOnceItIsKnown(t *testing.T) {
	var got map[string]any
	_ = json.Unmarshal(serve(&fakeApp{}, "GET", "/api/state", "", nil).Body.Bytes(), &got)
	if _, sent := got["account"]; sent {
		t.Errorf("an account not yet known is sent: %v", got["account"])
	}
	_ = json.Unmarshal(serve(&fakeApp{account: "Goldsmith#1234"}, "GET", "/api/state", "", nil).Body.Bytes(), &got)
	if got["account"] != "Goldsmith#1234" {
		t.Errorf("account = %v", got["account"])
	}
	page := serve(&fakeApp{}, "GET", "/", "", nil).Body.String()
	script := serve(&fakeApp{}, "GET", "/app.js", "", nil).Body.String()
	if !strings.Contains(page, `id="account"`) || !strings.Contains(script, `$("account").textContent = s.account`) {
		t.Errorf("the page does not show the account where it says it is linked")
	}
}

func TestLinkingByCodeStartsInTheApp(t *testing.T) {
	app := &fakeApp{}
	if rec := serve(app, "POST", "/api/link/device", "{}", nil); rec.Code != http.StatusOK {
		t.Fatalf("starting a link: %d %s", rec.Code, rec.Body)
	}
	if app.deviceLinks != 1 {
		t.Errorf("the app started %d links, want 1", app.deviceLinks)
	}
}

func TestLinkingByCodeKeepsThePageRules(t *testing.T) {
	app := &fakeApp{}
	noSecret := serve(app, "POST", "/api/link/device", "{}", func(r *http.Request) { r.Header.Del("Cookie") })
	notJSON := serve(app, "POST", "/api/link/device", "{}", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") })
	otherSite := serve(app, "POST", "/api/link/device", "{}", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })
	if noSecret.Code != http.StatusForbidden || notJSON.Code != http.StatusUnsupportedMediaType || otherSite.Code != http.StatusForbidden {
		t.Fatalf("no secret %d, not JSON %d, another site %d", noSecret.Code, notJSON.Code, otherSite.Code)
	}
	if app.deviceLinks != 0 {
		t.Errorf("a refused request started %d links", app.deviceLinks)
	}
}

// A launch asks whether the copy its file names still runs: the real client against the real page.
func TestAnotherLaunchFindsTheRunningCopyWithoutSendingItsSecret(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	live := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: New(&fakeApp{}, secret, live)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	if !instance.Alive(t.Context(), instance.Info{Port: live, Secret: secret}) {
		t.Error("the running copy was not recognised")
	}
	if instance.Alive(t.Context(), instance.Info{Port: live, Secret: "another copy's secret"}) {
		t.Error("a copy with another secret was taken for this one")
	}
}

// A newer download asks the running copy to quit with instance.Quit before replacing it: the real
// client against the real page. In v0.1.0-rc.2 the client sent no body, the page wanted JSON, and so
// every update failed with "the running companion answered 400".
func TestANewerDownloadCanAskTheRunningCopyToQuit(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	live := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: New(&fakeApp{}, secret, live)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	if err := instance.Quit(t.Context(), instance.Info{Port: live, Secret: secret}); err != nil {
		t.Errorf("the running copy refused to quit: %v", err)
	}
}

// The page's Quit is the app's own: done by the time the page has its answer, so no goroutine is left
// behind it that nothing waits for.
func TestQuitHasQuitByTheTimeThePageHasItsAnswer(t *testing.T) {
	app := &fakeApp{}
	if rec := serve(app, "POST", "/api/quit", "{}", nil); rec.Code != http.StatusOK {
		t.Fatalf("quit: %d", rec.Code)
	}
	if n := app.quits.Load(); n != 1 {
		t.Errorf("the app was asked to quit %d times by the answer, want 1", n)
	}
}

// Unlink forgets the token on this computer only: the server keeps it live until it is revoked.
func TestUnlinkSaysTheTokenStaysLiveUntilRevoked(t *testing.T) {
	page := serve(&fakeApp{}, "GET", "/", "", nil).Body.String()
	script := serve(&fakeApp{}, "GET", "/app.js", "", nil).Body.String()
	if !strings.Contains(page, "keeps working until you revoke it") || !strings.Contains(page, `id="revoke"`) {
		t.Error("the page does not say that an unlinked token stays live")
	}
	if !strings.Contains(script, `$("revoke").href = s.site + "/me/tokens"`) {
		t.Error("the revoke link does not go to the tokens page")
	}
	if !strings.Contains(script, "an account keeps at most 20 tokens") {
		t.Error("an expired code does not mention the limit on live tokens")
	}
}

func TestTheStateCarriesEachFlavoursPriceFileForAFreshAStaleAndAFailedFetch(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	app := &fakeApp{prices: []PriceFile{
		{Flavour: "World of Warcraft › _forever_", Game: "forever", Markets: []PriceMarket{{"forever-EU-normal-horde", 3088}}, Items: 3088, Size: 146432, Fetched: &at, Outcome: "ok"},
		{Flavour: "World of Warcraft › _forever_ptr_", Game: "forever", Markets: []PriceMarket{{"forever-EU-normal-horde", 3088}}, Items: 3088, Size: 146432, Fetched: &at, Outcome: "not modified"},
		{Flavour: "World of Warcraft › _retail_", Game: "retail", Fetched: &at, Outcome: "error", Error: "server unavailable: offline"},
	}}

	rec := serve(app, "GET", "/api/state", "", nil)

	var got struct {
		Prices []map[string]any `json:"prices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Prices) != 3 {
		t.Fatalf("state = %s, %v", rec.Body, err)
	}
	fresh, _ := json.Marshal(got.Prices[0])
	want := `{"fetched":"2026-10-22T12:00:00Z","flavour":"World of Warcraft › _forever_","game":"forever","items":3088,"markets":[{"items":3088,"market":"forever-EU-normal-horde"}],"outcome":"ok","size":146432}`
	if string(fresh) != want {
		t.Errorf("a fresh file = %s\nwant %s", fresh, want)
	}
	if got.Prices[1]["outcome"] != "not modified" {
		t.Errorf("a stale file = %v", got.Prices[1])
	}
	if got.Prices[2]["outcome"] != "error" || got.Prices[2]["error"] != "server unavailable: offline" {
		t.Errorf("a failed fetch = %v", got.Prices[2])
	}
	if page := serve(app, "GET", "/", "", nil).Body.String(); !strings.Contains(page, `id="prices"`) {
		t.Error("the page has no place for the price files")
	}
}

// refusal is an app error the player can fix.
type refusal string

func (r refusal) Error() string { return string(r) }
func (refusal) PlayerError()    {}

func TestTheAppsRefusalsAreThePlayersToFixAndItsFailuresAreNot(t *testing.T) {
	for err, want := range map[error]int{
		refusal("no GoldForever.lua under /games yet"):                       http.StatusBadRequest,
		fmt.Errorf("adding: %w", refusal("no GoldForever.lua under /games")): http.StatusBadRequest,
		errors.New("disk full"):                                              http.StatusInternalServerError,
	} {
		rec := serve(&fakeApp{err: err}, "POST", "/api/folder", `{"path":"/games"}`, nil)

		var body struct{ Error string }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != want || body.Error != err.Error() {
			t.Errorf("%v: %d %q, want %d with the message", err, rec.Code, body.Error, want)
		}
	}
}

func TestTheUpdateSectionChecksAndUpdatesThroughTheApp(t *testing.T) {
	app := &fakeApp{}
	check := serve(app, "POST", "/api/update/check", "{}", nil)
	update := serve(app, "POST", "/api/update", "{}", nil)
	if check.Code != http.StatusOK || update.Code != http.StatusOK || app.checks != 1 || app.updates != 1 {
		t.Fatalf("check %d, update %d: %d checks, %d updates", check.Code, update.Code, app.checks, app.updates)
	}
	if rec := serve(app, "POST", "/api/update", "{}", func(r *http.Request) { r.Header.Del("Cookie") }); rec.Code != http.StatusForbidden || app.updates != 1 {
		t.Errorf("an update without the secret: %d, %d updates", rec.Code, app.updates)
	}
}

func TestThePageHasAnUpdateSection(t *testing.T) {
	body := serve(&fakeApp{}, "GET", "/", "", nil).Body.String()
	for _, id := range []string{`id="running"`, `id="latest"`, `id="check"`, `id="update"`, `id="update-error"`, `id="log-page"`, `id="newer"`, `id="older"`} {
		if !strings.Contains(body, id) {
			t.Errorf("the page has no %s", id)
		}
	}
}

func TestTheLogIsAskedForByPage(t *testing.T) {
	for query, want := range map[string]int{"?page=3": 3, "": 1, "?page=x": 1} {
		app := &fakeApp{}
		if rec := serve(app, "GET", "/api/history"+query, "", nil); rec.Code != http.StatusOK || app.page != want {
			t.Errorf("%q: %d, page %d", query, rec.Code, app.page)
		}
	}
}

// The banner sits on top of the page, above the header and every section, so each layout shows it:
// the first run's, the unlinked one's and the linked one's.
func TestTheUpdateBannerIsOnTopOfEveryLayout(t *testing.T) {
	body := serve(&fakeApp{}, "GET", "/", "", nil).Body.String()
	banner := strings.Index(body, `id="banner"`)
	if banner < 0 || !strings.Contains(body, `id="update-now"`) {
		t.Fatal("the page has no banner with Update now")
	}
	tag := body[strings.LastIndex(body[:banner], "<"):]
	if !strings.Contains(tag[:strings.Index(tag, ">")], " hidden") {
		t.Error("the banner shows before the page knows of an update")
	}
	for layout, section := range map[string]string{"first run": `id="welcome"`, "unlinked": `id="unlinked"`, "linked": `id="linked"`, "header": "<header>"} {
		if at := strings.Index(body, section); at < banner {
			t.Errorf("%s: the banner is not above it", layout)
		}
	}
	if strings.Count(body[:banner], "<section") != 0 || strings.Count(body[:banner], "<main") != 0 {
		t.Error("the banner is inside a section a layout can hide")
	}
}

// The banner's style lays it out without undoing its hidden, so it stays away while up to date.
func TestTheBannersStyleKeepsItHiddenWhileUpToDate(t *testing.T) {
	css := serve(&fakeApp{}, "GET", "/style.css", "", nil).Body.String()
	if !strings.Contains(css, ".banner[hidden] { display: none; }") {
		t.Error("the banner's display overrides its hidden")
	}
}

// The page's state carries the banner's update only while a newer release is known.
func TestTheStateCarriesTheBannerOnlyWhileAnUpdateIsKnown(t *testing.T) {
	for name, notice := range map[string]*Notice{"up to date": nil, "newer": {Version: "v0.2.0", URL: "https://example/v0.2.0"}} {
		var s map[string]any
		json.NewDecoder(serve(&fakeApp{notice: notice}, "GET", "/api/state", "", nil).Body).Decode(&s)
		if _, shown := s["notice"]; shown != (notice != nil) {
			t.Errorf("%s: the state's notice is %v", name, s["notice"])
		}
	}
}
