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
// domain names: Go compares names, not ports, when it decides whether a token may follow.
func routed(hosts map[string]*httptest.Server) *http.Client {
	dial := &net.Dialer{}
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			return dial.DialContext(ctx, network, hosts[host].Listener.Addr().String())
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
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://gold-forever.app"+r.URL.RequestURI(), http.StatusPermanentRedirect)
	}))
	t.Cleanup(old.Close)
	c := &Client{
		BaseURL: "http://gold-forever.evensberget.com", Token: "gf_t", Version: "v0.1.0", OS: "linux",
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
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example"+r.URL.RequestURI(), http.StatusPermanentRedirect)
	}))
	t.Cleanup(old.Close)
	c := &Client{
		BaseURL: "http://gold-forever.evensberget.com", Token: "gf_t",
		HTTP: routed(map[string]*httptest.Server{"gold-forever.evensberget.com": old, "evil.example": elsewhere}),
	}

	_, _ = c.Upload(context.Background(), []byte("GoldForeverDB = {\n}\n"))
	if strings.Contains(gotAuth, "gf_t") {
		t.Error("the token was sent to a host that is not the public site")
	}
}
