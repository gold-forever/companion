package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// sent logs n uploads, a minute apart, the newest now: upload 1 is the oldest.
func sent(t *testing.T, c *App, from, n int) {
	t.Helper()
	for i := from; i < from+n; i++ {
		e := sendlog.Entry{Time: time.Now().Add(time.Duration(i) * time.Minute), Save: fmt.Sprintf("upload %d", i), Answer: "stored"}
		if err := c.log.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

func shown(entries []sendlog.Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Save)
	}
	return out
}

func TestWhatWasSentShowsTenPerPageNewestFirst(t *testing.T) {
	c, _ := fixture(t)
	sent(t, c, 1, 25)

	first, last := c.History(1), c.History(3)

	if got := shown(first.Entries); len(got) != 10 || got[0] != "upload 25" || got[9] != "upload 16" {
		t.Errorf("page 1: %v", got)
	}
	if got := shown(last.Entries); len(got) != 5 || got[0] != "upload 5" || got[4] != "upload 1" {
		t.Errorf("page 3: %v", got)
	}
	if first.Page != 1 || first.Pages != 3 || last.Page != 3 {
		t.Errorf("page %d and %d of %d", first.Page, last.Page, first.Pages)
	}
}

func TestAPageOutsideTheLogIsItsNearestPage(t *testing.T) {
	c, _ := fixture(t)
	sent(t, c, 1, 20)
	for asked, want := range map[int]int{0: 1, -3: 1, 2: 2, 3: 2, 99: 2} {
		if h := c.History(asked); h.Page != want || h.Pages != 2 || len(h.Entries) != 10 {
			t.Errorf("page %d: page %d of %d, %d entries", asked, h.Page, h.Pages, len(h.Entries))
		}
	}
}

func TestAnEmptyLogIsOneEmptyPage(t *testing.T) {
	c, _ := fixture(t)
	if h := c.History(1); h.Page != 1 || h.Pages != 1 || len(h.Entries) != 0 {
		t.Errorf("page %d of %d, %d entries", h.Page, h.Pages, len(h.Entries))
	}
}

// The page asks again for the page the player chose: a new upload is on top of the first page, and
// a later page stays the one asked for.
func TestANewUploadArrivesOnTheFirstPage(t *testing.T) {
	c, _ := fixture(t)
	sent(t, c, 1, 20)

	sent(t, c, 21, 1)

	if got := shown(c.History(1).Entries); got[0] != "upload 21" {
		t.Errorf("page 1: %v", got)
	}
	if h := c.History(2); h.Page != 2 || h.Pages != 3 {
		t.Errorf("page %d of %d", h.Page, h.Pages)
	}
}
