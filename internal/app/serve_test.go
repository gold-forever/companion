package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/platform/instance"
)

// Quitting or uninstalling from the page answers it, then Serve returns with nothing it started still
// running: the page's server, the loops, and whatever the use cases left in the background.
func TestServeAnswersTheStopAndReturnsWithNothingLeftRunning(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the tray would show: only Linux can run without one")
	}
	for name, ask := range map[string]func(context.Context, instance.Info) error{
		"quit":      instance.Quit,
		"uninstall": uninstallFromThePage,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-bus")) // no tray
			layout := install.For(install.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
			dataDir := filepath.Join(home, "data")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			served := make(chan error, 1)
			go func() {
				served <- Serve(ctx, ServeOptions{
					Version: "v0.1.0", Site: "http://127.0.0.1:1", Layout: layout, Exe: layout.Exe, Home: home,
					DataDir: dataDir, Mode: "--autostart", OpenBrowser: func(string) error { return nil },
				})
			}()
			var running instance.Info
			for running.Port == 0 && ctx.Err() == nil {
				running, _ = instance.Read(filepath.Join(dataDir, InstanceFile))
				time.Sleep(5 * time.Millisecond)
			}

			if err := ask(ctx, running); err != nil {
				t.Errorf("the page got no answer: %v", err)
			}
			select {
			case err := <-served:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("Serve still runs")
			}
			if left := stillRunning(); left != "" {
				t.Fatalf("Serve returned with goroutines it started still running:\n%s", left)
			}
		})
	}
}

// uninstallFromThePage asks the running copy to uninstall itself, keeping the player's data, as the page
// does.
func uninstallFromThePage(ctx context.Context, i instance.Info) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/uninstall", i.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"keepData":true}`))
	if err != nil {
		return err
	}
	req.Header.Set(instance.SecretHeader, i.Secret)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the page answered %d", res.StatusCode)
	}
	return nil
}
