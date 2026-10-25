//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/install"
	"github.com/gold-forever/companion/internal/instance"
)

// A double-clicked download that is no newer starts the installed copy as the installed one: started
// with no arguments, a copy whose own path compares unequal would hand off to itself for ever.
func TestADownloadNoNewerThanTheInstalledCopyStartsItAsInstalled(t *testing.T) {
	l := install.For(install.Env{GOOS: "linux", Home: t.TempDir(), Getenv: func(string) string { return "" }})
	started := filepath.Join(t.TempDir(), "started")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --version ]; then echo gold-forever-companion v9.9.9; exit 0; fi\n" +
		"echo \"$*\" > '" + started + ".tmp' && mv '" + started + ".tmp' '" + started + "'\n"
	if err := os.MkdirAll(filepath.Dir(l.Exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installFromDownload(l, filepath.Join(t.TempDir(), "gold-forever-companion"), instance.Info{}, false); err != nil {
		t.Fatal(err)
	}

	var args []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(started); err == nil {
			args = data
			break
		}
	}
	if string(args) != "--installed\n" {
		t.Fatalf("the installed copy was started with %q, want --installed", args)
	}
}
