// Package watch notices when the game has finished writing a save. It polls each file's size and
// modification time rather than using the operating system's file events, which behave differently
// on each system and are unreliable under Wine and on network drives.
package watch

import (
	"bytes"
	"io/fs"
	"os"
	"time"
)

// Settled is a save that stopped changing and reads as a whole file.
type Settled struct {
	Path string
	Data []byte
}

type state struct {
	size      int64
	mod       time.Time
	changedAt time.Time
	read      bool // read since the last change; read again only after the next one
}

// Watcher remembers what it last saw of each file.
type Watcher struct {
	settle time.Duration
	files  map[string]*state
	stat   func(string) (fs.FileInfo, error)
	read   func(string) ([]byte, error)
}

// New watches files that count as written once they have been unchanged for settle.
func New(settle time.Duration) *Watcher {
	return &Watcher{settle: settle, files: map[string]*state{}, stat: os.Stat, read: os.ReadFile}
}

// Poll looks at every path once and returns the saves that settled since the last poll. A file that
// settles incomplete is not read again until it changes, so a broken file is never retried in a loop;
// one that could not be read at all is.
func (w *Watcher) Poll(now time.Time, paths []string) []Settled {
	var out []Settled
	for _, path := range paths {
		info, err := w.stat(path)
		if err != nil {
			delete(w.files, path)
			continue
		}
		s := w.files[path]
		if s == nil || s.size != info.Size() || !s.mod.Equal(info.ModTime()) {
			w.files[path] = &state{size: info.Size(), mod: info.ModTime(), changedAt: now}
			continue
		}
		if s.read || now.Sub(s.changedAt) < w.settle {
			continue
		}
		data, err := w.read(path)
		if err != nil {
			continue // held by another program for now: the next poll tries again
		}
		s.read = true
		if Complete(data) {
			out = append(out, Settled{path, data})
		}
	}
	return out
}

var (
	byteOrderMark = []byte("\xef\xbb\xbf")
	opening       = []byte("GoldForeverDB = {")
)

// Complete says whether a save reads as a whole file: it opens GoldForeverDB's table and its last
// line closes it. Every nested table closes with "},", so a file cut off mid-write never ends in a
// bare "}".
func Complete(data []byte) bool {
	// The game starts the file with a blank line (CRLF under Wine and on Windows).
	data = bytes.TrimLeft(bytes.TrimPrefix(data, byteOrderMark), " \t\r\n")
	if !bytes.HasPrefix(data, opening) {
		return false
	}
	trimmed := bytes.TrimRight(data, " \t\r\n")
	last := trimmed[bytes.LastIndexByte(trimmed, '\n')+1:]
	return string(bytes.TrimSpace(last)) == "}"
}
