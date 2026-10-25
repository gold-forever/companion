// Package api is the companion's HTTP client for the Gold Forever server. Every request it can make
// is listed in PROTOCOL.md.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The save's file name as the server logs it; the path it was read from is never sent.
const saveName = "GoldForever.lua"

// A 429 without Retry-After waits this long.
const defaultRetryAfter = time.Minute

// The statuses of a 200: the save is the server's now.
const (
	statusImported        = "imported"
	statusAlreadyImported = "already imported"
)

// How long the server may take to answer once the request is sent, and the whole of a small call.
const answerTimeout = 2 * time.Minute

// The slowest uplink a save is given time for, in bytes per second: 32 MiB then takes 34 minutes.
const minUploadRate = 16 << 10

// The server's answers are small JSON objects; anything longer is not one.
const maxAnswerBytes = 64 << 10

// Client talks to one Gold Forever server with one token.
type Client struct {
	// BaseURL is the site, e.g. https://gold-forever.app.
	BaseURL string
	Token   string
	// Version and OS make the User-Agent, so the server can tell the companion's uploads apart.
	Version string
	OS      string
	// HTTP is the client used; nil means one with a two-minute timeout, and for an upload one whose
	// deadline grows with the save (uploadDeadline).
	HTTP *http.Client
	// Now and Sleep pace the wait for a link; nil means the real clock.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Answer is what the server says about an upload it took.
type Answer struct {
	Status   string `json:"status"`
	Problems int    `json:"problems"`
	Saves    int    `json:"saves"`
	Reason   string `json:"reason"`
}

// StatusImported is an upload's status when the server took new data from it; "already imported" is
// one it had.
const StatusImported = "imported"

// ErrUnauthorized means the token is wrong or revoked: uploads stop until the player links again.
var ErrUnauthorized = errors.New("the server does not accept this token")

// ErrTooLarge means the save is over the server's limit; sending it again helps only once the limit
// is raised.
var ErrTooLarge = errors.New("the save is too large for the server")

// TooLargeError is ErrTooLarge with the server's reason, which names the limit; "" when it gave none.
type TooLargeError struct{ Reason string }

func (e *TooLargeError) Error() string        { return withReason(ErrTooLarge.Error(), e.Reason).Error() }
func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// ForbiddenError means the account may not upload now (suspended, or a token without upload rights):
// uploads wait until that changes on the server.
type ForbiddenError struct{ Reason string }

func (e *ForbiddenError) Error() string { return "the server refuses uploads: " + e.Reason }

// RefusedError is a save the server read and refused, with its reason (an unknown game, a broken file).
type RefusedError struct {
	Status int
	Reason string
}

func (e *RefusedError) Error() string { return fmt.Sprintf("refused (%d): %s", e.Status, e.Reason) }

// RateLimitedError asks the companion to wait before the next upload.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

// unavailableError is the server down, unreachable or failing: worth trying again later, and not
// before retryAfter when the server named a wait (a paused server says 600 s).
type unavailableError struct {
	cause      error
	retryAfter time.Duration
}

func (e *unavailableError) Error() string { return "server unavailable: " + e.cause.Error() }
func (e *unavailableError) Unwrap() error { return e.cause }

const (
	firstWait = time.Minute
	longWait  = 30 * time.Minute
)

// NextWait doubles the wait after a failed try that is Retryable, from one minute up to thirty.
func NextWait(wait time.Duration) time.Duration {
	if wait == 0 {
		return firstWait
	}
	return min(2*wait, longWait)
}

// Retryable says whether the same upload may succeed later without anything changing on this side.
func Retryable(err error) bool {
	var limited *RateLimitedError
	var unavailable *unavailableError
	return errors.As(err, &limited) || errors.As(err, &unavailable)
}

// RetryAfter is how long the server asked a failed try to wait before the next: 0 when it named no wait.
func RetryAfter(err error) time.Duration {
	var unavailable *unavailableError
	if errors.As(err, &unavailable) {
		return unavailable.retryAfter
	}
	return 0
}

// Upload sends one save, byte for byte, as the game wrote it.
func (c *Client) Upload(ctx context.Context, save []byte) (Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, uploadDeadline(len(save)))
	defer cancel()
	url := strings.TrimSuffix(c.BaseURL, "/") + "/api/v1/saves?name=" + saveName
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(save))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.uploadClient().Do(req)
	if err != nil {
		return Answer{}, &unavailableError{cause: err}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
	return answer(res, body)
}

