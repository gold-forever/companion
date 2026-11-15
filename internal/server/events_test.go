package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stream serves body as an event stream and keeps what was asked; hold keeps it open after body.
func stream(t *testing.T, body string, hold bool) (*Client, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(context.Background())
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
		w.(http.Flusher).Flush()
		if hold {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "gf_secret", Version: "0.9.0", OS: "linux"}, &seen
}

func TestEventsDeliversEachEventAndSkipsKeepalives(t *testing.T) {
	c, seen := stream(t, "id: 1\nevent: hello\n\n: keepalive\n\nid: 2\nevent: prices\ndata: {\"market\":\"forever-x\"}\n\n: keepalive\n\nid: 3\nevent: shared\n\n", false)
	var got []Event

	err := c.Events(context.Background(), "", func(e Event) { got = append(got, e) })

	want := []Event{{ID: "1", Name: "hello"}, {ID: "2", Name: "prices", Data: `{"market":"forever-x"}`}, {ID: "3", Name: "shared"}}
	if len(got) != len(want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if !errors.Is(err, ErrStreamEnded) {
		t.Errorf("err = %v, want the stream's end", err)
	}
	if seen.URL.Path != "/api/v1/companion/events" || seen.Header.Get("Authorization") != "Bearer gf_secret" ||
		seen.Header.Get("Accept") != "text/event-stream" || seen.Header.Get("User-Agent") != "gold-forever-companion/0.9.0 (linux)" {
		t.Errorf("asked %s with %v", seen.URL.Path, seen.Header)
	}
	if _, sent := seen.Header["Last-Event-Id"]; sent {
		t.Errorf("Last-Event-ID sent on a first connect")
	}
}

func TestEventsResumesFromTheLastEventID(t *testing.T) {
	c, seen := stream(t, "", false)
	_ = c.Events(context.Background(), "41", func(Event) {})
	if seen.Header.Get("Last-Event-ID") != "41" {
		t.Fatalf("Last-Event-ID = %q, want 41", seen.Header.Get("Last-Event-ID"))
	}
}

func TestEventsRefusedByTheServerIsItsError(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if err := c.Events(context.Background(), "", func(Event) {}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestEventsGivesUpOnAStreamThatFallsSilent(t *testing.T) {
	c, _ := stream(t, "id: 1\nevent: hello\n\n", true)
	c.StreamIdle = 50 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- c.Events(context.Background(), "", func(Event) {}) }()
	select {
	case err := <-done:
		if !Retryable(err) {
			t.Fatalf("err = %v, want one to retry", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a silent stream was held open")
	}
}

func TestEventsEndsWithItsContext(t *testing.T) {
	c, _ := stream(t, ": keepalive\n\n", true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, "", func(Event) {}) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived its context")
	}
}

func TestEventsKeepsAStreamOpenWhileItsKeepalivesArrive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 10 {
			_, _ = io.WriteString(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "gf_secret", StreamIdle: 60 * time.Millisecond}

	if err := c.Events(context.Background(), "", func(Event) {}); !errors.Is(err, ErrStreamEnded) {
		t.Fatalf("err = %v; want the server's own end after 200 ms of keepalives", err)
	}
}
