// Package instance keeps the companion to one copy per user. The running copy holds a lock the system
// keeps for it (Claim), and writes where its settings page listens, with the page's secret, to a file
// only that user can read; a second launch reads it and opens that page, or asks the running copy to
// quit so a newer download can replace it.
package instance

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrRunning means another copy of the companion holds the claim.
var ErrRunning = errors.New("another copy of the companion is running")

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

// Proof is the running copy's answer to a ping's nonce. Only a holder of secret can make it, so a
// ping needs no secret, and whatever listens on a stale file's port never learns it.
func Proof(secret, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

// PingPath is where the running copy answers a nonce with its Proof, without the secret.
const PingPath = "/api/ping"

// Alive says whether the copy i describes is running: its page answers a fresh nonce with the proof
// only its secret makes.
func Alive(i Info) bool {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return false
	}
	asked := hex.EncodeToString(nonce)
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s?nonce=%s", i.Port, PingPath, asked))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var answer struct {
		Proof string `json:"proof"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&answer) != nil {
		return false
	}
	return hmac.Equal([]byte(answer.Proof), []byte(Proof(i.Secret, asked)))
}

// Quit asks the running copy to stop, so a newer download can take its place.
func Quit(i Info) error {
	res, err := post(i, "/api/quit")
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

// post asks the running copy, with its secret, to act. The page takes JSON bodies only, so it carries
// an empty object rather than nothing.
func post(i Info, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", i.Port, path), strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set(SecretHeader, i.Secret)
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}
