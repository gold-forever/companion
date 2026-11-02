// Package server is the companion's HTTP client for the Gold Forever server. Every request it can make
// is listed in PROTOCOL.md; the requests and answers are the types generated from openapi.yaml.
package server

//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// How long the server may take to answer once the request is sent, and the whole of a small call.
const answerTimeout = 2 * time.Minute

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

func (c *Client) userAgent() string {
	return fmt.Sprintf("gold-forever-companion/%s (%s)", c.Version, c.OS)
}

func (c *Client) httpClient() *http.Client {
	client := http.Client{Timeout: answerTimeout}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.CheckRedirect = carryToken
	return &client
}
