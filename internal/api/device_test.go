package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clock is a fake time: sleeping moves it on, and every sleep is recorded.
type clock struct {
	now   time.Time
	slept []time.Duration
}

func (c *clock) Now() time.Time { return c.now }

func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	return nil
}

// tokenServer answers each poll of /api/v1/device/token with the next of answers (status, body); the
// last one repeats. It counts the polls.
func tokenServer(t *testing.T, answers ...[2]any) (*Client, *clock, *int) {
	t.Helper()
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DeviceCode string `json:"device_code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/api/v1/device/token" || body.DeviceCode != "dc_secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected poll: %s %q %q", r.URL.Path, body.DeviceCode, r.Header.Get("Content-Type"))
		}
		a := answers[min(polls, len(answers)-1)]
		polls++
		w.WriteHeader(a[0].(int))
		_, _ = io.WriteString(w, a[1].(string))
	}))
	t.Cleanup(srv.Close)
	clk := &clock{now: time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)}
	return &Client{BaseURL: srv.URL, Version: "0.0.1", OS: "linux", Now: clk.Now, Sleep: clk.Sleep}, clk, &polls
}

var code = DeviceCode{DeviceCode: "dc_secret", UserCode: "BCDF-GHJK", ExpiresIn: 600, Interval: 5}

const pending = `{"error":"authorization_pending"}`

func TestStartLinkAsksForACodeNamingThisComputer(t *testing.T) {
	var got map[string]string
	var contentType, agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/device/code" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		contentType, agent = r.Header.Get("Content-Type"), r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"device_code":"dc_secret","user_code":"BCDF-GHJK","verification_uri":"https://gold-forever.app/link",`+
			`"verification_uri_complete":"https://gold-forever.app/link?code=BCDF-GHJK","expires_in":600,"interval":5}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Version: "0.0.1", OS: "linux"}

	started, err := c.StartLink(context.Background(), "gaming-pc")

	if err != nil {
		t.Fatalf("start: %v", err)
	}
	want := DeviceCode{"dc_secret", "BCDF-GHJK", "https://gold-forever.app/link", "https://gold-forever.app/link?code=BCDF-GHJK", 600, 5}
	if started != want {
		t.Errorf("code = %+v", started)
	}
	if got["client"] != "gold-forever-companion" || got["hostname"] != "gaming-pc" {
		t.Errorf("body = %v", got)
	}
	if contentType != "application/json" || agent != "gold-forever-companion/0.0.1 (linux)" {
		t.Errorf("headers: %q %q", contentType, agent)
	}
}

func TestStartLinkNamesAComputerWithoutAHostname(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"device_code":"d","user_code":"u","expires_in":600,"interval":5}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := (&Client{BaseURL: srv.URL}).StartLink(context.Background(), "  "); err != nil {
		t.Fatal(err)
	}
	if got["hostname"] != "this computer" {
		t.Errorf("hostname = %q", got["hostname"])
	}
}

func TestStartLinkWhenTheServerIsBusyIsWorthTryingLater(t *testing.T) {
	c, _, _ := server(t, 503, "", nil)

	_, err := c.StartLink(context.Background(), "pc")

	if !Retryable(err) {
		t.Errorf("err = %v, want retryable", err)
	}
}

func TestWaitForTokenPollsEveryIntervalUntilApproved(t *testing.T) {
	c, clk, polls := tokenServer(t, [2]any{400, pending}, [2]any{400, pending},
		[2]any{200, `{"access_token":"gf_new","token_type":"Bearer","scope":"saves:write prices:read"}`})

	token, err := c.WaitForToken(context.Background(), code)

	if err != nil || token != "gf_new" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
	if *polls != 3 {
		t.Errorf("polls = %d", *polls)
	}
	for _, d := range clk.slept {
		if d != 5*time.Second {
			t.Errorf("slept %v, want the 5 s interval before every poll", clk.slept)
			break
		}
	}
}

func TestWaitForTokenAsksLessOftenAfterSlowDown(t *testing.T) {
	c, clk, _ := tokenServer(t, [2]any{400, `{"error":"slow_down"}`}, [2]any{400, pending},
		[2]any{200, `{"access_token":"gf_new"}`})

	if _, err := c.WaitForToken(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(clk.slept) != len(want) || clk.slept[1] != want[1] || clk.slept[2] != want[2] {
		t.Errorf("slept %v, want %v (RFC 8628: slow_down adds 5 s for good)", clk.slept, want)
	}
}

func TestWaitForTokenStopsWhenThePlayerDenies(t *testing.T) {
	c, _, _ := tokenServer(t, [2]any{400, pending}, [2]any{400, `{"error":"access_denied"}`})

	_, err := c.WaitForToken(context.Background(), code)

	if !errors.Is(err, ErrLinkDenied) {
		t.Errorf("err = %v, want ErrLinkDenied", err)
	}
}

func TestWaitForTokenStopsWhenTheServerSaysExpired(t *testing.T) {
	c, _, _ := tokenServer(t, [2]any{400, `{"error":"expired_token"}`})

	_, err := c.WaitForToken(context.Background(), code)

	if !errors.Is(err, ErrLinkExpired) {
		t.Errorf("err = %v, want ErrLinkExpired", err)
	}
}

func TestWaitForTokenGivesUpWhenTheCodeRunsOut(t *testing.T) {
	c, clk, polls := tokenServer(t, [2]any{400, pending})
	start := clk.now

	_, err := c.WaitForToken(context.Background(), code)

	if !errors.Is(err, ErrLinkExpired) {
		t.Errorf("err = %v, want ErrLinkExpired", err)
	}
	if clk.now.Sub(start) > 10*time.Minute+5*time.Second {
		t.Errorf("kept polling for %v after a 10-minute code", clk.now.Sub(start))
	}
	if *polls < 100 || *polls > 120 {
		t.Errorf("polls = %d, want one every 5 s for 10 minutes", *polls)
	}
}

func TestWaitForTokenRidesOutAServerHiccup(t *testing.T) {
	c, _, _ := tokenServer(t, [2]any{502, "bad gateway"}, [2]any{200, `{"access_token":"gf_new"}`})

	token, err := c.WaitForToken(context.Background(), code)

	if err != nil || token != "gf_new" {
		t.Errorf("token = %q, err = %v", token, err)
	}
}

func TestWaitForTokenStopsWhenCancelled(t *testing.T) {
	c, _, _ := tokenServer(t, [2]any{400, pending})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.WaitForToken(ctx, code)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// The approval page is opened by the system's URL handler, which on Windows also runs files and other
// schemes: only a page of the site the companion talks to, or of the public site, is taken.
func TestALinkToAnythingButTheSiteIsRefused(t *testing.T) {
	for link, ok := range map[string]bool{
		"/link?code=BCDF-GHJK":                         true, // made absolute on the test server below
		"https://gold-forever.app/link?code=BCDF-GHJK": true,
		"https://evil.example/link?code=BCDF-GHJK":     false,
		"file:///C:/Users/Public/x.exe":                false,
		`\\host\share\x.exe`:                           false,
		"search-ms:query=x":                            false,
		"https://gold-forever.app@evil.example/link":   false,
		"http://gold-forever.app/link":                 false,
	} {
		var base string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			complete := link
			if strings.HasPrefix(link, "/") {
				complete = base + link
			}
			payload, _ := json.Marshal(map[string]any{"device_code": "dc", "user_code": "BCDF-GHJK", "verification_uri_complete": complete, "expires_in": 600, "interval": 5})
			_, _ = w.Write(payload)
		}))
		base = srv.URL
		c := &Client{BaseURL: srv.URL, Version: "0.0.1", OS: "linux"}

		_, err := c.StartLink(context.Background(), "host")

		if (err == nil) != ok {
			t.Errorf("%s: err = %v", link, err)
		}
		srv.Close()
	}
}
