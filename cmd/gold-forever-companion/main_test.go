package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	code := run([]string{"--once", "--file", save(t), "--url", srv.URL}, func(k string) string {
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

	run([]string{"--once", "--file", save(t), "--url", srv.URL, "--token", "gf_flag"},
		func(string) string { return "gf_env" }, io.Discard, io.Discard)

	if *auth != "Bearer gf_flag" {
		t.Errorf("auth = %q", *auth)
	}
}

func TestNoTokenIsAUsageError(t *testing.T) {
	var errs bytes.Buffer

	code := run([]string{"--once", "--file", save(t)}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), "GF_TOKEN") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestARefusalExitsNonZeroWithItsReason(t *testing.T) {
	srv, _ := fakeServer(t, 400, `{"status":"unknown game","reason":"unknown game version 30403"}`)
	var errs bytes.Buffer

	code := run([]string{"--once", "--file", save(t), "--url", srv.URL, "--token", "t"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 1 || !strings.Contains(errs.String(), "unknown game version 30403") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestWithoutOnceItSaysWatchingIsNotBuiltYet(t *testing.T) {
	var errs bytes.Buffer

	code := run([]string{"--token", "t"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), "--once") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

func TestVersionPrintsTheBuildVersion(t *testing.T) {
	var out bytes.Buffer

	code := run([]string{"--version"}, func(string) string { return "" }, &out, io.Discard)

	if code != 0 || strings.TrimSpace(out.String()) != "gold-forever-companion "+version {
		t.Errorf("code = %d, out = %q", code, out.String())
	}
}
