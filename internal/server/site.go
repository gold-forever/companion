package server

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// CheckSite says why the companion will not use a site address: the token goes only over https, and
// over http only to this computer (localhost, 127.0.0.0/8, ::1), for running the site locally.
func CheckSite(site string) error {
	u, err := url.Parse(site)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("%q is not a site address: it should start with https://, like https://%s", site, PublicHost)
	}
	if !secure(u) {
		return fmt.Errorf("%s is http: the companion sends your token only over https (http only to this computer, for running the site locally)", site)
	}
	return nil
}

// secure is a URL the token may go to: https, or http to this computer.
func secure(u *url.URL) bool {
	return u.Scheme == "https" || (u.Scheme == "http" && thisComputer(u.Hostname()))
}

func thisComputer(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// endpoint is the URL of path on the site, or why the site may not be called.
func (c *Client) endpoint(path string) (string, error) {
	if err := CheckSite(c.BaseURL); err != nil {
		return "", err
	}
	return strings.TrimSuffix(c.BaseURL, "/") + path, nil
}
