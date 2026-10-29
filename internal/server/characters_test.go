package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestTheCharactersAreAskedForWithTheTokenAndTheirETag(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"characters":[]}`, map[string]string{"ETag": `"c-2"`})

	body, etag, err := c.Characters(context.Background(), `"c-1"`)

	if err != nil || string(body) != `{"characters":[]}` || etag != `"c-2"` {
		t.Fatalf("Characters = %q, %q, %v", body, etag, err)
	}
	if seen.Method != "GET" || seen.URL.Path != "/api/v1/companion/characters" || seen.URL.RawQuery != "" {
		t.Errorf("asked %s %s?%s", seen.Method, seen.URL.Path, seen.URL.RawQuery)
	}
	if seen.Header.Get("Authorization") != "Bearer gf_secret" || seen.Header.Get("If-None-Match") != `"c-1"` {
		t.Errorf("headers = %v", seen.Header)
	}
}

func TestUnchangedCharactersAreNotModified(t *testing.T) {
	c, _, _ := server(t, http.StatusNotModified, "", nil)
	if body, etag, err := c.Characters(context.Background(), `"c-1"`); !errors.Is(err, ErrNotModified) || body != nil || etag != `"c-1"` {
		t.Fatalf("Characters = %q, %q, %v", body, etag, err)
	}
}

func TestAServerWithoutTheCharactersSaysSo(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, `{"reason":"not found"}`, nil)
	if _, _, err := c.Characters(context.Background(), ""); !errors.Is(err, ErrNoCharacters) {
		t.Fatalf("err = %v, want ErrNoCharacters", err)
	}
}

func TestTheCharactersAreRefusedAsPricesAre(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if _, _, err := c.Characters(context.Background(), ""); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: err = %v", err)
	}
	c, _, _ = server(t, http.StatusForbidden, `{"reason":"suspended"}`, nil)
	var forbidden *ForbiddenError
	if _, _, err := c.Characters(context.Background(), ""); !errors.As(err, &forbidden) {
		t.Errorf("403: err = %v", err)
	}
	c, _, _ = server(t, http.StatusOK, strings.Repeat(" ", charactersLimit+1), nil)
	if _, _, err := c.Characters(context.Background(), ""); err == nil {
		t.Error("an answer over the limit is not a list of characters")
	}
}
