package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gold-forever/companion/internal/account/config"
)

const httpElsewhere = "http://gold-forever.example"

func TestOnceRefusesAnHTTPSiteElsewhere(t *testing.T) {
	var errs bytes.Buffer

	code := run(context.Background(), []string{"--once", "--file", save(t), "--url", httpElsewhere, "--token", "t"}, func(string) string { return "" }, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), httpElsewhere+" is http") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
}

// Linking to an http site elsewhere is refused before anything is asked or saved.
func TestLinkRefusesAnHTTPSiteElsewhere(t *testing.T) {
	isolatedHome(t)
	var errs bytes.Buffer

	code := run(context.Background(), []string{"link", "--url", httpElsewhere}, os.Getenv, io.Discard, &errs)

	if code != 2 || !strings.Contains(errs.String(), httpElsewhere+" is http") {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("the settings were written: %v", err)
	}
}

// Settings saved by an older companion may hold an http site; run says so and uploads nothing,
// leaving the file as it is for the player to fix.
func TestRunRefusesASavedHTTPSiteElsewhere(t *testing.T) {
	isolatedHome(t)
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(cfgPath, config.Config{Token: "gf_t", URL: httpElsewhere}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cfgPath)
	var errs bytes.Buffer

	code := run(context.Background(), []string{"run"}, os.Getenv, io.Discard, &errs)

	if code != 1 || !strings.Contains(errs.String(), httpElsewhere+" is http") || !strings.Contains(errs.String(), cfgPath) {
		t.Errorf("code = %d, stderr = %q", code, errs.String())
	}
	if after, _ := os.ReadFile(cfgPath); !bytes.Equal(after, before) {
		t.Errorf("the settings were rewritten: %s", after)
	}
}
