package addonupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// github answers the addon repository's release list and its files, by address, and records what it
// was asked.
type github struct {
	files map[string]string
	asked []string
}

func (g *github) RoundTrip(r *http.Request) (*http.Response, error) {
	g.asked = append(g.asked, r.URL.String())
	body, ok := g.files[r.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

const releases = "https://api.github.com/repos/gold-forever/addon/releases?per_page=20"

func asset(version, digest string) string {
	return `{"name":"GoldForever-` + version + `.zip","browser_download_url":"https://github.com/gold-forever/addon/releases/download/addon-v` +
		version + `/GoldForever-` + version + `.zip","digest":"` + digest + `"}`
}

func release(version string, pre, draft bool, assets ...string) string {
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	return `{"tag_name":"addon-v` + version + `","prerelease":` + b(pre) + `,"draft":` + b(draft) + `,"assets":[` + strings.Join(assets, ",") + `]}`
}

func source(files map[string]string) (Source, *github) {
	g := &github{files: files}
	return Source{Client: &http.Client{Transport: g}}, g
}

func TestLatestIsTheNewestReleaseAndSkipsPreReleasesAndDrafts(t *testing.T) {
	s, _ := source(map[string]string{releases: "[" + strings.Join([]string{
		release("0.18.0", false, true, asset("0.18.0", digest("d"))),
		release("0.17.0-beta.1", true, false, asset("0.17.0-beta.1", digest("b"))),
		release("0.16.1", false, false, asset("0.16.1", digest("zip"))),
		release("0.16.0", false, false, asset("0.16.0", digest("old"))),
	}, ",") + "]"})

	r, err := s.Latest(t.Context(), ChannelRelease)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.16.1" || r.SHA256 != digest("zip")[len("sha256:"):] ||
		r.URL != "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip" {
		t.Errorf("latest: %+v", r)
	}

	r, err = s.Latest(t.Context(), ChannelBeta)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.17.0-beta.1" {
		t.Errorf("on the beta channel, the latest is %q", r.Version)
	}
}

func TestLatestRefusesAReleaseWhoseZipIsNotOnTheAddonsReleasePage(t *testing.T) {
	elsewhere := `{"name":"GoldForever-0.16.1.zip","browser_download_url":"https://example.com/GoldForever-0.16.1.zip","digest":"` + digest("zip") + `"}`
	s, _ := source(map[string]string{releases: "[" + release("0.16.1", false, false, elsewhere) + "]"})

	if _, err := s.Latest(t.Context(), ChannelRelease); err == nil {
		t.Error("a zip from another site was offered")
	}
}

func TestDownloadRefusesAZipTheChecksumDoesNotVouchFor(t *testing.T) {
	url := "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip"
	s, _ := source(map[string]string{url: "tampered"})

	_, err := s.Download(t.Context(), Release{Version: "0.16.1", URL: url, SHA256: digest("zip")[len("sha256:"):]})

	if !errors.Is(err, ErrChecksum) {
		t.Errorf("a zip that does not match its checksum: %v", err)
	}
}

func TestDownloadRefusesAReleaseThatPublishesNoChecksum(t *testing.T) {
	url := "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip"
	s, g := source(map[string]string{url: "zip"})

	_, err := s.Download(t.Context(), Release{Version: "0.16.1", URL: url})

	if !errors.Is(err, ErrChecksum) || len(g.asked) != 0 {
		t.Errorf("a release with no checksum: %v, asked %v", err, g.asked)
	}
}

func TestDownloadKeepsAZipThatMatchesItsChecksum(t *testing.T) {
	url := "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip"
	s, _ := source(map[string]string{url: "zip"})

	data, err := s.Download(t.Context(), Release{Version: "0.16.1", URL: url, SHA256: digest("zip")[len("sha256:"):]})

	if err != nil || string(data) != "zip" {
		t.Errorf("a matching zip: %q, %v", data, err)
	}
}

func TestTheBetaChannelPicksTheNewestBySemanticVersionNotByListOrder(t *testing.T) {
	s, _ := source(map[string]string{releases: "[" + strings.Join([]string{
		release("0.17.0-beta.2", true, false, asset("0.17.0-beta.2", digest("b2"))),
		release("0.17.0-rc.1", true, false, asset("0.17.0-rc.1", digest("rc"))),
		release("0.17.0-beta.10", true, false, asset("0.17.0-beta.10", digest("b10"))),
		release("0.16.1", false, false, asset("0.16.1", digest("zip"))),
	}, ",") + "]"})

	r, err := s.Latest(t.Context(), ChannelBeta)

	if err != nil || r.Version != "0.17.0-rc.1" {
		t.Errorf("on the beta channel: %+v, %v", r, err)
	}
}

const snapshotRelease = "https://api.github.com/repos/gold-forever/addon/releases/tags/addon-snapshot"

func snapshot(version, url, digest string) string {
	return `{"tag_name":"addon-snapshot","prerelease":true,"draft":false,"assets":[` +
		`{"name":"SHA256SUMS","browser_download_url":"https://github.com/gold-forever/addon/releases/download/addon-snapshot/SHA256SUMS","digest":"` + digest + `"},` +
		`{"name":"GoldForever-` + version + `.zip","browser_download_url":"` + url + `","digest":"` + digest + `"}]}`
}

func TestTheSnapshotChannelIsTheAddonSnapshotRelease(t *testing.T) {
	const v = "0.16.1-snapshot.202611031530.2ceabf9"
	url := "https://github.com/gold-forever/addon/releases/download/addon-snapshot/GoldForever-" + v + ".zip"
	s, g := source(map[string]string{snapshotRelease: snapshot(v, url, digest("snap"))})

	r, err := s.Latest(t.Context(), ChannelSnapshot)

	if err != nil || r.Version != v || r.URL != url || r.SHA256 != digest("snap")[len("sha256:"):] {
		t.Errorf("the snapshot: %+v, %v", r, err)
	}
	if len(g.asked) != 1 || g.asked[0] != snapshotRelease {
		t.Errorf("asked %v", g.asked)
	}
}

func TestTheSnapshotChannelRefusesAZipThatIsNotOnTheSnapshotsPage(t *testing.T) {
	const v = "0.16.1-snapshot.202611031530.2ceabf9"
	for name, url := range map[string]string{
		"another site":    "https://example.com/GoldForever-" + v + ".zip",
		"another release": "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-" + v + ".zip",
	} {
		s, _ := source(map[string]string{snapshotRelease: snapshot(v, url, digest("snap"))})
		if _, err := s.Latest(t.Context(), ChannelSnapshot); err == nil {
			t.Errorf("a zip from %s was offered", name)
		}
	}
}

func TestTheSnapshotChannelRefusesAZipWhoseVersionIsNotASnapshot(t *testing.T) {
	url := "https://github.com/gold-forever/addon/releases/download/addon-snapshot/GoldForever-0.16.1.zip"
	s, _ := source(map[string]string{snapshotRelease: snapshot("0.16.1", url, digest("snap"))})

	if _, err := s.Latest(t.Context(), ChannelSnapshot); err == nil {
		t.Error("a snapshot named as a release was offered")
	}
}

func TestTheSnapshotChannelWithNoSnapshotPublishedSaysSo(t *testing.T) {
	s, _ := source(map[string]string{})

	if _, err := s.Latest(t.Context(), ChannelSnapshot); err == nil {
		t.Error("no snapshot, no error")
	}
}

func TestWantedSaysWhetherTheChannelsBuildReplacesTheInstalledOne(t *testing.T) {
	const snap, oldSnap = "0.16.1-snapshot.202611031530.2ceabf9", "0.16.1-snapshot.202611021200.1a2b3c4"
	cases := []struct {
		channel           Channel
		latest, installed string
		want              bool
	}{
		{ChannelRelease, "0.16.1", "0.16.0", true},
		{ChannelRelease, "0.16.1", "0.16.1", false},
		{ChannelRelease, "0.16.0", "0.16.1", false},
		{ChannelRelease, "0.17.0", "0.16.10", true},
		{ChannelBeta, "0.17.0-beta.1", "0.16.1", true},
		{ChannelBeta, "0.17.0", "0.17.0-beta.1", true},
		{ChannelBeta, "0.17.0-beta.1", "0.17.0-beta.2", false},
		// Back on the release channel, a beta or a snapshot gives way to the newest release, older or not.
		{ChannelRelease, "0.16.1", "0.17.0-beta.1", true},
		{ChannelRelease, "0.16.1", snap, true},
		{ChannelBeta, "0.17.0-beta.1", snap, true},
		// A snapshot installs whenever it differs from the installed build, never by newer.
		{ChannelSnapshot, snap, "0.16.1", true},
		{ChannelSnapshot, snap, "0.17.0", true},
		{ChannelSnapshot, snap, oldSnap, true},
		{ChannelSnapshot, oldSnap, snap, true},
		{ChannelSnapshot, snap, snap, false},
		// A version that is not a build of the addon is a developer's copy, and none installed is none:
		// never replaced, on any channel.
		{ChannelRelease, "0.17.0", "@project-version@", false},
		{ChannelSnapshot, snap, "@project-version@", false},
		{ChannelRelease, "0.17.0", "", false},
		{ChannelSnapshot, snap, "", false},
	}
	for _, c := range cases {
		if got := Wanted(c.channel, c.latest, c.installed); got != c.want {
			t.Errorf("Wanted(%s, %q, %q) = %v", c.channel, c.latest, c.installed, got)
		}
	}
}
