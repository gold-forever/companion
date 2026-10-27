package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
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
				running, _ = instance.Read(filepath.Join(dataDir, instanceFile))
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

// serving runs Serve as installed in home, started in mode, until ctx ends: what its instance file
// records once its page listens, the pages it opened (to read once it has returned), and its result.
func serving(ctx context.Context, t *testing.T, home, version, mode string) (instance.Info, *[]string, chan error) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the tray would show: only Linux can run without one")
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-bus")) // no tray
	layout := install.For(install.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
	dataDir := filepath.Join(home, "data")
	os.Remove(InstancePath(dataDir))
	opened := &[]string{}
	served := make(chan error, 1)
	go func() {
		served <- Serve(ctx, ServeOptions{
			Version: version, Site: "http://127.0.0.1:1", Layout: layout, Exe: layout.Exe, Home: home,
			DataDir: dataDir, Mode: mode, OpenBrowser: func(url string) error { *opened = append(*opened, url); return nil },
		})
	}()
	var running instance.Info
	for running.Port == 0 && ctx.Err() == nil {
		running, _ = instance.Read(InstancePath(dataDir))
		time.Sleep(5 * time.Millisecond)
	}
	return running, opened, served
}

// stateFor asks the page on port for its state with the cookie the tab holds for secret: the version
// that answers, or "" when none does.
func stateFor(ctx context.Context, port int, secret string) string {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/state", port), nil)
	req.AddCookie(&http.Cookie{Name: fmt.Sprintf("companion_%d", port), Value: secret})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var s struct{ Version string }
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&s) != nil {
		return ""
	}
	return s.Version
}

// The restart handover: the tab that asked for the update keeps its address and cookie, and the new copy
// answers it there, without opening a second tab.
func TestTheNewCopyCarriesOnInTheTabThatAskedForTheUpdate(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	old, _, oldServed := serving(ctx, t, home, "v0.2.0-rc.1", "--autostart")
	if err := keepTab(filepath.Join(home, "data"), old); err != nil {
		t.Fatal(err)
	}
	if err := instance.Quit(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := <-oldServed; err != nil {
		t.Fatal(err)
	}

	running, opened, served := serving(ctx, t, home, "v0.2.0-rc.2", "--installed")

	if running.Port != old.Port || running.Secret != old.Secret {
		t.Errorf("the new copy listens at %d, the tab at %d", running.Port, old.Port)
	}
	if v := stateFor(ctx, old.Port, old.Secret); v != "v0.2.0-rc.2" {
		t.Errorf("the tab is answered by %q", v)
	}
	instance.Quit(ctx, running)
	<-served
	if len(*opened) != 0 {
		t.Errorf("the new copy opened %v beside the tab", *opened)
	}
}

// With no tab waiting, one whose port another program has taken since, or when a double-click starts it
// rather than an update, the new copy opens its own page.
func TestTheNewCopyOpensItsOwnPageWhenNoTabCanCarryOn(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := free.Addr().(*net.TCPAddr).Port
	free.Close()
	for name, start := range map[string]struct {
		mode string
		tab  *instance.Info
	}{
		"no tab":         {"--installed", nil},
		"its port taken": {"--installed", &instance.Info{Port: busy.Addr().(*net.TCPAddr).Port, Secret: "0123456789abcdef"}},
		"a double-click": {"", &instance.Info{Port: freePort, Secret: "0123456789abcdef"}},
	} {
		mode, tab := start.mode, start.tab
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			if tab != nil {
				if err := keepTab(filepath.Join(home, "data"), *tab); err != nil {
					t.Fatal(err)
				}
			}

			running, opened, served := serving(ctx, t, home, "v0.2.0-rc.2", mode)

			if tab != nil && (running.Port == tab.Port || running.Secret == tab.Secret) {
				t.Errorf("listens as the tab it could not take: %+v", running)
			}
			instance.Quit(ctx, running)
			<-served
			if len(*opened) != 1 || (*opened)[0] != instance.PageURL(running) {
				t.Errorf("opened %v", *opened)
			}
		})
	}
}