// answer reads the server's answer. Only the answers PROTOCOL.md names as final settle a save; any
// other, such as a proxy's 404 mid-deploy or a captive portal's 200, is tried again.
func answer(res *http.Response, body []byte) (Answer, error) {
	var a Answer
	unreadable := json.Unmarshal(body, &a) != nil
	switch {
	case res.StatusCode == http.StatusOK:
		if unreadable || (a.Status != statusImported && a.Status != statusAlreadyImported) {
			return a, &unavailableError{cause: errors.New("an answer that is not the server's")}
		}
		return a, nil
	case res.StatusCode == http.StatusUnauthorized:
		return a, ErrUnauthorized
	case res.StatusCode == http.StatusForbidden:
		return a, &ForbiddenError{reasonOf(a)}
	case res.StatusCode == http.StatusRequestEntityTooLarge:
		return a, &TooLargeError{a.Reason}
	case res.StatusCode == http.StatusTooManyRequests:
		wait, ok := retryAfter(res.Header.Get("Retry-After"))
		if !ok {
			wait = defaultRetryAfter
		}
		return a, &RateLimitedError{wait}
	case res.StatusCode == http.StatusBadRequest, res.StatusCode == http.StatusUnsupportedMediaType,
		res.StatusCode == http.StatusUnprocessableEntity:
		return a, &RefusedError{res.StatusCode, reasonOf(a)}
	case res.StatusCode >= 500:
		wait, _ := retryAfter(res.Header.Get("Retry-After"))
		return a, &unavailableError{withReason(fmt.Sprintf("status %d", res.StatusCode), a.Reason), wait}
	default:
		return a, &unavailableError{cause: fmt.Errorf("unexpected answer %d", res.StatusCode)}
	}
}

// reasonOf is the answer's reason, else its status, which carries it on some answers.
func reasonOf(a Answer) string {
	if a.Reason != "" {
		return a.Reason
	}
	return a.Status
}

// withReason is what failed, with the server's own words when it gave them.
func withReason(what, reason string) error {
	if reason == "" {
		return errors.New(what)
	}
	return fmt.Errorf("%s: %s", what, reason)
}

// retryAfter reads Retry-After in seconds, the only form the server sends; false when there is none.
func retryAfter(header string) (time.Duration, bool) {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}

func (c *Client) userAgent() string {
	return fmt.Sprintf("gold-forever-companion/%s (%s)", c.Version, c.OS)
}

// uploadDeadline bounds one upload of size bytes: sending it at minUploadRate, then the answer.
func uploadDeadline(size int) time.Duration {
	return answerTimeout + time.Duration(size/minUploadRate)*time.Second
}

// uploadTransport bounds each phase of an upload rather than the whole: a slow uplink still finishes.
var uploadTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = answerTimeout
	return t
}()

func (c *Client) uploadClient() *http.Client {
	if c.HTTP != nil {
		return c.httpClient()
	}
	return &http.Client{Transport: uploadTransport, CheckRedirect: carryToken}
}

func (c *Client) httpClient() *http.Client {
	client := http.Client{Timeout: answerTimeout}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.CheckRedirect = carryToken
	return &client
}

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

// Release is the newest companion the server knows of.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// Latest asks the server for the newest release, without the token. No release named (404) is an
// empty Release; the player downloads a newer one themselves, as the companion never updates itself.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	var r Release
	url := strings.TrimSuffix(c.BaseURL, "/") + "/api/v1/companion/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.httpClient().Do(req)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		err = json.NewDecoder(io.LimitReader(res.Body, maxAnswerBytes)).Decode(&r)
		if err == nil && r.URL != "" && !releasePage(c.BaseURL, r.URL) {
			r, err = Release{}, fmt.Errorf("the version check named a page that is not a release: %q", r.URL)
		}
	case http.StatusNotFound:
	default:
		err = fmt.Errorf("the version check answered %d", res.StatusCode)
	}
	return r, err
}
