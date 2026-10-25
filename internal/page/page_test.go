package page

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/instance"
)

const (
	secret = "0123456789abcdef"
	port   = 50123
	host   = "127.0.0.1:50123"
	origin = "http://127.0.0.1:50123"
)

type fakeApp struct {
	restored    string
	uninstalls  int
	linked      string
	deviceLinks int
	prices      []PriceFile
}

func (a *fakeApp) State() State {
	return State{Version: "v0.1.0", Linked: a.linked != "", Prices: a.prices}
}
func (a *fakeApp) Link(token string) error       { a.linked = token; return nil }
func (a *fakeApp) Unlink() error                 { a.linked = ""; return nil }
func (a *fakeApp) StartDeviceLink() error        { a.deviceLinks++; return nil }
func (a *fakeApp) SetSaveOn(string, bool) error  { return nil }
func (a *fakeApp) AddFolder(string) error        { return nil }
func (a *fakeApp) SetAutostart(bool) error       { return nil }
func (a *fakeApp) Welcomed() error               { return nil }
func (a *fakeApp) History() History              { return History{} }
func (a *fakeApp) Restore(id string) error       { a.restored = id; return nil }
func (a *fakeApp) Uninstall(keepData bool) error { a.uninstalls++; return nil }
func (a *fakeApp) Quit()                         {}
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

	if !instance.Alive(instance.Info{Port: live, Secret: secret}) {
		t.Error("the running copy was not recognised")
	}
	if instance.Alive(instance.Info{Port: live, Secret: "another copy's secret"}) {
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

	if err := instance.Quit(instance.Info{Port: live, Secret: secret}); err != nil {
		t.Errorf("the running copy refused to quit: %v", err)
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
