package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/platform/atomicfile"
)

// remindEvery is how often the player is reminded again of a release they have not installed.
const remindEvery = 7 * 24 * time.Hour

// reminded is the release the player was last reminded of, and when.
type reminded struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// due says whether version is worth a reminder: one the player has not been reminded of, or not for a week.
func (r reminded) due(version string, now time.Time) bool {
	return version != r.Version || now.Sub(r.At) >= remindEvery
}

// remindOfUpdate asks the player on the desktop whether to update, when a newer release is due a
// reminder. Later, or closing it, counts as an answer: the reminder is recorded before it is asked.
func (c *App) remindOfUpdate(ctx context.Context) {
	version := c.UpdateVersion()
	path := filepath.Join(c.dataDir, remindedFile)
	var last reminded
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &last)
	}
	now := c.now()
	if version == "" || !last.due(version, now) {
		return
	}
	data, _ := json.Marshal(reminded{version, now})
	if atomicfile.Write(path, data) != nil {
		return // a reminder that cannot be recorded would come back every day
	}
	// The desktop's question may not end with the companion, so it is waited for only until then.
	answer := make(chan bool, 1)
	go func() { answer <- c.ask(ctx, "Version "+version+" of the companion is out.") }()
	select {
	case yes := <-answer:
		if yes {
			c.UpdateNow()
		}
	case <-ctx.Done():
	}
}
