package app

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The tray's menu shows one short line; the page keeps the whole line, with its folder.
func TestTheTrayStatusIsShortAndThePageKeepsTheWholeLine(t *testing.T) {
	for line, want := range map[string]string{
		"prices for the account's retail markets written into /home/player/Games/World of Warcraft/_retail_ (3 markets, 36317 items)\n": "Prices written: 3 markets, 36,317 items",
		"prices for forever-eu written into /games/wow/_forever_ (1 markets, 812 items)\n":                                              "Prices written: 1 market, 812 items",
		"prices not written into /home/player/Games/World of Warcraft/_retail_: permission denied\n":                                    "Prices not written: see Settings",
		"12:00:01 uploaded GoldForever.lua\n": "12:00:01 uploaded GoldForever.lua",
	} {
		c, _ := fixture(t)
		c.status.Write([]byte(line))

		if got := c.Status(); got != want {
			t.Errorf("tray %q, want %q", got, want)
		}
		if got := c.State().Status; got != strings.TrimSpace(line) {
			t.Errorf("page %q", got)
		}
	}
}

func TestALongStatusLineIsCutForTheTray(t *testing.T) {
	c, _ := fixture(t)
	c.status.Write([]byte(strings.Repeat("a long line ", 20) + "\n"))

	if got := c.Status(); utf8.RuneCountInString(got) > maxTrayStatus || !strings.HasSuffix(got, "…") {
		t.Errorf("%d runes: %q", utf8.RuneCountInString(got), got)
	}
}
