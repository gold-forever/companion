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

	r, err := s.Latest(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.16.1" || r.SHA256 != digest("zip")[len("sha256:"):] ||
		r.URL != "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip" {
		t.Errorf("latest: %+v", r)
	}

	r, err = s.Latest(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.17.0-beta.1" {
		t.Errorf("with pre-releases, the latest is %q", r.Version)
	}
}

func TestLatestRefusesAReleaseWhoseZipIsNotOnTheAddonsReleasePage(t *testing.T) {
	elsewhere := `{"name":"GoldForever-0.16.1.zip","browser_download_url":"https://example.com/GoldForever-0.16.1.zip","digest":"` + digest("zip") + `"}`
	s, _ := source(map[string]string{releases: "[" + release("0.16.1", false, false, elsewhere) + "]"})

	if _, err := s.Latest(t.Context(), false); err == nil {
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

func TestNewerComparesTheReleaseWithTheInstalledVersion(t *testing.T) {
	cases := []struct {
		latest, installed string
		want              bool
	}{
		{"0.16.1", "0.16.0", true},
		{"0.16.1", "0.16.1", false},
		{"0.16.0", "0.16.1", false},
		{"0.17.0", "0.16.10", true},
		{"0.17.0-beta.1", "0.16.1", true},
		{"0.17.0", "0.17.0-beta.1", true},
		// A version that is not a release is a developer's copy: never replaced.
		{"0.17.0", "@project-version@", false},
		{"0.17.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.installed); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.installed, got)
		}
	}
}
