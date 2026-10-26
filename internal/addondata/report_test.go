package addondata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func withReport(r *Refresher, now *time.Time) *Report {
	r.Report = &Report{Now: func() time.Time { return *now }}
	return r.Report
}

func TestTheReportShowsAFreshFetchWithTheFilesMarketsItemsAndSize(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_classic_": 11507})
	now := t0
	r := refresher(&fetcher{body: cannedMarkets(t), etag: `"a1"`}, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)

	_ = r.Refresh(context.Background())

	lua, _ := os.ReadFile(filepath.Join(dataDir(install, "_forever_"), "Data.lua"))
	want := []File{{
		Flavour: filepath.Join(install, "_forever_"), Game: "forever",
		Markets: []Market{{"forever-BETA-normal-alliance", 2}, {"forever-BETA-normal-horde", 2}},
		Items:   4, Size: int64(len(lua)), Fetched: t0, Outcome: OutcomeOK,
	}}
	if got := report.Files(saves); !reflect.DeepEqual(got, want) {
		t.Fatalf("Files =\n%+v\nwant\n%+v", got, want)
	}
}

func TestTheReportShowsAnUnchangedAnswerAndTheFileStillHeld(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	now := t0
	f := &fetcher{body: cannedMarkets(t), etag: `"a1"`}
	r := refresher(f, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)
	_ = r.Refresh(context.Background())
	now = t0.Add(time.Hour)

	_ = r.Refresh(context.Background())

	got := report.Files(saves)[0]
	if got.Outcome != OutcomeNotModified || !got.Fetched.Equal(now) || got.Items != 4 || got.Error != "" {
		t.Fatalf("Files = %+v; want not modified at %s with the file's 4 items", got, now)
	}
}

func TestTheReportShowsAFailedFetchWithItsMessageAndTheLastGoodFile(t *testing.T) {
	_, saves := tree(t, map[string]int{"_forever_": 16001})
	now := t0
	state := filepath.Join(t.TempDir(), "prices.json")
	r := refresher(&fetcher{body: cannedMarkets(t), etag: `"a1"`}, saves, "", state)
	report := withReport(r, &now)
	_ = r.Refresh(context.Background())
	now = t0.Add(time.Hour)

	for _, f := range []*fetcher{{err: errors.New("server unavailable: offline")}, {body: []byte(malformedAccount["a float price"])}} {
		r.Fetcher = f
		_ = r.Refresh(context.Background())

		got := report.Files(saves)[0]
		if got.Outcome != OutcomeError || got.Error == "" || !got.Fetched.Equal(now) || len(got.Markets) != 2 {
			t.Fatalf("Files = %+v; want the error at %s and the last good file's markets", got, now)
		}
	}
	if got := report.Files(saves)[0].Error; !strings.Contains(got, "refused") {
		t.Errorf("Error = %q, want it to say the answer was refused", got)
	}
}

func TestTheReportShowsAFlavourNotWrittenAsFailedAndTheOtherAsWritten(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001, "_forever_ptr_": 16002})
	blocked := dataDir(install, "_forever_ptr_")
	if err := os.MkdirAll(filepath.Join(blocked, "Data.lua", "blocker"), 0o755); err != nil { // nothing renames over it
		t.Fatal(err)
	}
	now := t0
	r := refresher(&fetcher{body: cannedMarkets(t)}, saves, "", filepath.Join(t.TempDir(), "prices.json"))
	report := withReport(r, &now)

	_ = r.Refresh(context.Background())

	files := report.Files(saves)
	if files[0].Outcome != OutcomeOK || files[1].Outcome != OutcomeError || files[1].Error == "" {
		t.Fatalf("Files = %+v; want _forever_ written, _forever_ptr_ failed with a message", files)
	}
}

func TestTheReportListsAFlavourBeforeItsFirstFetch(t *testing.T) {
	_, saves := tree(t, map[string]int{"_retail_": 110002})

	files := (&Report{}).Files(saves)

	if len(files) != 1 || files[0].Game != "retail" || files[0].Outcome != "" || !files[0].Fetched.IsZero() || files[0].Size != 0 {
		t.Fatalf("Files = %+v; want the retail flavour with no file and no fetch yet", files)
	}
}
