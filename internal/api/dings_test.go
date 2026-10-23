package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestDingsStateIsAskedForWithTheTokenAndItsETag(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"v":1}`, map[string]string{"ETag": `"d-2"`})

	body, etag, err := c.DingsState(context.Background(), `"d-1"`)

	if err != nil || string(body) != `{"v":1}` || etag != `"d-2"` {
		t.Fatalf("DingsState = %q, %q, %v", body, etag, err)
	}
	if seen.Method != "GET" || seen.URL.Path != "/api/v1/dings/state" || seen.URL.RawQuery != "" {
		t.Errorf("asked %s %s?%s", seen.Method, seen.URL.Path, seen.URL.RawQuery)
	}
	if seen.Header.Get("Authorization") != "Bearer gf_secret" || seen.Header.Get("If-None-Match") != `"d-1"` {
		t.Errorf("headers = %v", seen.Header)
	}
}

func TestAnUnchangedDingsStateIsNotModified(t *testing.T) {
	c, _, _ := server(t, http.StatusNotModified, "", nil)
	if body, etag, err := c.DingsState(context.Background(), `"d-1"`); !errors.Is(err, ErrNotModified) || body != nil || etag != `"d-1"` {
		t.Fatalf("DingsState = %q, %q, %v", body, etag, err)
	}
}

func TestAServerWithoutDingsStateSaysSo(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, `{"reason":"not found"}`, nil)
	if _, _, err := c.DingsState(context.Background(), ""); !errors.Is(err, ErrNoDings) {
		t.Fatalf("err = %v, want ErrNoDings", err)
	}
}

func TestDingsStateIsRefusedAsPricesAre(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if _, _, err := c.DingsState(context.Background(), ""); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: err = %v", err)
	}
	c, _, _ = server(t, http.StatusForbidden, `{"reason":"suspended"}`, nil)
	var forbidden *ForbiddenError
	if _, _, err := c.DingsState(context.Background(), ""); !errors.As(err, &forbidden) {
		t.Errorf("403: err = %v", err)
	}
	c, _, _ = server(t, http.StatusOK, string(make([]byte, maxAnswerBytes+1)), nil)
	if _, _, err := c.DingsState(context.Background(), ""); err == nil {
		t.Error("an answer over the limit is not a dings state")
	}
}
