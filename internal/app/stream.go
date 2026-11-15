package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/server"
)

// The stream's states, as the page and the tray show them; "" while there is none.
const (
	streamConnected    = "connected"
	streamReconnecting = "reconnecting"
)

// A dropped stream is opened again after streamFirstWait, doubling up to streamLongWait, the poll that
// stands in for it meanwhile; a stream the server refuses waits that long at once.
const (
	streamFirstWait = 2 * time.Second
	streamLongWait  = refreshPricesEvery
)

// eventSource is the server's event stream; server.Client is the real one.
type eventSource interface {
	Events(ctx context.Context, lastID string, on func(server.Event)) error
}

// streamState is the stream's state, for the page and the tray.
type streamState struct {
	mu    sync.Mutex
	state string
}

func (s *streamState) set(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *streamState) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// listener holds the event stream open and pokes the refresh on each event of what the game carries:
// the account's lists and profiles, the names wanted, or the prices of a market it carries.
type listener struct {
	events  eventSource
	poke    func()
	carries func(market string) bool
	state   *streamState
	after   func(time.Duration) <-chan time.Time // the clock, time.After
}

// Run listens until ctx ends, reconnecting from the last event seen. A reconnect pokes once, for what
// changed while the stream was down.
func (l listener) Run(ctx context.Context) {
	defer l.state.set("")
	var lastID string
	var wait time.Duration
	dropped := false
	for ctx.Err() == nil {
		heard := false
		err := l.events.Events(ctx, lastID, func(e server.Event) {
			heard = true
			l.state.set(streamConnected)
			if e.ID != "" {
				lastID = e.ID
			}
			if l.wanted(e, dropped) {
				l.poke()
			}
		})
		if ctx.Err() != nil {
			return
		}
		l.state.set(streamReconnecting)
		dropped = true
		wait = nextStreamWait(wait, heard, err)
		select {
		case <-ctx.Done():
			return
		case <-l.after(wait):
		}
	}
}

// wanted says whether e calls for a refresh: hello only after a drop.
func (l listener) wanted(e server.Event, dropped bool) bool {
	switch e.Name {
	case "hello":
		return dropped
	case "shared", "names":
		return true
	case "prices":
		var p struct {
			Market string `json:"market"`
		}
		return json.Unmarshal([]byte(e.Data), &p) != nil || l.carries(p.Market)
	}
	return false
}

// nextStreamWait is the wait before the next connect: the first again after a connection that said
// something, double the last after one that did not, and the longest after a refusal.
func nextStreamWait(last time.Duration, heard bool, err error) time.Duration {
	var limited *server.RateLimitedError
	switch {
	case errors.As(err, &limited):
		return max(limited.RetryAfter, streamFirstWait)
	case !server.Retryable(err):
		return streamLongWait
	case heard || last == 0:
		return streamFirstWait
	}
	return min(2*last, streamLongWait)
}
