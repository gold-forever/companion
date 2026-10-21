package api

import (
	"context"
	"net/http"
	"testing"
)

func TestLatestReadsTheServersNewestRelease(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"version":"v0.2.0","url":"https://github.com/gold-forever/companion/releases/tag/v0.2.0"}`, nil)
	got, err := c.Latest(context.Background())
	if err != nil || got.Version != "v0.2.0" || got.URL == "" {
		t.Fatalf("Latest = %+v, %v", got, err)
	}
	if seen.URL.Path != "/api/v1/companion/latest" || seen.Method != http.MethodGet {
		t.Errorf("asked %s %s", seen.Method, seen.URL.Path)
	}
	if seen.Header.Get("Authorization") != "" {
		t.Error("the version check carries no token: it says nothing about who asks")
	}
}

func TestNoLatestReleaseIsNoNotice(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, ``, nil)
	if got, err := c.Latest(context.Background()); err != nil || got.Version != "" {
		t.Errorf("a 404 means the server names no release: %+v, %v", got, err)
	}
}
