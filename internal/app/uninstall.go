package app

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Uninstall removes the companion and everything it put on the computer, then stops it.
func (c *App) Uninstall(ctx context.Context, keepData bool) error {
	err := c.layout.Uninstall(c.running, keepData)
	_ = os.Remove(filepath.Join(c.dataDir, InstanceFile))
	go func() {
		time.Sleep(300 * time.Millisecond) // let the page get its answer first
		c.quit()
	}()
	return err
}

func (c *App) Quit() { c.quit() }
