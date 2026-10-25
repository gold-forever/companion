package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/config"
	"github.com/gold-forever/companion/internal/instance"
	"github.com/gold-forever/companion/internal/link"
)

func save(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "GoldForever.lua")
	if err := os.WriteFile(path, []byte("GoldForeverDB = {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeServer(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestOnceUploadsTheFileAndPrintsTheAnswer(t *testing.T) {
	srv, auth := fakeServer(t, 200, `{"status":"imported","problems":0,"saves":3}`)
	var out, errs bytes.Buffer

	code := run(context.Background(), []string{"--once", "--file", save(t), "--url", srv.URL}, func(k string) string {
		return map[string]string{"GF_TOKEN": "gf_env"}[k]
	}, &out, &errs)

	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errs.String())
	}
	if *auth != "Bearer gf_env" {
		t.Errorf("auth = %q", *auth)
	}
	if !strings.Contains(out.String(), "imported") || !strings.Contains(out.String(), "3 saves") {
		t.Errorf("out = %q", out.String())
	}
}

func TestTheTokenFlagWinsOverTheEnvironment(t *testing.T) {
	srv, auth := fakeServer(t, 200, `{"status":"imported"}`)

	run(context.Background(), []string{"--once", "--file", save(t), "--url", srv.URL, "--token", "gf_flag"},
		func(string) string { return "gf_env" }, io.Discard, io.Discard)

	if *auth != "Bearer gf_flag" {
		t.Errorf("auth = %q", *auth)
	}
}

