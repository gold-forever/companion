// Package sendlog is the player's record of every upload: one JSON line per answer the server gave,
// kept on their own computer, so they can see what was sent and when.
package sendlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/platform/atomicfile"
)

// Entry is one upload and the server's answer.
type Entry struct {
	Time   time.Time `json:"time"`
	Save   string    `json:"save"` // the save's label, as the player sees it
	Size   int       `json:"size"`
	SHA256 string    `json:"sha256"`
	Answer string    `json:"answer"`
}

// Log is a JSON-lines file; Prune drops entries older than Keep.
type Log struct {
	Path string
	Keep time.Duration
}

// Append adds one entry.
func (l *Log) Append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read returns every entry, oldest first; lines it cannot read are skipped.
func (l *Log) Read() ([]Entry, error) {
	data, err := os.ReadFile(l.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, scanner.Err()
}

// Prune rewrites the log without the entries older than Keep at now.
func (l *Log) Prune(now time.Time) error {
	entries, err := l.Read()
	if err != nil || len(entries) == 0 {
		return err
	}
	var kept bytes.Buffer
	for _, e := range entries {
		if now.Sub(e.Time) > l.Keep {
			continue
		}
		line, _ := json.Marshal(e)
		kept.Write(append(line, '\n'))
	}
	return atomicfile.Write(l.Path, kept.Bytes())
}
