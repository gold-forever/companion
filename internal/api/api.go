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
	"strconv"
	"strings"
	"time"
)

// The save's file name as the server logs it; the path it was read from is never sent.
const saveName = "GoldForever.lua"

// A 429 without Retry-After waits this long.
const defaultRetryAfter = time.Minute

// The server's answers are small JSON objects; anything longer is not one.
const maxAnswerBytes = 64 << 10

// Client talks to one Gold Forever server with one token.
type Client struct {
	// BaseURL is the site, e.g. https://gold-forever.evensberget.com.
	BaseURL string
	Token   string
	// Version and OS make the User-Agent, so the server can tell the companion's uploads apart.
	Version string
	OS      string
	// HTTP is the client used; nil means one with a two-minute timeout.
	HTTP *http.Client
}

// Answer is what the server says about an upload it took.
type Answer struct {
	Status   string `json:"status"`
	Problems int    `json:"problems"`
	Saves    int    `json:"saves"`
	Reason   string `json:"reason"`
}

// ErrUnauthorized means the token is wrong or revoked: uploads stop until the player links again.
var ErrUnauthorized = errors.New("the server does not accept this token")

// ErrTooLarge means the save is over the server's limit; sending it again changes nothing.
var ErrTooLarge = errors.New("the save is too large for the server")

// ForbiddenError means the account may not upload now (suspended, or a token without upload rights):
// uploads stop until that changes on the server.
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

// unavailableError is the server down, unreachable or failing: worth trying again later.
type unavailableError struct{ cause error }

func (e *unavailableError) Error() string { return "server unavailable: " + e.cause.Error() }
func (e *unavailableError) Unwrap() error { return e.cause }

// Retryable says whether the same upload may succeed later without anything changing on this side.
func Retryable(err error) bool {
	var limited *RateLimitedError
	var unavailable *unavailableError
	return errors.As(err, &limited) || errors.As(err, &unavailable)
}

// Upload sends one save, byte for byte, as the game wrote it.
func (c *Client) Upload(ctx context.Context, save []byte) (Answer, error) {
	url := strings.TrimSuffix(c.BaseURL, "/") + "/api/v1/saves?name=" + saveName
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(save))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.httpClient().Do(req)
	if err != nil {
		return Answer{}, &unavailableError{err}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
	return answer(res, body)
}

func answer(res *http.Response, body []byte) (Answer, error) {
	var a Answer
	_ = json.Unmarshal(body, &a)
	switch {
	case res.StatusCode == http.StatusOK:
		return a, nil
	case res.StatusCode == http.StatusUnauthorized:
		return a, ErrUnauthorized
	case res.StatusCode == http.StatusForbidden:
		return a, &ForbiddenError{reasonOf(a)}
	case res.StatusCode == http.StatusRequestEntityTooLarge:
		return a, ErrTooLarge
	case res.StatusCode == http.StatusTooManyRequests:
		return a, &RateLimitedError{retryAfter(res.Header.Get("Retry-After"))}
	case res.StatusCode >= 500:
		return a, &unavailableError{fmt.Errorf("status %d", res.StatusCode)}
	default:
		return a, &RefusedError{res.StatusCode, reasonOf(a)}
	}
}

// reasonOf is the answer's reason, else its status, which carries it on some answers.
func reasonOf(a Answer) string {
	if a.Reason != "" {
		return a.Reason
	}
	return a.Status
}

// retryAfter reads Retry-After in seconds, the only form the server sends.
func retryAfter(header string) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultRetryAfter
}

func (c *Client) userAgent() string {
	return fmt.Sprintf("gold-forever-companion/%s (%s)", c.Version, c.OS)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
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
	case http.StatusNotFound:
	default:
		err = fmt.Errorf("the version check answered %d", res.StatusCode)
	}
	return r, err
}
