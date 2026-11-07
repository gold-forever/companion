package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// routed is a client that sends each host name to a local test server, so a redirect crosses real
// domain names: Go compares names, not ports, when it decides whether a token may follow. A key with a
// port ("gold-forever.app:80") routes that port alone, so one name can answer both https and http.
func routed(hosts map[string]*httptest.Server) *http.Client {
	dial := &net.Dialer{}
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			srv, ok := hosts[addr]
			if !ok {
				host, _, _ := net.SplitHostPort(addr)
				srv = hosts[host]
			}
			return dial.DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}}
}

// The site moved to gold-forever.app, and the old name answers 308. A companion still set to the old
// name must arrive with its save intact and its token, or every upload would answer 401.
func TestAnUploadFollowsTheSitesMoveWithItsToken(t *testing.T) {
	save := []byte("\r\nGoldForeverDB = {\r\n}\r\n")
	var gotAuth string
	var gotSave []byte
	moved := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSave, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	t.Cleanup(moved.Close)
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://gold-forever.app"+r.URL.RequestURI(), http.StatusPermanentRedirect)
	}))
	t.Cleanup(old.Close)
	c := &Client{
		BaseURL: "https://gold-forever.evensberget.com", Token: "gf_t", Version: "v0.1.0", OS: "linux",
		HTTP: routed(map[string]*httptest.Server{"gold-forever.evensberget.com": old, "gold-forever.app": moved}),
	}

	if _, err := c.Upload(context.Background(), save); err != nil {
		t.Fatalf("Upload through the move: %v", err)
	}
	if gotAuth != "Bearer gf_t" {
		t.Errorf("the token did not follow the move to gold-forever.app: %q", gotAuth)
	}
	if !bytes.Equal(gotSave, save) {
		t.Errorf("the save changed on the way: %q", gotSave)
	}
}

// The token follows a redirect only to the public site: a redirect anywhere else never carries it.
func TestARedirectElsewhereNeverCarriesTheToken(t *testing.T) {
	var gotAuth string
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"status":"imported"}`)
	}))
	t.Cleanup(elsewhere.Close)
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example"+r.URL.RequestURI(), http.StatusPermanentRedirect)
	}))
	t.Cleanup(old.Close)
	c := &Client{
		BaseURL: "https://gold-forever.evensberget.com", Token: "gf_t",
		HTTP: routed(map[string]*httptest.Server{"gold-forever.evensberget.com": old, "evil.example": elsewhere}),
	}

	_, _ = c.Upload(context.Background(), []byte("GoldForeverDB = {\n}\n"))
	if strings.Contains(gotAuth, "gf_t") {
		t.Error("the token was sent to a host that is not the public site")
	}
}

// A redirect from the site to http on the same name would carry the token in clear: Go keeps it for
// the same host whatever the scheme. It goes without.
func TestARedirectToHTTPDropsTheToken(t *testing.T) {
	var gotAuth string
	reached := false
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached, gotAuth = true, r.Header.Get("Authorization")
		io.WriteString(w, `{"status":"imported"}`)
	}))
	t.Cleanup(plain.Close)
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://gold-forever.app"+r.URL.RequestURI(), http.StatusPermanentRedirect)
	}))
	t.Cleanup(site.Close)
	c := &Client{
		BaseURL: "https://gold-forever.app", Token: "gf_t",
		HTTP: routed(map[string]*httptest.Server{"gold-forever.app:443": site, "gold-forever.app:80": plain}),
	}

	_, _ = c.Upload(context.Background(), []byte("GoldForeverDB = {\n}\n"))
	if !reached {
		t.Fatal("the redirect was not followed, so the test proves nothing")
	}
	if gotAuth != "" {
		t.Errorf("the token went to http: %q", gotAuth)
	}
}

// A redirect that stays on https keeps the token, so the site may move a path.
func TestARedirectWithinHTTPSKeepsTheToken(t *testing.T) {
	var gotAuth string
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "https://gold-forever.app/moved", http.StatusPermanentRedirect)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"status":"imported"}`)
	}))
	t.Cleanup(site.Close)
	c := &Client{
		BaseURL: "https://gold-forever.app", Token: "gf_t",
		HTTP: routed(map[string]*httptest.Server{"gold-forever.app": site}),
	}

	if _, err := c.Upload(context.Background(), []byte("GoldForeverDB = {\n}\n")); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer gf_t" {
		t.Errorf("the token did not follow a redirect within https: %q", gotAuth)
	}
}

// Running the site locally over http still works through a redirect on this computer.
func TestARedirectOnThisComputerKeepsTheToken(t *testing.T) {
	var gotAuth string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "/moved", http.StatusPermanentRedirect)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"status":"imported"}`)
	}))
	t.Cleanup(local.Close)
	c := &Client{BaseURL: local.URL, Token: "gf_t"}

	if _, err := c.Upload(context.Background(), []byte("GoldForeverDB = {\n}\n")); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer gf_t" {
		t.Errorf("the token did not follow a redirect on this computer: %q", gotAuth)
	}
}
