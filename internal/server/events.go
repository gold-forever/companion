package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Event is one event of the companion's stream: a signal that something it carries into the game
// changed, which it then fetches over its other calls.
type Event struct {
	ID   string // grows with each event; sent back as Last-Event-ID to resume
	Name string // hello, shared, prices or names
	Data string // for prices, {"market":"…"}
}

// ErrStreamEnded means the server closed the stream: connect again.
var ErrStreamEnded = &unavailableError{cause: errors.New("the server closed the event stream")}

// The server sends a keepalive every 25 s; a stream silent for longer than two is dropped.
const streamIdle = 60 * time.Second

// The longest line of the stream to trust: its events carry a market key at most.
const maxEventLine = 4 << 10

// Events holds the companion's event stream open, handing on each event, until ctx ends or the stream
// drops. With lastID the server resumes after that event. Every drop is retryable; a refusal is the
// server's answer, as for the other calls.
func (c *Client) Events(ctx context.Context, lastID string, on func(Event)) error {
	url, err := c.endpoint("/api/v1/companion/events")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	client := c.httpClient()
	client.Timeout = 0 // the stream stays open; silence is what ends it
	idle := orDefault(c.StreamIdle, streamIdle)
	silent := time.AfterFunc(idle, cancel)
	defer silent.Stop()
	res, err := client.Do(req)
	if err != nil {
		return &unavailableError{cause: err}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
		_, err := answer(res, body)
		if err == nil {
			err = fmt.Errorf("the event stream answered %s", res.Status)
		}
		return err
	}
	lines := bufio.NewScanner(res.Body)
	lines.Buffer(make([]byte, 0, 512), maxEventLine)
	var e Event
	for lines.Scan() {
		silent.Reset(idle)
		line := lines.Text()
		if line == "" { // the blank line that ends an event
			if e.Name != "" {
				on(e)
			}
			e = Event{ID: e.ID}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field { // "" is a comment, the keepalive among them
		case "id":
			e.ID = value
		case "event":
			e.Name = value
		case "data":
			e.Data = value
		}
	}
	if err := lines.Err(); err != nil {
		return &unavailableError{cause: err}
	}
	if ctx.Err() != nil {
		return &unavailableError{cause: errors.New("the event stream fell silent")}
	}
	return ErrStreamEnded
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
