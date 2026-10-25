package link

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/config"
)

// site hands out one code, then answers polls with tokenAnswer (status, body).
func site(t *testing.T, status int, body string) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/api/v1/device/code" {
			_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"BCDF-GHJK","verification_uri":"https://gold-forever.app/link",`+
				`"verification_uri_complete":"https://gold-forever.app/link?code=BCDF-GHJK","expires_in":600,"interval":5}`)
			return
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	now := time.Now()
	return &api.Client{
		BaseURL: srv.URL,
		Now:     func() time.Time { return now },
		Sleep:   func(context.Context, time.Duration) error { now = now.Add(5 * time.Second); return nil },
	}
}

func TestLinkSavesTheApprovedTokenAndKeepsTheOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{Folders: []string{"/wow"}, Off: []string{"/wow/x"}}); err != nil {
		t.Fatal(err)
	}
	var shown api.DeviceCode

	err := Link(context.Background(), site(t, 200, `{"access_token":"gf_new"}`), "gaming-pc", path,
		func(c api.DeviceCode) { shown = c })

	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if shown.UserCode != "BCDF-GHJK" || shown.VerificationURIComplete != "https://gold-forever.app/link?code=BCDF-GHJK" {
		t.Errorf("shown = %+v", shown)
	}
	cfg, _ := config.Load(path)
	if cfg.Token != "gf_new" || len(cfg.Folders) != 1 || len(cfg.Off) != 1 {
		t.Errorf("config = %+v", cfg)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestLinkLeavesTheSettingsAloneWhenDenied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{Token: "gf_old"}); err != nil {
		t.Fatal(err)
	}

	err := Link(context.Background(), site(t, 400, `{"error":"access_denied"}`), "pc", path, func(api.DeviceCode) {})

	if !errors.Is(err, api.ErrLinkDenied) {
		t.Errorf("err = %v", err)
	}
	if cfg, _ := config.Load(path); cfg.Token != "gf_old" {
		t.Errorf("token = %q, want the old one kept", cfg.Token)
	}
}

// The site says it in frontend/src/app/sharing.ts; a change there is a change here, word for word.
func TestTheSharingLineIsTheSitesOwn(t *testing.T) {
	const site = "Every upload adds its auction scans, item details and loot counts to the shared prices, " +
		"never your name. Your characters, gold and ledger stay private."
	if SharingLine != site {
		t.Errorf("SharingLine = %q", SharingLine)
	}
}
