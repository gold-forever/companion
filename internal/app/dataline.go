package app

import (
	"fmt"
	"time"
)

// dataLine says when the game's data was last updated, and the event stream's state; "" before the
// first fetch.
func (c *App) dataLine() string {
	updated := c.prices.Updated()
	if updated.IsZero() {
		return ""
	}
	line := "Data for the game: updated " + ago(c.now().Sub(updated)) + " ago"
	if state := c.stream.String(); state != "" {
		line += " · " + state
	}
	return line
}

// ago is d rounded down to its largest unit: 12 s, 3 min, 2 h, 2 days.
func ago(d time.Duration) string {
	switch d = max(d, 0); {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}