func TestNoTokenIsAUsageError(t *testing.T) {
	var errs bytes.Buffer

	code := run(context.Background(), []string{"--once", "--file", save(t)}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), "GF_TOKEN") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestARefusalExitsNonZeroWithItsReason(t *testing.T) {
	srv, _ := fakeServer(t, 400, `{"status":"unknown game","reason":"unknown game version 30403"}`)
	var errs bytes.Buffer

	code := run(context.Background(), []string{"--once", "--file", save(t), "--url", srv.URL, "--token", "t"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 1 || !strings.Contains(errs.String(), "unknown game version 30403") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestWithoutACommandItShowsTheUsage(t *testing.T) {
	var errs bytes.Buffer

	code := run(context.Background(), []string{"--token", "t"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), "run") || !strings.Contains(errs.String(), "--once") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

// isolatedHome points every per-user folder the companion uses, on any system, at a new temporary
// home, so a test never reads or writes the real settings (macOS ignores the XDG variables).
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	return home
}

func TestRunWithoutATokenSaysHowToGetOne(t *testing.T) {
	isolatedHome(t)
	var errs bytes.Buffer

	code := run(context.Background(), []string{"run"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), "GF_TOKEN") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestRunFindsTheSavesInAConfiguredFolderAndUploadsThem(t *testing.T) {
	home := isolatedHome(t)
	wow := filepath.Join(home, "elsewhere", "World of Warcraft")
	dir := filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte("GoldForeverDB = {\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	t.Cleanup(srv.Close)
	// The app's own answers, so the test holds on every system (macOS ignores XDG_CONFIG_HOME).
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(cfgPath, config.Config{Token: "gf_t", URL: srv.URL, Folders: []string{wow}}); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(config.DataDir(runtime.GOOS, home, os.Getenv), "sent.jsonl")
	go func() {
		// Stop once the answer is in the log, as a player would close the companion.
		for ctx.Err() == nil {
			if _, err := os.Stat(logPath); err == nil {
				cancel()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var out bytes.Buffer

	code := run(ctx, []string{"run"}, os.Getenv, &out, io.Discard)

	if code != 0 || !strings.Contains(out.String(), "sent World of Warcraft › _forever_ › A: imported") {
		t.Fatalf("code = %d, out = %q", code, out.String())
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("no log was written: %v", err)
	}
}

// The tray app and run share one backups folder and log: run must not start beside a running copy (CO-07).
func TestRunDoesNotStartWhileAnotherCopyHoldsTheClaim(t *testing.T) {
	home := isolatedHome(t)
	var uploads int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uploads++
		mu.Unlock()
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	t.Cleanup(srv.Close)
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(cfgPath, config.Config{Token: "gf_t", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	release, err := instance.Claim(config.DataDir(runtime.GOOS, home, os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var errs bytes.Buffer

	code := run(ctx, []string{"run"}, os.Getenv, io.Discard, &errs)

	if code == 0 || !strings.Contains(errs.String(), "already running") {
		t.Fatalf("code = %d, stderr = %q", code, errs.String())
	}
	if ctx.Err() != nil {
		t.Error("run kept running beside the other copy until the test stopped it")
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 0 {
		t.Errorf("%d requests reached the server", uploads)
	}
}

func TestVersionPrintsTheBuildVersion(t *testing.T) {
	var out bytes.Buffer

	code := run(context.Background(), []string{"--version"}, func(string) string { return "" }, &out, io.Discard)

	if code != 0 || strings.TrimSpace(out.String()) != "gold-forever-companion "+version {
		t.Errorf("code = %d, out = %q", code, out.String())
	}
}

func TestLinkShowsTheCodeAndSavesTheApprovedTokenWithoutPrintingIt(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/api/v1/device/code" {
			_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"BCDF-GHJK","verification_uri":"https://gold-forever.app/link",`+
				`"verification_uri_complete":"https://gold-forever.app/link?code=BCDF-GHJK","expires_in":600,"interval":5}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"gf_new_secret"}`)
	}))
	t.Cleanup(srv.Close)
	pollSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { pollSleep = nil })
	var out bytes.Buffer

	code := run(context.Background(), []string{"link", "--url", srv.URL}, os.Getenv, &out, io.Discard)

	if code != 0 {
		t.Fatalf("code = %d, out = %q", code, out.String())
	}
	for _, want := range []string{"BCDF-GHJK", "https://gold-forever.app/link?code=BCDF-GHJK", link.SharingLine} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "gf_new_secret") {
		t.Errorf("the token was printed: %q", out.String())
	}
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)
	if cfg.Token != "gf_new_secret" || cfg.URL != srv.URL {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLinkThatIsDeniedSaysSoAndFails(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/api/v1/device/code" {
			_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"BCDF-GHJK","expires_in":600,"interval":5}`)
			return
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":"access_denied"}`)
	}))
	t.Cleanup(srv.Close)
	pollSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { pollSleep = nil })
	var errs bytes.Buffer

	code := run(context.Background(), []string{"link", "--url", srv.URL}, os.Getenv, io.Discard, &errs)

	if code != 1 || !strings.Contains(errs.String(), "denied") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestRunWritesTheMarketsPricesIntoTheForeverFlavourAtStart(t *testing.T) {
	for _, viaFlag := range []bool{true, false} {
		home := isolatedHome(t)
		wow := filepath.Join(home, "World of Warcraft")
		dir := filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		save := "GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte(save), 0o600); err != nil {
			t.Fatal(err)
		}
		const market = "forever-BETA-normal-horde"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/prices" && r.URL.Query().Get("market") == market {
				_, _ = io.WriteString(w, `{"v":1,"game":"forever","market":"`+market+`","at":5,"items":[{"id":2589,"value":1234,"minBuyout":1100,"players":1,"scannedAt":4}]}`)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
		}))
		cfgPath, _ := config.Path()
		cfg := config.Config{Token: "gf_t", URL: srv.URL, Folders: []string{wow}}
		args := []string{"run", "--market", market}
		if !viaFlag {
			cfg.Market, args = market, []string{"run"}
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(wow, "_forever_", "Interface", "AddOns", "GoldForever_Data", "Data.lua")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		go func() {
			for ctx.Err() == nil {
				if _, err := os.Stat(data); err == nil {
					cancel()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()

		code := run(ctx, args, os.Getenv, io.Discard, io.Discard)
		cancel()
		srv.Close()

		lua, err := os.ReadFile(data)
		if code != 0 || err != nil || !strings.Contains(string(lua), "[2589] = { 1234, 1100, 1, 4 },") {
			t.Fatalf("flag %v: code = %d, Data.lua = %q, %v", viaFlag, code, lua, err)
		}
	}
}

func TestRunAsksForThePricesAgainWithTheirETagOnceAnUploadIsImported(t *testing.T) {
	home := isolatedHome(t)
	wow := filepath.Join(home, "World of Warcraft")
	dir := filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := "GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte(save), 0o600); err != nil {
		t.Fatal(err)
	}
	const market = "forever-BETA-normal-horde"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var mu sync.Mutex
	var sent []string // If-None-Match of each price request, in order
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/prices" {
			mu.Lock()
			sent = append(sent, r.Header.Get("If-None-Match"))
			n := len(sent)
			mu.Unlock()
			if n > 1 {
				w.WriteHeader(http.StatusNotModified)
				cancel()
				return
			}
			w.Header().Set("ETag", `"m-1"`)
			_, _ = io.WriteString(w, `{"v":1,"game":"forever","market":"`+market+`","at":5,"items":[]}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	defer srv.Close()
	cfgPath, _ := config.Path()
	if err := config.Save(cfgPath, config.Config{Token: "gf_t", URL: srv.URL, Folders: []string{wow}, Market: market}); err != nil {
		t.Fatal(err)
	}

	code := run(ctx, []string{"run"}, os.Getenv, io.Discard, io.Discard)

	if left := stillRunning(); left != "" {
		t.Fatalf("run returned with its loops still running:\n%s", left)
	}
	mu.Lock()
	defer mu.Unlock()
	if code != 0 || len(sent) != 2 || sent[0] != "" || sent[1] != `"m-1"` {
		t.Fatalf("code = %d, If-None-Match sent = %q; want a full fetch at start, then one with its ETag", code, sent)
	}
}

// stillRunning is the stack of every goroutine inside the upload or price loops: "" when none is.
func stillRunning() string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var left []string
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "companion/internal/prices.") || strings.Contains(g, "companion/internal/core.") {
			left = append(left, g)
		}
	}
	return strings.Join(left, "\n\n")
}

func TestRunWithoutAMarketWritesTheAccountsMarkets(t *testing.T) {
	home := isolatedHome(t)
	wow := filepath.Join(home, "World of Warcraft")
	dir := filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := "GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "GoldForever.lua"), []byte(save), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/prices" && r.URL.Query().Get("game") == "forever" && !r.URL.Query().Has("market") {
			_, _ = io.WriteString(w, `{"v":1,"game":"forever","at":5,"markets":[`+
				`{"market":"forever-EU-normal-horde","items":[{"id":2589,"value":1234,"minBuyout":1100,"players":1,"scannedAt":4}]},`+
				`{"market":"forever-EU-normal-alliance","items":[{"id":2589,"value":99,"minBuyout":90,"players":2,"scannedAt":4}]}]}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	defer srv.Close()
	cfgPath, _ := config.Path()
	if err := config.Save(cfgPath, config.Config{Token: "gf_t", URL: srv.URL, Folders: []string{wow}}); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(wow, "_forever_", "Interface", "AddOns", "GoldForever_Data", "Data.lua")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(data); err == nil {
				cancel()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	code := run(ctx, []string{"run"}, os.Getenv, io.Discard, io.Discard)

	lua, err := os.ReadFile(data)
	if code != 0 || err != nil || !strings.Contains(string(lua), `["forever-EU-normal-horde"]`) || !strings.Contains(string(lua), `["forever-EU-normal-alliance"]`) {
		t.Fatalf("code = %d, Data.lua = %q, %v", code, lua, err)
	}
}
