package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	release "github.com/gold-forever/companion/internal/platform/version"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

// releaseAsker says which release of the companion is the latest.
type releaseAsker interface {
	Latest(ctx context.Context) (server.Release, error)
}

const (
	checkVersionEvery = 24 * time.Hour
	// A release's page and its files on GitHub, followed by its tag; the release workflow publishes
	// SHA256SUMS beside the files.
	releaseTag       = "https://github.com/gold-forever/companion/releases/tag/"
	releaseDownloads = "https://github.com/gold-forever/companion/releases/download/"
	checksums        = "SHA256SUMS"
	maxChecksums     = 64 << 10
	maxReleaseFile   = 256 << 20
	// updateDir, in the data folder, holds the downloaded companion while it installs itself.
	updateDir = "update"
)

var (
	ErrNothingNewer = refusal("no newer release to install: check for updates first")
	ErrNotHere      = refusal("this release cannot be installed from here: download it from its page")
	ErrUpdating     = refusal("an update is already under way")
	// ErrChecksum is a download the release's published checksum does not vouch for; it never runs.
	ErrChecksum = errors.New("the download does not match the checksum the release publishes, so nothing was run")
)

// releaseFile is the file a release publishes for goos on goarch, as the Makefile names it: "" where
// the companion does not install itself (the Mac's .app stays where the player put it) or none is built.
func releaseFile(goos, goarch string) string {
	switch {
	case goos == "windows" && goarch == "amd64":
		return "GoldForever-Companion.exe"
	case goos == "windows" && goarch == "arm64":
		return "GoldForever-Companion-arm64.exe"
	case goos == "linux" && (goarch == "amd64" || goarch == "arm64"):
		return "gold-forever-companion-linux-" + goarch
	}
	return ""
}

// checkVersion checks for an update once a day.
func (c *App) checkVersion(ctx context.Context) {
	asker := c.connect("")
	for {
		_ = c.checkForUpdate(ctx, asker)
		c.remindOfUpdate(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkVersionEvery):
		}
	}
}

// CheckForUpdate asks the server whether a newer release exists, for the page's notice and update section.
func (c *App) CheckForUpdate(ctx context.Context) error {
	return c.checkForUpdate(ctx, c.connect(""))
}

func (c *App) checkForUpdate(ctx context.Context, asker releaseAsker) error {
	latest, err := asker.Latest(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.update.Error = "Checking for updates failed: " + err.Error()
		return err
	}
	newer := release.Newer(latest.Version, c.version)
	c.update = page.Update{
		Latest: latest.Version, URL: latest.URL, Newer: newer, Installing: c.update.Installing,
		CanInstall: newer && c.releaseFile != "" && latest.URL == releaseTag+latest.Version,
	}
	if newer {
		c.notice = &page.Notice{Version: latest.Version, URL: latest.URL}
	}
	return nil
}

// Update downloads the newer release the last check found, checks it against the release's published
// checksum and runs it: like a double-click on a newer download, it stops this copy, installs itself
// over it and starts again.
func (c *App) Update(ctx context.Context) error {
	c.mu.Lock()
	u := c.update
	switch {
	case !u.Newer:
		c.mu.Unlock()
		return ErrNothingNewer
	case !u.CanInstall:
		c.mu.Unlock()
		return ErrNotHere
	case u.Installing:
		c.mu.Unlock()
		return ErrUpdating
	}
	c.update.Installing, c.update.Error = true, ""
	c.mu.Unlock()

	err := c.install(ctx, releaseDownloads+u.Latest+"/")
	if err != nil {
		c.mu.Lock()
		c.update.Installing, c.update.Error = false, "Updating failed: "+err.Error()+"."
		c.mu.Unlock()
	}
	return err
}

func (c *App) install(ctx context.Context, downloads string) error {
	sums, err := c.download(ctx, downloads+checksums, maxChecksums)
	if err != nil {
		return err
	}
	want, ok := checksumOf(sums, c.releaseFile)
	if !ok {
		return ErrChecksum
	}
	data, err := c.download(ctx, downloads+c.releaseFile, maxReleaseFile)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return ErrChecksum
	}
	path := c.downloadPath()
	if err := writeExecutable(path, data); err != nil {
		return err
	}
	return c.runInstaller(path)
}

func (c *App) downloadPath() string { return filepath.Join(c.dataDir, updateDir, c.releaseFile) }

func (c *App) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.releases.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s answered %d", url, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%s is larger than any release", url)
	}
	return data, err
}

// checksumOf finds file's SHA-256 in a sha256sum listing.
func checksumOf(sums []byte, file string) (string, bool) {
	lines := bufio.NewScanner(bytes.NewReader(sums))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == file {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// writeExecutable writes the download beside its place first, so a half-written one never runs.
func writeExecutable(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
