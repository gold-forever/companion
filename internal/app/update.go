package app

import (
	"context"
	"time"

	release "github.com/gold-forever/companion/internal/platform/version"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

// releaseAsker says which release of the companion is the latest.
type releaseAsker interface {
	Latest(ctx context.Context) (server.Release, error)
}

const checkVersionEvery = 24 * time.Hour

// checkVersion checks for an update once a day.
func (c *App) checkVersion(ctx context.Context) {
	asker := c.connect("")
	for {
		_ = c.checkForUpdate(ctx, asker)
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkVersionEvery):
		}
	}
}

// CheckForUpdate asks the server whether a newer release exists, for the page's notice.
func (c *App) CheckForUpdate(ctx context.Context) error {
	return c.checkForUpdate(ctx, c.connect(""))
}

func (c *App) checkForUpdate(ctx context.Context, asker releaseAsker) error {
	latest, err := asker.Latest(ctx)
	if err != nil {
		return err
	}
	if release.Newer(latest.Version, c.version) {
		c.mu.Lock()
		c.notice = &page.Notice{Version: latest.Version, URL: latest.URL}
		c.mu.Unlock()
	}
	return nil
}
