package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gold-forever/companion/internal/server"
)

const (
	newerTag  = "https://github.com/gold-forever/companion/releases/tag/v0.2.0"
	downloads = "https://github.com/gold-forever/companion/releases/download/v0.2.0/"
	linuxFile = "gold-forever-companion-linux-amd64"
)

// github serves a release's files from files, by their address, and counts what it was asked.
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

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// releasing makes c find release v0.2.0 at its GitHub page, with files at its downloads, and records
// what c runs instead of running it.
func releasing(t *testing.T, c *App, files map[string]string) (*github, *[]string) {
	t.Helper()
	(&fakeServer{latest: func() (server.Release, error) {
		return server.Release{Version: "v0.2.0", URL: newerTag}, nil
	}}).connects(c)
	g := &github{files: map[string]string{}}
	for name, body := range files {
		g.files[downloads+name] = body
	}
	c.releases = &http.Client{Transport: g}
	c.releaseFile = linuxFile
	var ran []string
	c.runInstaller = func(path string) error { ran = append(ran, path); return nil }
	return g, &ran
}

func TestUpToDateShowsTheLatestReleaseAndNothingToInstall(t *testing.T) {
	c, _ := fixture(t)
	(&fakeServer{latest: func() (server.Release, error) {
		return server.Release{Version: "v0.1.0", URL: "https://github.com/gold-forever/companion/releases/tag/v0.1.0"}, nil
	}}).connects(c)

	if err := c.CheckForUpdate(t.Context()); err != nil {
		t.Fatal(err)
	}

	u := c.State().Update
	if u.Latest != "v0.1.0" || u.Newer || u.Error != "" {
		t.Errorf("up to date: %+v", u)
	}
}

func TestANewerReleaseIsDownloadedCheckedAndRun(t *testing.T) {
	c, _ := fixture(t)
	companion := "the new companion"
	_, ran := releasing(t, c, map[string]string{
		linuxFile:    companion,
		"SHA256SUMS": sum("other") + "  GoldForever-Companion.exe\n" + sum(companion) + "  " + linuxFile + "\n",
	})
	if err := c.CheckForUpdate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if u := c.State().Update; !u.Newer || !u.CanInstall || u.Latest != "v0.2.0" || u.URL != newerTag {
		t.Fatalf("a newer release: %+v", u)
	}

	if err := c.Update(t.Context()); err != nil {
		t.Fatal(err)
	}

	if len(*ran) != 1 {
		t.Fatalf("ran %v", *ran)
	}
	if got, _ := os.ReadFile((*ran)[0]); string(got) != companion {
		t.Errorf("ran %q", got)
	}
	if info, _ := os.Stat((*ran)[0]); info.Mode().Perm()&0o100 == 0 {
		t.Error("the download is not executable")
	}
}

func TestADownloadThatDoesNotMatchItsChecksumIsNeverRun(t *testing.T) {
	c, _ := fixture(t)
	_, ran := releasing(t, c, map[string]string{
		linuxFile:    "something else",
		"SHA256SUMS": sum("the new companion") + "  " + linuxFile + "\n",
	})
	c.CheckForUpdate(t.Context())

	err := c.Update(t.Context())

	if !errors.Is(err, ErrChecksum) {
		t.Errorf("err = %v", err)
	}
	if len(*ran) != 0 {
		t.Errorf("ran %v", *ran)
	}
	if _, err := os.Stat(c.downloadPath()); !os.IsNotExist(err) {
		t.Error("the download that did not match is kept")
	}
	if u := c.State().Update; !strings.Contains(u.Error, "checksum") || u.URL != newerTag {
		t.Errorf("the page must say so and link the release: %+v", u)
	}
}

func TestAReleaseThatListsNoChecksumForThisSystemIsNotDownloaded(t *testing.T) {
	c, _ := fixture(t)
	g, ran := releasing(t, c, map[string]string{linuxFile: "the new companion", "SHA256SUMS": sum("x") + "  GoldForever-Companion.exe\n"})
	c.CheckForUpdate(t.Context())

	if err := c.Update(t.Context()); !errors.Is(err, ErrChecksum) {
		t.Errorf("err = %v", err)
	}
	if len(*ran) != 0 || len(g.asked) != 1 {
		t.Errorf("asked %v, ran %v", g.asked, *ran)
	}
}

func TestAFailedDownloadSaysSoAndLinksTheRelease(t *testing.T) {
	c, _ := fixture(t)
	_, ran := releasing(t, c, map[string]string{"SHA256SUMS": sum("x") + "  " + linuxFile + "\n"})
	c.CheckForUpdate(t.Context())

	if err := c.Update(t.Context()); err == nil {
		t.Fatal("a download answered 404")
	}
	if u := c.State().Update; u.Error == "" || u.URL != newerTag || len(*ran) != 0 {
		t.Errorf("%+v, ran %v", u, *ran)
	}
}

func TestAFailedCheckSaysSo(t *testing.T) {
	c, _ := fixture(t)
	(&fakeServer{latest: func() (server.Release, error) { return server.Release{}, errors.New("no network") }}).connects(c)

	if err := c.CheckForUpdate(t.Context()); err == nil {
		t.Fatal("the check failed")
	}

	if u := c.State().Update; !strings.Contains(u.Error, "no network") {
		t.Errorf("%+v", u)
	}
}

// Only a release on the companion's GitHub page has files to download; a page of the site, or a Mac,
// where the .app is wherever the player put it, has the player download it themselves.
func TestUpdateInstallsOnlyAGitHubReleaseWhereTheCompanionInstallsItself(t *testing.T) {
	for name, edit := range map[string]func(c *App){
		"a page of the site": func(c *App) {
			(&fakeServer{latest: func() (server.Release, error) {
				return server.Release{Version: "v0.2.0", URL: "https://gold-forever.example/companion"}, nil
			}}).connects(c)
		},
		"macOS": func(c *App) { c.releaseFile = "" },
	} {
		c, _ := fixture(t)
		g, ran := releasing(t, c, map[string]string{linuxFile: "x", "SHA256SUMS": sum("x") + "  " + linuxFile + "\n"})
		edit(c)
		c.CheckForUpdate(t.Context())

		if u := c.State().Update; !u.Newer || u.CanInstall {
			t.Errorf("%s: %+v", name, u)
		}
		if err := c.Update(t.Context()); err == nil || len(g.asked) != 0 || len(*ran) != 0 {
			t.Errorf("%s: err %v, asked %v, ran %v", name, err, g.asked, *ran)
		}
	}
}

func TestUpdateWithNothingNewerInstallsNothing(t *testing.T) {
	c, _ := fixture(t)
	g, ran := releasing(t, c, nil)

	if err := c.Update(t.Context()); err == nil || len(g.asked) != 0 || len(*ran) != 0 {
		t.Errorf("before any check: err %v, asked %v, ran %v", err, g.asked, *ran)
	}
}

func TestEachSystemUpdatesFromItsOwnReleaseFile(t *testing.T) {
	for system, file := range map[[2]string]string{
		{"windows", "amd64"}: "GoldForever-Companion.exe",
		{"windows", "arm64"}: "GoldForever-Companion-arm64.exe",
		{"linux", "amd64"}:   "gold-forever-companion-linux-amd64",
		{"linux", "arm64"}:   "gold-forever-companion-linux-arm64",
		{"darwin", "arm64"}:  "",
		{"linux", "386"}:     "",
	} {
		if got := releaseFile(system[0], system[1]); got != file {
			t.Errorf("%v: %q", system, got)
		}
	}
}
