package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// server answers every request with status and body, and keeps the last request it saw.
func server(t *testing.T, status int, body string, headers map[string]string) (*Client, *http.Request, *[]byte) {
	t.Helper()
	var seen http.Request
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(context.Background())
		sent, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: "gf_secret", Version: "0.0.1", OS: "linux"}, &seen, &sent
}

func TestUploadSendsTheSaveAsItIsWithTheToken(t *testing.T) {
	c, seen, sent := server(t, 200, `{"status":"imported","problems":2,"saves":7}`, nil)

	answer, err := c.Upload(context.Background(), []byte("GoldForeverDB = {}"))

	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if answer.Status != "imported" || answer.Problems != 2 || answer.Saves != 7 {
		t.Errorf("answer = %+v", answer)
	}
	if string(*sent) != "GoldForeverDB = {}" {
		t.Errorf("body = %q", *sent)
	}
	checks := map[string]string{
		"method":        seen.Method,
		"path":          seen.URL.Path,
		"name":          seen.URL.Query().Get("name"),
		"authorization": seen.Header.Get("Authorization"),
		"content-type":  seen.Header.Get("Content-Type"),
		"user-agent":    seen.Header.Get("User-Agent"),
	}
	want := map[string]string{
		"method":        "POST",
		"path":          "/api/v1/saves",
		"name":          "GoldForever.lua",
		"authorization": "Bearer gf_secret",
		"content-type":  "application/octet-stream",
		"user-agent":    "gold-forever-companion/0.0.1 (linux)",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s = %q, want %q", k, checks[k], w)
		}
	}
}

func TestAnAlreadyImportedSaveIsNotAnError(t *testing.T) {
	c, _, _ := server(t, 200, `{"status":"already imported","saves":7}`, nil)

	answer, err := c.Upload(context.Background(), []byte("x"))

	if err != nil || answer.Status != "already imported" {
		t.Fatalf("answer = %+v, err = %v", answer, err)
	}
}

func TestARevokedTokenStopsUploads(t *testing.T) {
	c, _, _ := server(t, 401, `{"status":"unauthorized"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestAForbiddenAccountStopsUploadsWithTheReason(t *testing.T) {
	c, _, _ := server(t, 403, `{"status":"this account is suspended; uploads are refused"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) || forbidden.Reason != "this account is suspended; uploads are refused" {
		t.Fatalf("err = %v, want a ForbiddenError with the reason", err)
	}
	if Retryable(err) {
		t.Fatal("a forbidden upload must not be retried on its own")
	}
}

func TestASaveTheServerRefusesCarriesItsReason(t *testing.T) {
	for _, status := range []int{400, 422} {
		c, _, _ := server(t, status, `{"status":"unknown game","reason":"unknown game version 30403"}`, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		var refused *RefusedError
		if !errors.As(err, &refused) || refused.Status != status || refused.Reason != "unknown game version 30403" {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

func TestATooLargeSaveIsNotRetriedAndCarriesTheLimit(t *testing.T) {
	c, _, _ := server(t, 413, `{"status":"too large","reason":"a forever save may be at most 10 MB"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var tooLarge *TooLargeError
	if !errors.Is(err, ErrTooLarge) || Retryable(err) || !errors.As(err, &tooLarge) || tooLarge.Reason != "a forever save may be at most 10 MB" {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestRateLimitedWaitsForRetryAfter(t *testing.T) {
	c, _, _ := server(t, 429, ``, map[string]string{"Retry-After": "120"})

	_, err := c.Upload(context.Background(), []byte("x"))

	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 120*time.Second || !Retryable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitedWithoutRetryAfterWaitsAMinute(t *testing.T) {
	c, _, _ := server(t, 429, ``, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != time.Minute {
		t.Fatalf("err = %v", err)
	}
}

func TestAServerErrorIsRetried(t *testing.T) {
	c, _, _ := server(t, 502, `bad gateway`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestAnUnreachableServerIsRetried(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Token: "t", Version: "0", OS: "linux"}

	_, err := c.Upload(context.Background(), []byte("x"))

	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v, retryable = %v", err, Retryable(err))
	}
}

func TestTheBaseURLMayEndWithASlash(t *testing.T) {
	c, seen, _ := server(t, 200, `{"status":"imported"}`, nil)
	c.BaseURL += "/"

	if _, err := c.Upload(context.Background(), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/api/v1/saves" {
		t.Errorf("path = %q", seen.URL.Path)
	}
}

// A proxy or ingress mid-deploy, or a wrong url in the settings, is not the server's last word on a save.
func TestAnUnexpectedClientErrorIsRetried(t *testing.T) {
	for _, status := range []int{404, 405, 408, 409, 421} {
		c, _, _ := server(t, status, `<html>not here</html>`, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		var refused *RefusedError
		if !Retryable(err) || errors.As(err, &refused) {
			t.Errorf("%d: err = %v, retryable = %v", status, err, Retryable(err))
		}
	}
}

func TestAWrongContentTypeIsRefusedForGood(t *testing.T) {
	c, _, _ := server(t, 415, `{"status":"send the file as application/octet-stream"}`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	var refused *RefusedError
	if !errors.As(err, &refused) || Retryable(err) {
		t.Fatalf("err = %v", err)
	}
}

// A captive portal or a proxy answers 200 too; only the server's own answer means the save arrived.
func TestA200ThatIsNotTheServersAnswerIsRetried(t *testing.T) {
	for _, body := range []string{`<html>portal</html>`, `{"status":"maybe"}`, ``} {
		c, _, _ := server(t, 200, body, nil)

		_, err := c.Upload(context.Background(), []byte("x"))

		if !Retryable(err) {
			t.Errorf("%q: err = %v, retryable = %v", body, err, Retryable(err))
		}
	}
}

// While the operator pauses uploads the server answers 503 with Retry-After and why.
func TestAPausedServerSaysHowLongToWaitAndWhy(t *testing.T) {
	c, _, _ := server(t, 503, `{"status":"paused","reason":"maintenance until 20:00"}`, map[string]string{"Retry-After": "600"})

	_, err := c.Upload(context.Background(), []byte("x"))

	if !Retryable(err) || RetryAfter(err) != 600*time.Second || !strings.Contains(err.Error(), "maintenance until 20:00") {
		t.Fatalf("err = %v, retryable = %v, retry after %s", err, Retryable(err), RetryAfter(err))
	}
}

func TestAServerErrorWithoutRetryAfterLeavesTheWaitToTheClient(t *testing.T) {
	c, _, _ := server(t, 502, `bad gateway`, nil)

	_, err := c.Upload(context.Background(), []byte("x"))

	if RetryAfter(err) != 0 {
		t.Fatalf("retry after %s, want none", RetryAfter(err))
	}
}
