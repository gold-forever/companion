package app

import (
	"context"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

// The window and the tray say how fresh the game's data is and whether the stream is up.
func TestThePageAndTheTraySayWhenTheGameDataWasUpdatedAndTheStreamsState(t *testing.T) {
	c, _ := fixture(t)
	if got := c.State().Data; got != "" {
		t.Fatalf("before any fetch the page says %q", got)
	}
	c.prices.Now = func() time.Time { return t0 }
	if err := c.refresher(pricesServer{twoItems}, sharedServer{`{"v":1,"at":7,"lists":[],"profiles":[]}`}).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return t0.Add(12 * time.Second) }
	c.stream.set(streamConnected)
	c.status.Set("prices for the account's forever markets written into /games/wow/_forever_ (1 markets, 2 items)")

	want := "Data for the game: updated 12 s ago · connected"
	if got := c.State().Data; got != want {
		t.Errorf("page %q, want %q", got, want)
	}
	if got := c.Status(); got != want {
		t.Errorf("tray %q, want %q", got, want)
	}
	c.stream.set(streamReconnecting)
	c.status.Set("12:00:13 sent GoldForever.lua: imported")
	if got := c.Status(); got != "12:00:13 sent GoldForever.lua: imported" {
		t.Errorf("tray after an upload %q; the upload's line comes first", got)
	}
	if got := c.State().Data; got != "Data for the game: updated 12 s ago · reconnecting" {
		t.Errorf("page while reconnecting %q", got)
	}
}

func TestHowLongAgoTheGameDataWasUpdatedIsRoundedDownToItsUnit(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                            "0 s",
		59 * time.Second:             "59 s",
		3*time.Minute + 59e9:         "3 min",
		2*time.Hour + 59*time.Minute: "2 h",
		50 * time.Hour:               "2 days",
		-time.Second:                 "0 s", // a clock set back
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%s) = %q, want %q", d, got, want)
		}
	}
}
