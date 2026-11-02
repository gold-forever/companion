package server

import (
	"net/url"
	"strings"
)

// The companion's own releases, the one other place a release notice may link to.
const releasesHost, releasesPath = "github.com", "/gold-forever/companion/releases/"

// onSite says whether link is a web page of the site at base, or of the public site: the companion
// opens a link from the server with the system's URL handler, which also runs files and other schemes.
func onSite(base, link string) bool {
	b, err := url.Parse(base)
	if err != nil {
		return false
	}
	l, ok := webPage(link)
	if !ok {
		return false
	}
	return (l.Scheme == "https" && l.Host == PublicHost) || (l.Scheme == b.Scheme && strings.EqualFold(l.Host, b.Host))
}

// releasePage says whether link is one of the companion's releases, or a page of the site.
func releasePage(base, link string) bool {
	if l, ok := webPage(link); ok && l.Scheme == "https" && l.Host == releasesHost && strings.HasPrefix(l.Path, releasesPath) {
		return true
	}
	return onSite(base, link)
}

// webPage parses an absolute http or https address with no user in it.
func webPage(link string) (*url.URL, bool) {
	l, err := url.Parse(link)
	if err != nil || (l.Scheme != "https" && l.Scheme != "http") || l.User != nil || l.Host == "" {
		return nil, false
	}
	return l, true
}
