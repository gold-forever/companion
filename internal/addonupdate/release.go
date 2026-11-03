// Package addonupdate keeps the Gold Forever addon in a game's AddOns folder at the newest build of its
// channel on GitHub: it finds the release, downloads its zip, checks it against the SHA-256 GitHub publishes for
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
	downloads = "https://github.com/gold-forever/addon/releases/download/"
	// The rolling pre-release built from the addon's main branch.
	snapshotTag = "addon-snapshot"
	snapshotURL = "https://api.github.com/repos/gold-forever/addon/releases/tags/" + snapshotTag
	tagPrefix   = "addon-v"
	maxAnswer   = 1 << 20
	maxZip      = 32 << 20
	sha256Kind  = "sha256:"
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
// Channel is which builds of the addon the player follows: its releases, its releases and
// pre-releases, or the rolling snapshot of its main branch.
type Channel string

const (
	ChannelRelease  Channel = "release"
	ChannelBeta     Channel = "beta"
	ChannelSnapshot Channel = "snapshot"
)

// Latest is the channel's newest build: the newest release by semantic version on the release and beta
// channels (pre-releases on beta only), the snapshot release on the snapshot channel; none found is an
// error.
func (s Source) Latest(ctx context.Context, channel Channel) (Release, error) {
	if channel == ChannelSnapshot {
		return s.snapshot(ctx)
	}
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
		if r.Draft || r.Prerelease && channel != ChannelBeta || !strings.HasPrefix(r.Tag, tagPrefix) {
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

// snapshot is the zip of the addon's snapshot release, whose tag moves with every build, so the zip's
// name carries the version.
func (s Source) snapshot(ctx context.Context) (Release, error) {
	body, err := s.get(ctx, snapshotURL, maxAnswer)
	if err != nil {
		return Release{}, err
	}
	var r githubRelease
	if err := json.Unmarshal(body, &r); err != nil {
		return Release{}, fmt.Errorf("the addon's snapshot could not be read: %w", err)
	}
	for _, a := range r.Assets {
		v, ok := strings.CutPrefix(a.Name, "GoldForever-")
		v, zip := strings.CutSuffix(v, ".zip")
		if !ok || !zip {
			continue
		}
		if !isSnapshot(v) {
			return Release{}, fmt.Errorf("the addon's snapshot names a zip that is not a snapshot: %q", a.Name)
		}
		if a.URL != downloads+snapshotTag+"/"+a.Name {
			return Release{}, fmt.Errorf("the addon's snapshot names a zip that is not on its release page: %q", a.URL)
		}
		return Release{Version: v, URL: a.URL, SHA256: strings.TrimPrefix(a.Digest, sha256Kind)}, nil
	}
	return Release{}, errors.New("the addon's snapshot has no zip")
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

// Wanted says whether the channel's latest build replaces the installed one. A snapshot replaces any
// other build; on the release channel the newest release replaces a pre-release or a snapshot, newer or
// not, and on the beta channel a snapshot. Otherwise only a newer build replaces the installed one.
// None installed, or a version that is no build of the addon (a developer's own copy), is never replaced.
func Wanted(channel Channel, latest, installed string) bool {
	switch {
	case !isSnapshot(installed) && !version.IsRelease(installed):
		return false
	case channel == ChannelSnapshot:
		return latest != installed
	case channel == ChannelRelease && strings.Contains(installed, "-"):
		return true
	}
	// A snapshot is no release, so any release is newer.
	return version.Newer(latest, installed)
}

// isSnapshot says whether v is a snapshot's version: a release's, then -snapshot. and the build's time
// and commit (0.16.1-snapshot.202611031530.2ceabf9).
func isSnapshot(v string) bool {
	core, build, ok := strings.Cut(v, "-snapshot.")
	return ok && build != "" && !strings.Contains(core, "-") && version.IsRelease(core)
}
