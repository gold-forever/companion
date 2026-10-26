package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/platform/instance"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/ui/page"
	"github.com/gold-forever/companion/internal/ui/tray"
)

// ServeOptions is what the installed companion runs with.
type ServeOptions struct {
	Version, Site string
	Layout        install.Layout
	// Exe is the running executable; Home and DataDir are the player's home and the companion's data folder.
	Exe, Home, DataDir string
	// Mode is "" for a double-click, --installed when a download starts the installed copy, and
	// --autostart at login, which stays quiet in the tray.
	Mode string
	// OpenBrowser opens a page in the player's browser.
	OpenBrowser func(url string) error
}

// Serve runs the installed companion until Quit, Uninstall or ctx's end. The caller holds the one-copy
// claim.
func Serve(ctx context.Context, o ServeOptions) error {
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
	instancePath := InstancePath(o.DataDir)
	c := newApp(appOptions{
		version: o.Version, site: o.Site, running: o.Exe, layout: o.Layout, cfgPath: cfgPath, dataDir: o.DataDir,
		candidates: find.Candidates(goos, o.Home, os.Getenv, find.RegistryInstall), quit: stop, life: ctx,
	})
	c.status.out = os.Stdout
	c.openURL = func(url string) { _ = o.OpenBrowser(url) }
	_ = c.log.Prune(time.Now())
	_ = o.Layout.FollowApp(o.Exe, !c.settings.Get().Welcomed)

	info := instance.Info{PID: os.Getpid(), Port: port, Secret: hex.EncodeToString(secret), Version: o.Version}
	srv := &http.Server{Handler: page.New(c, info.Secret, port), ReadHeaderTimeout: 10 * time.Second}
	defer func() {
		stop()
		shutdown(srv)
		c.owned.Wait()
	}()
	c.owned.Go(func() { _ = srv.Serve(listener) })
	if err := instance.Write(instancePath, info); err != nil {
		return err
	}
	defer removeIfOurs(instancePath, info)
	c.owned.Go(func() { c.loop(ctx) })
	c.owned.Go(func() { c.checkVersion(ctx) })

	pageURL := instance.PageURL(info)
	c.pageURL = pageURL
	withTray := tray.Available()
	if o.Mode != "--autostart" || !withTray {
		_ = o.OpenBrowser(pageURL)
	}
	if !withTray {
		<-ctx.Done()
		return nil
	}
	tray.Run(ctx, c)
	return nil
}

// answerFor is how long the page's requests in flight, a quit's among them, have to get their answer
// once the companion stops.
const answerFor = 2 * time.Second

// shutdown stops the page, letting the requests in flight answer first; after that, no use case starts
// anything more.
func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), answerFor)
	defer cancel()
	if srv.Shutdown(ctx) != nil {
		_ = srv.Close()
	}
}

// removeIfOurs removes the instance file unless a newer copy has written its own since.
func removeIfOurs(path string, ours instance.Info) {
	if now, err := instance.Read(path); err == nil && now.Secret == ours.Secret {
		_ = os.Remove(path)
	}
}
