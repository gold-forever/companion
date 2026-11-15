package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/server"
)

// fakeStream plays one connection's events per Events call, then drops it with that connection's
// error; past the last connection it says so on held and holds the stream open until ctx ends.
type fakeStream struct {
	conns   [][]server.Event
	drops   []error
	resumed []string // the Last-Event-ID of each connect
	held    chan struct{}
}

func (f *fakeStream) Events(ctx context.Context, lastID string, on func(server.Event)) error {
	f.resumed = append(f.resumed, lastID)
	n := len(f.resumed) - 1
	if n >= len(f.conns) {
		close(f.held)
		<-ctx.Done()
		return ctx.Err()
	}
	for _, e := range f.conns[n] {
		on(e)
	}
	return f.drops[n]
}

// listening runs a listener over f until it has played every connection, and returns how often it
// poked, the waits it asked for between connections, and its state once stopped.
func listening(t *testing.T, f *fakeStream, carries func(string) bool) (int, []time.Duration, string) {
	t.Helper()
	f.held = make(chan struct{})
	pokes := 0
	var waits []time.Duration
	state := &streamState{}
	l := listener{
		events: f, poke: func() { pokes++ }, carries: carries, state: state,
		after: func(d time.Duration) <-chan time.Time {
			waits = append(waits, d)
			ch := make(chan time.Time, 1)
			ch <- time.Time{}
			return ch
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	select {
	case <-f.held:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener did not reconnect")
	}
	cancel()
	<-done
	return pokes, waits, state.String()
}

func all(string) bool { return true }

func TestAnEventOfWhatTheGameCarriesPokesTheRefreshOnce(t *testing.T) {
	f := &fakeStream{
		conns: [][]server.Event{{
			{ID: "1", Name: "hello"},
			{ID: "2", Name: "shared"},
			{ID: "3", Name: "names"},
			{ID: "4", Name: "prices", Data: `{"market":"forever-eu"}`},
			{ID: "5", Name: "prices", Data: `{"market":"retail-us"}`},
		}, {{ID: "6", Name: "hello"}}},
		drops: []error{errors.New("down"), server.ErrStreamEnded},
	}

	pokes, _, _ := listening(t, f, func(m string) bool { return m == "forever-eu" })

	if pokes != 3+1 { // shared, names, the carried market; then the hello after the drop
		t.Fatalf("poked %d times, want 4: one per event carried and one on reconnecting", pokes)
	}
}

func TestADroppedStreamReconnectsFromItsLastEventAndBacksOff(t *testing.T) {
	f := &fakeStream{
		conns: [][]server.Event{{{ID: "7", Name: "hello"}, {ID: "8", Name: "shared"}}, {}, {{ID: "9", Name: "hello"}}},
		drops: []error{server.ErrStreamEnded, server.ErrStreamEnded, server.ErrStreamEnded},
	}

	_, waits, state := listening(t, f, all)

	if f.resumed[0] != "" || f.resumed[1] != "8" || f.resumed[2] != "8" || f.resumed[3] != "9" {
		t.Errorf("resumed from %q, want none, then the last event's id", f.resumed)
	}
	// A connection that said hello starts the backoff again; one that said nothing doubles it.
	if len(waits) != 3 || waits[0] != streamFirstWait || waits[1] != 2*streamFirstWait || waits[2] != streamFirstWait {
		t.Errorf("waits = %v", waits)
	}
	if state != "" {
		t.Errorf("state after stopping = %q, want none", state)
	}
}

func TestAStreamTheServerRefusesIsTriedAgainSlowly(t *testing.T) {
	f := &fakeStream{conns: [][]server.Event{{}}, drops: []error{server.ErrUnauthorized}}
	f.conns = append(f.conns, []server.Event{{ID: "1", Name: "hello"}})
	f.drops = append(f.drops, server.ErrStreamEnded)

	_, waits, _ := listening(t, f, all)

	if waits[0] != streamLongWait {
		t.Fatalf("waits = %v, want %s after a refusal", waits, streamLongWait)
	}
}

func TestTheStreamStateIsReconnectingBetweenConnections(t *testing.T) {
	state := &streamState{}
	l := listener{events: &fakeStream{conns: [][]server.Event{{{ID: "1", Name: "hello"}}}, drops: []error{server.ErrStreamEnded}},
		poke: func() {}, carries: all, state: state}
	seen := make(chan string, 1)
	l.after = func(time.Duration) <-chan time.Time {
		seen <- state.String()
		return make(chan time.Time)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	var got string
	select {
	case got = <-seen:
	case <-time.After(5 * time.Second):
	}
	cancel()
	<-done
	if got != streamReconnecting {
		t.Fatalf("state while waiting = %q", got)
	}
}

// lines is a writer the test reads while the uploads write.
type lines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lines) count(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.buf.String(), s)
}

func TestAnEventOnTheStreamLeadsToOneFetchAndOneWrite(t *testing.T) {
	c, _ := fixture(t)
	var asked atomic.Int32
	srv := &fakeServer{
		prices: func(context.Context) ([]byte, string, error) { asked.Add(1); return []byte(twoItems), `"e1"`, nil },
		events: func(ctx context.Context, _ string, on func(server.Event)) error {
			on(server.Event{ID: "1", Name: "hello"})
			on(server.Event{ID: "2", Name: "shared"})
			<-ctx.Done()
			return ctx.Err()
		},
	}
	p := c.parts(fakeConnection{srv, validToken})
	out := &lines{}
	p.Out = out
	u := NewUploads(p)
	u.refresher.Gap = 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = u.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for asked.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // any further fetch would come now
	cancel()
	<-done

	if n, written := asked.Load(), out.count("written into"); n != 2 || written != 2 {
		t.Fatalf("asked %d times and wrote %d times; want the start's and the event's, one each", n, written)
	}
	if c.stream.String() != "" {
		t.Errorf("the stream's state outlived its round: %q", c.stream.String())
	}
}
