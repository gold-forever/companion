package server

import (
	"context"
	"strings"
	"testing"
)

// The token goes only over https; http only to this computer, for running the site locally.
func TestASiteAddressIsHTTPSOrThisComputer(t *testing.T) {
	allowed := []string{
		"https://gold-forever.app",
		"https://gold-forever.app/",
		"HTTPS://gold-forever.app",
		"https://192.0.2.7:8443",
		"http://localhost:8080",
		"http://LOCALHOST",
		"http://127.0.0.1:8080",
		"http://127.1.2.3",
		"http://[::1]:8080",
	}
	for _, site := range allowed {
		if err := CheckSite(site); err != nil {
			t.Errorf("CheckSite(%q) = %v, want allowed", site, err)
		}
	}
	refused := map[string]string{
		"http://gold-forever.app":       "https",
		"http://192.0.2.7":              "https",
		"http://[2001:db8::1]":          "https",
		"http://localhost.evil.example": "https",
		"http://128.0.0.1":              "https",
		"gold-forever.app":              "https://",
		"ftp://gold-forever.app":        "https://",
		"https://":                      "https://",
		"":                              "https://",
	}
	for site, says := range refused {
		err := CheckSite(site)
		if err == nil {
			t.Errorf("CheckSite(%q) allowed it", site)
			continue
		}
		if !strings.Contains(err.Error(), says) {
			t.Errorf("CheckSite(%q) = %q, want it to mention %q", site, err, says)
		}
	}
}

// A client set to an http site elsewhere sends nothing there, the token least of all.
func TestAClientRefusesToCallAnHTTPSiteElsewhere(t *testing.T) {
	c := &Client{BaseURL: "http://gold-forever.app", Token: "gf_t"}

	if _, err := c.Upload(context.Background(), []byte("GoldForeverDB = {}")); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("Upload = %v, want refused for not being https", err)
	}
	if _, _, err := c.Prices(context.Background(), "forever", "", ""); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("Prices = %v, want refused for not being https", err)
	}
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("Latest = %v, want refused for not being https", err)
	}
	if _, err := c.StartLink(context.Background(), "pc"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("StartLink = %v, want refused for not being https", err)
	}
}
