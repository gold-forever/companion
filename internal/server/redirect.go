package server

import (
	"errors"
	"net/http"
)

// PublicHost is the site's name. It moved there from gold-forever.evensberget.com, which answers 308.
const PublicHost = "gold-forever.app"

// carryToken lets a redirect to the public site keep the token: Go drops Authorization when a
// redirect changes domain, which would turn every upload from a companion set to the old name into a
// 401. Only https://gold-forever.app gets it; a redirect anywhere else goes without.
func carryToken(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	auth := via[0].Header.Get("Authorization")
	if auth != "" && req.Header.Get("Authorization") == "" && req.URL.Scheme == "https" && req.URL.Hostname() == PublicHost {
		req.Header.Set("Authorization", auth)
	}
	return nil
}
