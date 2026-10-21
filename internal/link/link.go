// Package link connects this computer to a Gold Forever account: the player approves a code on the
// site, and the token that comes back is kept in the settings file. The command line's `link` and the
// settings page both use it.
package link

import (
	"context"

	"github.com/magnusev/gold-forever-companion/internal/api"
	"github.com/magnusev/gold-forever-companion/internal/config"
)

// Link asks the site for a code naming hostname, hands it to show for the player to approve, waits
// for the approval, and saves the token into the settings at cfgPath. Nothing is saved on failure.
func Link(ctx context.Context, c *api.Client, hostname, cfgPath string, show func(api.DeviceCode)) error {
	code, err := c.StartLink(ctx, hostname)
	if err != nil {
		return err
	}
	show(code)
	token, err := c.WaitForToken(ctx, code)
	if err != nil {
		return err
	}
	// Read the settings only now: the player may have changed them while the code waited.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	cfg.Token = token
	return config.Save(cfgPath, cfg)
}
