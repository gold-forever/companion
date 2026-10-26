package backup

import (
	"fmt"
	"os"
	"testing"
	"time"
)

var start = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := start
	s := &Store{Dir: t.TempDir(), Keep: 3, Now: func() time.Time { return now }}
	return s, &now
}

func TestACopyWaitsAsPendingUntilMarkedSent(t *testing.T) {
	s, _ := newStore(t)

	kept, added, err := s.Add("/wow/_retail_/WTF/Account/A/SavedVariables/GoldForever.lua", []byte("one"))
	if err != nil || !added {
		t.Fatalf("Add = %v, %v", added, err)
	}
	pending, _ := s.Pending()
	if len(pending) != 1 || pending[0].Path != kept.Path || pending[0].Source != kept.Source {
		t.Fatalf("pending = %+v", pending)
	}
	if data, _ := os.ReadFile(kept.Path); string(data) != "one" {
		t.Fatalf("copy holds %q", data)
	}
	if info, _ := os.Stat(kept.Path); info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("copy is readable by others: %v", info.Mode())
	}

	if err := s.MarkSent(kept); err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.Pending(); len(pending) != 0 {
		t.Fatalf("still pending after being sent: %+v", pending)
	}
}

func TestTheSameBytesAsTheNewestCopyAreNotCopiedAgain(t *testing.T) {
	s, now := newStore(t)
	first, _, _ := s.Add("/a", []byte("one"))
	_ = s.MarkSent(first)
	*now = now.Add(time.Minute)

	if _, added, _ := s.Add("/a", []byte("one")); added {
		t.Fatal("an unchanged save was copied (and would be sent) again")
	}
	if _, added, _ := s.Add("/a", []byte("two")); !added {
		t.Fatal("a changed save was not copied")
	}
}

func TestOnlyTheNewestCopiesAreKept(t *testing.T) {
	s, now := newStore(t)
	for i := range 5 {
		*now = start.Add(time.Duration(i) * time.Minute)
		c, _, _ := s.Add("/a", []byte(fmt.Sprint(i)))
		_ = s.MarkSent(c)
	}

	copies, _ := s.Copies("/a")
	if len(copies) != 3 {
		t.Fatalf("kept %d copies, want 3", len(copies))
	}
	if data, _ := os.ReadFile(copies[0].Path); string(data) != "2" {
		t.Fatalf("oldest kept copy holds %q, want the third", data)
	}
}

func TestPendingCopiesComeOldestFirstAcrossSaves(t *testing.T) {
	s, now := newStore(t)
	_, _, _ = s.Add("/b", []byte("b1"))
	*now = now.Add(time.Second)
	_, _, _ = s.Add("/a", []byte("a1"))
	*now = now.Add(time.Second)
	_, _, _ = s.Add("/b", []byte("b2"))

	pending, _ := s.Pending()
	var order []string
	for _, c := range pending {
		data, _ := os.ReadFile(c.Path)
		order = append(order, string(data))
	}
	if fmt.Sprint(order) != "[b1 a1 b2]" {
		t.Fatalf("order = %v", order)
	}
}
