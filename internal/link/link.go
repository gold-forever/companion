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
	return Run(ctx, c, hostname, show, func(token string) error {
		// Read the settings only now: the player may have changed them while the code waited.
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		cfg.Token = token
		return config.Save(cfgPath, cfg)
	})
}

// Run is Link with the saving left to save, for the running companion, which keeps its settings itself.
func Run(ctx context.Context, c *api.Client, hostname string, show func(api.DeviceCode), save func(token string) error) error {
	code, err := c.StartLink(ctx, hostname)
	if err != nil {
		return err
	}
	show(code)
	token, err := c.WaitForToken(ctx, code)
	if err != nil {
		return err
	}
	return save(token)
}
