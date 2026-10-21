package sendlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var start = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func TestEntriesAreAppendedAndReadBack(t *testing.T) {
	l := &Log{Path: filepath.Join(t.TempDir(), "sent.jsonl"), Keep: 30 * 24 * time.Hour}

	if err := l.Append(Entry{Time: start, Save: "WoW › _retail_ › A", Size: 3, SHA256: "abc", Answer: "imported"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(Entry{Time: start.Add(time.Minute), Save: "WoW › _retail_ › A", Size: 3, SHA256: "abc", Answer: "already imported"}); err != nil {
		t.Fatal(err)
	}

	entries, err := l.Read()
	if err != nil || len(entries) != 2 || entries[1].Answer != "already imported" {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	if info, _ := os.Stat(l.Path); info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("log is readable by others: %v", info.Mode())
	}
}

func TestEntriesOlderThanKeepAreDropped(t *testing.T) {
	l := &Log{Path: filepath.Join(t.TempDir(), "sent.jsonl"), Keep: 30 * 24 * time.Hour}
	_ = l.Append(Entry{Time: start, Answer: "old"})
	_ = l.Append(Entry{Time: start.Add(31 * 24 * time.Hour), Answer: "new"})

	if err := l.Prune(start.Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(l.Path)
	if strings.Contains(string(data), "old") || !strings.Contains(string(data), "new") {
		t.Fatalf("log after pruning: %s", data)
	}
}
