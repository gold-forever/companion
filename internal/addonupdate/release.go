// Package addonupdate keeps the Gold Forever addon in a game's AddOns folder at its newest release on
// GitHub: it finds the release, downloads its zip, checks it against the SHA-256 GitHub publishes for
// it, and swaps the addon's folders in, keeping the old ones until the companion next starts.
package addonupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gold-forever/companion/internal/platform/version"
)

const (
	// The addon repository's releases, newest first: enough to find the newest release past a few
	// pre-releases and drafts.
	releasesURL = "https://api.github.com/repos/gold-forever/addon/releases?per_page=20"
	// Where the addon's release workflow publishes its zip, followed by the tag.
	downloads  = "https://github.com/gold-forever/addon/releases/download/"
	tagPrefix  = "addon-v"
	maxAnswer  = 1 << 20
	maxZip     = 32 << 20
	sha256Kind = "sha256:"
)

// ErrChecksum is a zip the release's published SHA-256 does not vouch for, or a release that
// publishes none: it is never installed.
var ErrChecksum = errors.New("the addon download does not match the checksum its release publishes")

// Source is the addon's releases on GitHub, asked with Client.
type Source struct{ Client *http.Client }

// Release is an addon release: its version (0.16.1), its zip's address and the zip's SHA-256 in hex.
type Release struct{ Version, URL, SHA256 string }

type githubRelease struct {
	Tag        string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// Latest is the newest release, a pre-release only when prereleases says so; none found is an error.
func (s Source) Latest(ctx context.Context, prereleases bool) (Release, error) {
	body, err := s.get(ctx, releasesURL, maxAnswer)
	if err != nil {
		return Release{}, err
	}
	var list []githubRelease
	if err := json.Unmarshal(body, &list); err != nil {
		return Release{}, fmt.Errorf("the addon's releases could not be read: %w", err)
	}
	var newest githubRelease
	for _, r := range list {
		if r.Draft || r.Prerelease && !prereleases || !strings.HasPrefix(r.Tag, tagPrefix) {
			continue
		}
		if version.Newer(versionOf(r.Tag), versionOf(newest.Tag)) {
			newest = r
		}
	}
	if newest.Tag == "" {
		return Release{}, errors.New("the addon has no release yet")
	}
	return zipOf(newest)
}

func versionOf(tag string) string { return strings.TrimPrefix(tag, tagPrefix) }

// zipOf is a release's zip, as the addon's release workflow names it, published on its release page.
func zipOf(r githubRelease) (Release, error) {
	v := versionOf(r.Tag)
	name := "GoldForever-" + v + ".zip"
	for _, a := range r.Assets {
		if a.Name != name {
			continue
		}
		if a.URL != downloads+r.Tag+"/"+name {
			return Release{}, fmt.Errorf("the addon release %s names a zip that is not on its release page: %q", v, a.URL)
		}
		return Release{Version: v, URL: a.URL, SHA256: strings.TrimPrefix(a.Digest, sha256Kind)}, nil
	}
	return Release{}, fmt.Errorf("the addon release %s has no %s", v, name)
}

// Download fetches the release's zip and keeps it only when it matches the release's SHA-256.
func (s Source) Download(ctx context.Context, r Release) ([]byte, error) {
	if len(r.SHA256) != sha256.Size*2 {
		return nil, ErrChecksum
	}
	data, err := s.get(ctx, r.URL, maxZip)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != strings.ToLower(r.SHA256) {
		return nil, ErrChecksum
	}
	return data, nil
}

func (s Source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", url, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%s is larger than any addon release", url)
	}
	return data, err
}

// Newer says whether latest is newer than the installed version. An installed version that is not a
// release is a developer's own copy, which is never replaced.
func Newer(latest, installed string) bool {
	return version.IsRelease(installed) && version.Newer(latest, installed)
}
