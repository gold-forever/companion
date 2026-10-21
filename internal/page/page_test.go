package page

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magnusev/gold-forever-companion/internal/instance"
)

const (
	secret = "0123456789abcdef"
	port   = 50123
	host   = "127.0.0.1:50123"
	origin = "http://127.0.0.1:50123"
)

type fakeApp struct {
	restored   string
	uninstalls int
	linked     string
}

func (a *fakeApp) State() State                  { return State{Version: "v0.1.0", Linked: a.linked != ""} }
func (a *fakeApp) Link(token string) error       { a.linked = token; return nil }
func (a *fakeApp) Unlink() error                 { a.linked = ""; return nil }
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
	if rec := serve(&fakeApp{}, "GET", "/api/ping", "", header); rec.Code != http.StatusOK {
		t.Errorf("ping with the secret header: %d", rec.Code)
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
