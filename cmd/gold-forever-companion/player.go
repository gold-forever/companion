package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gold-forever/companion/internal/config"
	"github.com/gold-forever/companion/internal/find"
	"github.com/gold-forever/companion/internal/install"
	"github.com/gold-forever/companion/internal/instance"
	"github.com/gold-forever/companion/internal/page"
	"github.com/gold-forever/companion/internal/tray"
	release "github.com/gold-forever/companion/internal/version"
)

const instanceFile = "instance.json"

var goos = runtime.GOOS

// The tray must run on the main thread on macOS, which only main's own goroutine can keep.
func init() { runtime.LockOSThread() }

// player is what a double-click does. A download installs itself and starts the installed copy; the
// installed copy shows the running one's page if there is one, and otherwise runs: the page, the
// tray and the uploads. mode is "" for a double-click, --installed when a download starts the
// installed copy, and --autostart at login, which stays quiet in the tray.
func player(ctx context.Context, mode string) int {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
	}
	home, _ := os.UserHomeDir()
	layout := install.For(install.Env{GOOS: goos, Home: home, Getenv: os.Getenv, RunKey: install.RunKey()})
	dataDir := config.DataDir(goos, home, os.Getenv)
	instancePath := filepath.Join(dataDir, instanceFile)
	other, err := instance.Read(instancePath)
	alive := err == nil && instance.Alive(other)
	switch {
	// Only a double-click installs: a copy started --installed or --autostart is the installed one, so it
	// never hands off again, whatever a path comparison says (a symlinked home once made that a loop).
	case mode == "" && layout.NeedsInstall(exe):
		err = installFromDownload(layout, exe, other, alive)
	case alive:
		err = openBrowser(instance.PageURL(other))
	default:
		err = serve(ctx, layout, exe, home, dataDir, instancePath, mode)
	}
	if err != nil {
		alert("Gold Forever Companion", err.Error())
		return 1
	}
	return 0
}

// installFromDownload installs the download when nothing is installed or it is newer, stopping the
// running copy first; a download no newer than what is installed only starts or shows that.
func installFromDownload(l install.Layout, exe string, other instance.Info, alive bool) error {
	_, statErr := os.Stat(l.Exe)
	fresh := os.IsNotExist(statErr)
	installed := other.Version
	if !alive && !fresh {
		installed = versionOf(l.Exe)
	}
	if !fresh && !release.Newer(version, installed) {
		if alive {
			return openBrowser(instance.PageURL(other))
		}
		return start(l.Exe, "--installed")
	}
	if alive {
		if err := instance.Quit(other); err != nil {
			return fmt.Errorf("could not stop the running companion to update it: %w", err)
		}
		for deadline := time.Now().Add(10 * time.Second); instance.Alive(other) && time.Now().Before(deadline); {
			time.Sleep(200 * time.Millisecond)
		}
	}
	autostartWasOn := l.Autostart.Enabled()
	if err := l.Install(exe); err != nil {
		return fmt.Errorf("could not install itself: %w", err)
	}
	// An update keeps the player's choice; only a first install turns autostart on for them.
	if !fresh && !autostartWasOn {
		_ = l.Autostart.Disable()
	}
	return start(l.Exe, "--installed")
}

// versionOf asks an installed companion its version: "" when it does not answer.
func versionOf(exe string) string {
	out, err := exec.Command(exe, "--version").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

// serve runs the installed companion until Quit, Uninstall or ctx's end.
func serve(ctx context.Context, layout install.Layout, exe, home, dataDir, instancePath, mode string) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	listener, port, err := page.Listen()
	if err != nil {
		return fmt.Errorf("could not open the settings page: %w", err)
	}
	cfgPath, err := config.Path()
	if err != nil {
		return err
	}
	c := newCompanion(companionOptions{
		version: version, site: defaultURL, running: exe, layout: layout, cfgPath: cfgPath, dataDir: dataDir,
		candidates: find.Candidates(goos, home, os.Getenv, find.RegistryInstall), quit: stop,
	})
	c.status.out = os.Stdout
	c.openURL = func(url string) { _ = openBrowser(url) }
	_ = c.log.Prune(time.Now())
	// macOS has no install step: autostart is turned on at the first run, as installing does elsewhere.
	if layout.Exe == "" && !c.settings().Welcomed && !layout.Autostart.Enabled() {
		_ = layout.Autostart.Enable(exe)
	}

	info := instance.Info{PID: os.Getpid(), Port: port, Secret: hex.EncodeToString(secret), Version: version}
	srv := &http.Server{Handler: page.New(c, info.Secret, port), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(listener)
	defer srv.Close()
	if err := instance.Write(instancePath, info); err != nil {
		return err
	}
	defer removeIfOurs(instancePath, info)
	go c.loop(ctx)
	go c.checkVersion(ctx)

	pageURL := instance.PageURL(info)
	withTray := tray.Available()
	if mode != "--autostart" || !withTray {
		_ = openBrowser(pageURL)
	}
	if !withTray {
		<-ctx.Done()
		return nil
	}
	go func() {
		<-ctx.Done()
		tray.Stop()
	}()
	tray.Run(tray.Menu{
		Status: func() string {
			if s := c.status.String(); s != "" {
				return s
			}
			return "Starting…"
		},
		OpenSite: func() { _ = openBrowser(c.url()) },
		Settings: func() { _ = openBrowser(pageURL) },
		Quit:     stop,
	})
	return nil
}

// removeIfOurs removes the instance file unless a newer copy has written its own since.
func removeIfOurs(path string, ours instance.Info) {
	if now, err := instance.Read(path); err == nil && now.Secret == ours.Secret {
		_ = os.Remove(path)
	}
}

// start runs exe on its own, without waiting for it.
func start(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start %s: %w", exe, err)
	}
	return cmd.Process.Release()
}

// openBrowser opens url in the player's default browser.
func openBrowser(url string) error {
	switch goos {
	case "windows":
		return start("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		return start("open", url)
	default:
		return start("xdg-open", url)
	}
}
