package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestTheNamesWantedAreAskedForTheGameAndLocaleWithTheirETag(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"locale":"frFR","items":[]}`, map[string]string{"ETag": `"n-2"`})

	body, etag, err := c.NamesWanted(context.Background(), "forever", "frFR", `"n-1"`)

	if err != nil || string(body) != `{"locale":"frFR","items":[]}` || etag != `"n-2"` {
		t.Fatalf("NamesWanted = %q, %q, %v", body, etag, err)
	}
	if seen.Method != "GET" || seen.URL.Path != "/api/v1/names/wanted" || seen.URL.RawQuery != "game=forever&locale=frFR" {
		t.Errorf("asked %s %s?%s", seen.Method, seen.URL.Path, seen.URL.RawQuery)
	}
	if seen.Header.Get("If-None-Match") != `"n-1"` {
		t.Errorf("headers = %v", seen.Header)
	}
}

func TestUnchangedNamesWantedAreNotModified(t *testing.T) {
	c, _, _ := server(t, http.StatusNotModified, "", nil)
	if body, etag, err := c.NamesWanted(context.Background(), "forever", "deDE", `"n-1"`); !errors.Is(err, ErrNotModified) || body != nil || etag != `"n-1"` {
		t.Fatalf("NamesWanted = %q, %q, %v", body, etag, err)
	}
}

func TestAServerWithoutTheNamesWantedForALocaleSaysSo(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest} {
		c, _, _ := server(t, status, `{"status":"bad request"}`, nil)
		if _, _, err := c.NamesWanted(context.Background(), "forever", "enUS", ""); !errors.Is(err, ErrNoNamesWanted) {
			t.Errorf("%d: err = %v, want ErrNoNamesWanted", status, err)
		}
	}
}

func TestNamesWantedOverTheLimitAreRefused(t *testing.T) {
	c, _, _ := server(t, http.StatusOK, strings.Repeat(" ", namesWantedLimit+1), nil)
	if _, _, err := c.NamesWanted(context.Background(), "forever", "frFR", ""); err == nil {
		t.Error("an answer over the limit is not a list of names wanted")
	}
}
