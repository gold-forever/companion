package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSharedStateIsAskedForWithTheTokenAndItsETag(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"v":1}`, map[string]string{"ETag": `"s-2"`})

	body, etag, err := c.SharedState(context.Background(), `"s-1"`)

	if err != nil || string(body) != `{"v":1}` || etag != `"s-2"` {
		t.Fatalf("SharedState = %q, %q, %v", body, etag, err)
	}
	if seen.Method != "GET" || seen.URL.Path != "/api/v1/shared/state" || seen.URL.RawQuery != "" {
		t.Errorf("asked %s %s?%s", seen.Method, seen.URL.Path, seen.URL.RawQuery)
	}
	if seen.Header.Get("Authorization") != "Bearer gf_secret" || seen.Header.Get("If-None-Match") != `"s-1"` {
		t.Errorf("headers = %v", seen.Header)
	}
}

func TestAnUnchangedSharedStateIsNotModified(t *testing.T) {
	c, _, _ := server(t, http.StatusNotModified, "", nil)
	if body, etag, err := c.SharedState(context.Background(), `"s-1"`); !errors.Is(err, ErrNotModified) || body != nil || etag != `"s-1"` {
		t.Fatalf("SharedState = %q, %q, %v", body, etag, err)
	}
}

func TestAServerWithoutSharedStateSaysSo(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, `{"reason":"not found"}`, nil)
	if _, _, err := c.SharedState(context.Background(), ""); !errors.Is(err, ErrNoShared) {
		t.Fatalf("err = %v, want ErrNoShared", err)
	}
}

func TestSharedStateIsRefusedAsPricesAre(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if _, _, err := c.SharedState(context.Background(), ""); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: err = %v", err)
	}
	c, _, _ = server(t, http.StatusForbidden, `{"reason":"suspended"}`, nil)
	var forbidden *ForbiddenError
	if _, _, err := c.SharedState(context.Background(), ""); !errors.As(err, &forbidden) {
		t.Errorf("403: err = %v", err)
	}
	c, _, _ = server(t, http.StatusOK, strings.Repeat(" ", sharedLimit+1), nil)
	if _, _, err := c.SharedState(context.Background(), ""); err == nil {
		t.Error("an answer over the limit is not a shared state")
	}
}

func TestASharedStateLargerThanOtherAnswersIsRead(t *testing.T) {
	big := `{"v":1,"pad":"` + strings.Repeat("x", 4*maxAnswerBytes) + `"}`
	c, _, _ := server(t, http.StatusOK, big, nil)
	if body, _, err := c.SharedState(context.Background(), ""); err != nil || len(body) != len(big) {
		t.Fatalf("a %d-byte state: %d bytes, %v", len(big), len(body), err)
	}
}
