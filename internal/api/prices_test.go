package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestPricesAsksForOneMarketWithTheToken(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"v":1}`, map[string]string{"ETag": `"m-7"`})

	body, etag, err := c.Prices(context.Background(), "forever", "forever-BETA-normal-horde", "")

	if err != nil || string(body) != `{"v":1}` || etag != `"m-7"` {
		t.Fatalf("Prices = %q, %q, %v", body, etag, err)
	}
	if _, sent := seen.Header["If-None-Match"]; sent {
		t.Errorf("If-None-Match sent without an ETag: %q", seen.Header.Get("If-None-Match"))
	}
	got := map[string]string{
		"method":        seen.Method,
		"path":          seen.URL.Path,
		"game":          seen.URL.Query().Get("game"),
		"market":        seen.URL.Query().Get("market"),
		"authorization": seen.Header.Get("Authorization"),
	}
	want := map[string]string{
		"method":        "GET",
		"path":          "/api/v1/prices",
		"game":          "forever",
		"market":        "forever-BETA-normal-horde",
		"authorization": "Bearer gf_secret",
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("%s = %q, want %q", k, got[k], want[k])
		}
	}
}

func TestPricesWithoutAValidTokenIsUnauthorized(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if _, _, err := c.Prices(context.Background(), "forever", "m", ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestPricesForAnUnknownMarketIsRefused(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, `{"reason":"unknown market"}`, nil)
	var refused *RefusedError
	if _, _, err := c.Prices(context.Background(), "forever", "m", ""); !errors.As(err, &refused) || refused.Status != 404 {
		t.Fatalf("err = %v", err)
	}
}

func TestPricesRefusesAnAnswerOverTheLimit(t *testing.T) {
	big := make([]byte, maxPricesBytes+1)
	c, _, _ := server(t, http.StatusOK, string(big), nil)
	if _, _, err := c.Prices(context.Background(), "forever", "m", ""); err == nil {
		t.Fatal("an answer over the limit is not a price list")
	}
}

func TestPricesAsksOnlyForAChangeSinceTheETagItHolds(t *testing.T) {
	c, seen, _ := server(t, http.StatusNotModified, "", nil)

	body, etag, err := c.Prices(context.Background(), "forever", "m", `"m-7"`)

	if !errors.Is(err, ErrNotModified) || body != nil || etag != `"m-7"` {
		t.Fatalf("Prices = %q, %q, %v; want ErrNotModified and the ETag kept", body, etag, err)
	}
	if got := seen.Header.Get("If-None-Match"); got != `"m-7"` {
		t.Errorf("If-None-Match = %q", got)
	}
}

func TestPricesFailingOnTheServerIsWorthTryingAgain(t *testing.T) {
	c, _, _ := server(t, http.StatusInternalServerError, "", nil)
	if _, _, err := c.Prices(context.Background(), "forever", "m", `"m-7"`); !Retryable(err) {
		t.Fatalf("err = %v, want retryable", err)
	}
}
