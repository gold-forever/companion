// Package instance keeps the companion to one copy per user. The running copy writes where its
// settings page listens, with the page's secret, to a file only that user can read; a second launch
// reads it and opens that page, or asks the running copy to quit so a newer download can replace it.
package instance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SecretHeader carries the page's secret on requests from another copy of the companion.
const SecretHeader = "X-Companion-Secret"

// Info is the running copy, as its file records it.
type Info struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Secret  string `json:"secret"`
	Version string `json:"version"`
}

// Write records the running copy, readable by its owner only.
func Write(path string, i Info) error {
	data, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Read returns what the file records; whether that copy still runs is Alive's question.
func Read(path string) (Info, error) {
	var i Info
	data, err := os.ReadFile(path)
	if err != nil {
		return i, err
	}
	return i, json.Unmarshal(data, &i)
}

var client = &http.Client{Timeout: 2 * time.Second}

// Alive says whether the copy i describes is running: its page answers a ping with its secret.
func Alive(i Info) bool {
	res, err := call(i, http.MethodGet, "/api/ping")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// Quit asks the running copy to stop, so a newer download can take its place.
func Quit(i Info) error {
	res, err := call(i, http.MethodPost, "/api/quit")
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the running companion answered %d", res.StatusCode)
	}
	return nil
}

// PageURL opens the running copy's settings page in a browser.
func PageURL(i Info) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?s=%s", i.Port, i.Secret)
}

func call(i Info, method, path string) (*http.Response, error) {
	// The page takes JSON bodies only, so a POST carries an empty object rather than nothing.
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", i.Port, path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(SecretHeader, i.Secret)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}
