package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/addonupdate"
	"github.com/gold-forever/companion/internal/ui/page"
)

const (
	checkAddonEvery = 6 * time.Hour
	addonWarning    = "The companion will download and install the Gold Forever addon from its GitHub releases whenever a new version is out, replacing the installed one. Turn it on?"
)

// ErrConfirmAddonUpdates is keeping the addon up to date turned on before the player confirmed the warning.
var ErrConfirmAddonUpdates error = refusal(addonWarning)

// SetAddonUpdates turns keeping the addon up to date on, once the player has confirmed the warning, or
// off, which stops any download under way.
func (c *App) SetAddonUpdates(ctx context.Context, on, confirmed bool) error {
	if on && !confirmed {
		return ErrConfirmAddonUpdates
	}
	return c.settings.Update(func(cfg *config.Config) error {
		cfg.AddonUpdates = on
		return nil
	})
}

// ErrUnknownAddonChannel is a channel other than release, beta or snapshot.
var ErrUnknownAddonChannel error = refusal("the addon's channel is release, beta or snapshot")

// SetAddonChannel picks which builds of the addon are installed: release, beta or snapshot.
func (c *App) SetAddonChannel(ctx context.Context, channel string) error {
	ch := addonupdate.Channel(channel)
	if !slices.Contains([]addonupdate.Channel{addonupdate.ChannelRelease, addonupdate.ChannelBeta, addonupdate.ChannelSnapshot}, ch) {
		return ErrUnknownAddonChannel
	}
	return c.settings.Update(func(cfg *config.Config) error {
		cfg.AddonChannel, cfg.AddonPrereleases = channel, false
		return nil
	})
}

// channelOf is the addon channel the settings pick: the one the player picked, else beta where the
// setting it replaced asked for pre-releases, else release.
func channelOf(cfg config.Config) addonupdate.Channel {
	switch {
	case cfg.AddonChannel != "":
		return addonupdate.Channel(cfg.AddonChannel)
	case cfg.AddonPrereleases:
		return addonupdate.ChannelBeta
	}
	return addonupdate.ChannelRelease
}

func (c *App) addonState() page.Addon {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.addon
	cfg := c.settings.Get()
	a.On, a.Warning, a.Channel = cfg.AddonUpdates, addonWarning, string(channelOf(cfg))
	return a
}

// addonWish is what the settings ask of the addon's updates; the check restarts when it changes.
type addonWish struct {
	on      bool
	channel addonupdate.Channel
}

func wishOf(cfg config.Config) addonWish { return addonWish{cfg.AddonUpdates, channelOf(cfg)} }

// keepAddonUpdated puts back what an interrupted install left, then, while the setting is on, checks
// for a newer addon now and every few hours. Turning it off ends the check and any download under way.
func (c *App) keepAddonUpdated(ctx context.Context) {
	c.recoverAddons()
	for ctx.Err() == nil {
		wish := wishOf(c.settings.Get())
		run, stop := context.WithCancel(ctx)
		var watching sync.WaitGroup
		watching.Go(func() { c.untilAddonWishChanges(run, wish); stop() })
		for wish.on && run.Err() == nil {
			c.checkAddon(run)
			select {
			case <-run.Done():
			case <-time.After(checkAddonEvery):
			}
		}
		<-run.Done()
		watching.Wait()
	}
}

func (c *App) untilAddonWishChanges(ctx context.Context, wish addonWish) {
	for {
		changed := c.settings.Changes()
		if wishOf(c.settings.Get()) != wish {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

// checkAddon installs the channel's latest addon build into every game folder whose installed addon it
// replaces, when the setting is on.
func (c *App) checkAddon(ctx context.Context) {
	cfg := c.settings.Get()
	if !cfg.AddonUpdates {
		return
	}
	channel := channelOf(cfg)
	latest, err := c.addons.Latest(ctx, channel)
	if err != nil {
		c.addonDone(ctx, "", err)
		return
	}
	var archive []byte
	var updated, added bool
	for _, dir := range c.addOnsFolders() {
		if !addonupdate.Wanted(channel, latest.Version, addonupdate.Installed(dir)) {
			continue
		}
		if archive == nil {
			if archive, err = c.addons.Download(ctx, latest); err != nil {
				break
			}
		}
		var folders []string
		if folders, err = addonupdate.Install(dir, archive); err != nil {
			break
		}
		updated, added = true, added || len(folders) > 0
	}
	switch {
	case err != nil:
		c.addonDone(ctx, "", err)
	case added:
		c.addonDone(ctx, fmt.Sprintf("Addon updated to v%s — restart the game: /reload does not load a new addon folder", latest.Version), nil)
	case updated:
		c.addonDone(ctx, fmt.Sprintf("Addon updated to v%s — /reload in game", latest.Version), nil)
	default:
		c.addonDone(ctx, fmt.Sprintf("The addon is up to date (v%s).", latest.Version), nil)
	}
}

// addonDone records how a check went, for the page and the tray; one that turning it off ended
// records nothing.
func (c *App) addonDone(ctx context.Context, status string, err error) {
	if ctx.Err() != nil {
		return
	}
	checked := c.now()
	c.mu.Lock()
	c.addon.Status, c.addon.Error, c.addon.Checked = status, "", &checked
	if err != nil {
		c.addon.Error = "Updating the addon failed: " + err.Error() + ". The installed addon is as it was."
	}
	c.mu.Unlock()
	if status != "" {
		c.status.Set(status)
	}
}

// recoverAddons puts back the old addon where an install was interrupted, and removes the backup of
// one that finished, now that the companion has started after it.
func (c *App) recoverAddons() {
	var errs []error
	restored := false
	for _, dir := range c.addOnsFolders() {
		r, err := addonupdate.Recover(dir)
		restored = restored || r
		errs = append(errs, err)
	}
	err := errors.Join(errs...)
	if !restored && err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addon.Error = "An addon update was interrupted: the previous addon is back."
	if err != nil {
		c.addon.Error = "Cleaning up after an addon update failed: " + err.Error() + "."
	}
}

// addOnsFolders is the AddOns folder of every game folder with a save, each once.
func (c *App) addOnsFolders() []string {
	var out []string
	for _, s := range c.saves() {
		dir := addondata.AddOns(filepath.Join(s.Install, s.Flavour))
		if !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}
