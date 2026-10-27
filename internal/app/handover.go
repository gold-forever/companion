package app

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/platform/instance"
)

const (
	// tabFile, in the update folder, is the page that asked for an update: its port and secret, which the
	// new copy takes over so that tab carries on with it.
	tabFile = "tab.json"
	// tabWaits is how long that tab waits for the new copy; a start later than that opens its own page.
	tabWaits = 2 * time.Minute
)

func tabPath(dataDir string) string { return filepath.Join(dataDir, updateDir, tabFile) }

// keepTab leaves the page's port and secret for the copy an update starts.
func keepTab(dataDir string, page instance.Info) error {
	return instance.Write(tabPath(dataDir), instance.Info{Port: page.Port, Secret: page.Secret})
}

// takeTab is the tab an update left, waiting since no earlier than tabWaits before now. It is taken once.
func takeTab(dataDir string, now time.Time) (instance.Info, bool) {
	path := tabPath(dataDir)
	stat, err := os.Stat(path)
	if err != nil {
		return instance.Info{}, false
	}
	tab, err := instance.Read(path)
	_ = os.Remove(path)
	if err != nil || now.Sub(stat.ModTime()) > tabWaits || tab.Port == 0 || tab.Secret == "" {
		return instance.Info{}, false
	}
	return tab, true
}
