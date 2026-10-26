package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/app"
	"github.com/gold-forever/companion/internal/install"
	"github.com/gold-forever/companion/internal/instance"
	release "github.com/gold-forever/companion/internal/version"
)

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
	instancePath := filepath.Join(dataDir, app.InstanceFile)
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
		err = serveAlone(ctx, layout, exe, home, dataDir, instancePath, mode)
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

// How long a launch waits for another copy that holds the claim: to show its page once it is up, or to
// take over once it has quit for an update.
const claimWait = 15 * time.Second

// serveAlone serves while holding the one-copy claim. Two launches at once (autostart and a click)
// both find no running copy; the one without the claim opens the other's page instead.
func serveAlone(ctx context.Context, layout install.Layout, exe, home, dataDir, instancePath, mode string) error {
	for deadline := time.Now().Add(claimWait); ; time.Sleep(200 * time.Millisecond) {
		release, err := instance.Claim(dataDir)
		if err == nil {
			defer release()
			return app.Serve(ctx, app.ServeOptions{
				Version: version, Site: defaultURL, Layout: layout, Exe: exe, Home: home, DataDir: dataDir, Mode: mode,
				OpenBrowser: openBrowser,
			})
		}
		if !errors.Is(err, instance.ErrRunning) {
			return fmt.Errorf("could not make sure only one companion runs: %w", err)
		}
		if other, err := instance.Read(instancePath); err == nil && instance.Alive(other) {
			return openBrowser(instance.PageURL(other))
		}
		if time.Now().After(deadline) {
			return errors.New("another copy of the companion is starting but does not answer: try again in a moment")
		}
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
