package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Linking the app to an account without copying a token (RFC 8628, as PROTOCOL.md describes): the
// app asks for a code, the player approves it on the site, and the app collects the token once.

// The name the server shows the player next to the code.
const clientName = "gold-forever-companion"

// What the server is told when the computer has no usable name; it only labels the token.
const unnamedComputer = "this computer"

// RFC 8628, 3.5: after slow_down, every later poll waits this much longer.
const slowDownStep = 5 * time.Second

// The poll interval when the server names none (RFC 8628's default).
const defaultInterval = 5 * time.Second

// DeviceCode is the server's answer to a link request.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"` // the app's secret for polling; never shown
	UserCode                string `json:"user_code"`   // what the player checks on the site, ABCD-EFGH
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"` // the page with the code filled in
	ExpiresIn               int    `json:"expires_in"`                // seconds
	Interval                int    `json:"interval"`                  // seconds between polls
}

// ErrLinkDenied means the player said no on the site.
var ErrLinkDenied = errors.New("the link was denied on the site")

// ErrLinkExpired means the code ran out (10 minutes) before anyone approved it.
var ErrLinkExpired = errors.New("the code expired before it was approved")

// StartLink asks the server for a code that links this computer, named hostname.
func (c *Client) StartLink(ctx context.Context, hostname string) (DeviceCode, error) {
	if strings.TrimSpace(hostname) == "" {
		hostname = unnamedComputer
	}
	var code DeviceCode
	res, body, err := c.postJSON(ctx, "/api/v1/device/code", map[string]string{"client": clientName, "hostname": hostname})
	if err != nil {
		return code, err
	}
	if res.StatusCode != http.StatusOK {
		_, err := answer(res, body)
		return code, err
	}
	return code, json.Unmarshal(body, &code)
}

// WaitForToken polls until the player approves code, then returns the account's new token. It stops
// with ErrLinkDenied, ErrLinkExpired, or the context's error; a server hiccup is waited out.
func (c *Client) WaitForToken(ctx context.Context, code DeviceCode) (string, error) {
	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	deadline := c.now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for {
		if err := c.sleep(ctx, interval); err != nil {
			return "", err
		}
		if c.now().After(deadline) {
			return "", ErrLinkExpired
		}
		token, verdict, err := c.pollToken(ctx, code.DeviceCode)
		switch {
		case err != nil && Retryable(err):
			continue
		case err != nil:
			return "", err
		case token != "":
			return token, nil
		case verdict == "authorization_pending":
		case verdict == "slow_down":
			interval += slowDownStep
		case verdict == "access_denied":
			return "", ErrLinkDenied
		case verdict == "expired_token":
			return "", ErrLinkExpired
		default:
			return "", fmt.Errorf("unexpected answer while linking: %q", verdict)
		}
	}
}

// pollToken asks once: the token when approved, else the server's error word.
func (c *Client) pollToken(ctx context.Context, deviceCode string) (token, verdict string, err error) {
	res, body, err := c.postJSON(ctx, "/api/v1/device/token", map[string]string{"device_code": deviceCode})
	if err != nil {
		return "", "", err
	}
	var reply struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	_ = json.Unmarshal(body, &reply)
	switch {
	case res.StatusCode == http.StatusOK && reply.AccessToken != "":
		return reply.AccessToken, "", nil
	case res.StatusCode == http.StatusBadRequest && reply.Error != "":
		return "", reply.Error, nil
	case res.StatusCode == http.StatusOK:
		return "", "", errors.New("the server answered without a token")
	default:
		_, err := answer(res, body)
		return "", "", err
	}
}

// postJSON sends v as JSON with no token: linking happens before the app has one.
func (c *Client) postJSON(ctx context.Context, path string, v any) (*http.Response, []byte, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	res, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &unavailableError{cause: err}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
	return res, body, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
