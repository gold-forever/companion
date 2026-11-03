package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/addonupdate"
)

const (
	addonReleases = "https://api.github.com/repos/gold-forever/addon/releases?per_page=20"
	addonZip      = "https://github.com/gold-forever/addon/releases/download/addon-v0.16.1/GoldForever-0.16.1.zip"
)

func addonToc(version string) string { return "## Interface: 16001\n## Version: " + version + "\n" }

// addonInstalled puts addon 0.16.0 and the companion's prices into the fixture's game folder, and
// returns its AddOns folder.
func addonInstalled(t *testing.T, save string) string {
	t.Helper()
	addOns := filepath.Join(filepath.Dir(save), "..", "..", "..", "..", "Interface", "AddOns")
	for name, body := range map[string]string{
		"GoldForever/GoldForever.toc": addonToc("0.16.0"),
		"GoldForever_Data/Data.lua":   "prices",
	} {
		path := filepath.Join(addOns, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Clean(addOns)
}

func addonZipOf(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("GoldForever/GoldForever.toc")
	f.Write([]byte(addonToc("0.16.1")))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func addonRelease(zipSum string) string {
	return `[{"tag_name":"addon-v0.16.1","prerelease":false,"draft":false,"assets":[{"name":"GoldForever-0.16.1.zip","browser_download_url":"` +
		addonZip + `","digest":"sha256:` + zipSum + `"}]}]`
}

// addonOn turns keeping the addon up to date on in c's settings, as the player confirmed it.
func addonOn(t *testing.T, c *App) {
	t.Helper()
	if err := c.SetAddonUpdates(t.Context(), true, true); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestKeepingTheAddonUpToDateIsOffByDefaultAndAsksNothing(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	g := &github{files: map[string]string{}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}

	if c.State().Addon.On || c.settings.Get().AddonUpdates {
		t.Error("keeping the addon up to date starts on")
	}
	c.checkAddon(t.Context())
	if len(g.asked) != 0 || addonupdate.Installed(addOns) != "0.16.0" {
		t.Errorf("off, the companion asked %v", g.asked)
	}
}

func TestTurningItOnNeedsTheWarningConfirmed(t *testing.T) {
	c, _ := fixture(t)
	want := "The companion will download and install the Gold Forever addon from its GitHub releases whenever a new version is out, replacing the installed one"

	err := c.SetAddonUpdates(t.Context(), true, false)

	if !errors.Is(err, ErrConfirmAddonUpdates) || !strings.HasPrefix(err.Error(), want) || c.settings.Get().AddonUpdates {
		t.Fatalf("turned on unconfirmed: %v", err)
	}
	if !strings.HasPrefix(c.State().Addon.Warning, want) {
		t.Errorf("the page warns %q", c.State().Addon.Warning)
	}
	addonOn(t, c)
	if !c.State().Addon.On {
		t.Error("confirmed, it is still off")
	}
	if err := c.SetAddonUpdates(t.Context(), false, false); err != nil || c.State().Addon.On {
		t.Errorf("turning it off: %v", err)
	}
}

func TestANewerAddonIsInstalledIntoTheSavesGameAndThePageSaysToReload(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	z := addonZipOf(t)
	g := &github{files: map[string]string{addonReleases: addonRelease(sum(z)), addonZip: z}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)

	c.checkAddon(t.Context())

	if addonupdate.Installed(addOns) != "0.16.1" {
		t.Errorf("installed: %q, asked %v", addonupdate.Installed(addOns), g.asked)
	}
	if readFile(t, filepath.Join(addOns, "GoldForever_Data", "Data.lua")) != "prices" {
		t.Error("the companion's prices were replaced")
	}
	if s := c.State().Addon; s.Status != "Addon updated to v0.16.1 — /reload in game" || s.Error != "" {
		t.Errorf("the page says %+v", s)
	}

	g.asked = nil
	c.checkAddon(t.Context())
	if len(g.asked) != 1 || g.asked[0] != addonReleases {
		t.Errorf("up to date, the companion asked %v", g.asked)
	}
}

func TestAZipTheChecksumRefusesLeavesTheAddonAsItWasAndSaysSo(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	z := addonZipOf(t)
	g := &github{files: map[string]string{addonReleases: addonRelease(sum("another zip")), addonZip: z}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)

	c.checkAddon(t.Context())

	if addonupdate.Installed(addOns) != "0.16.0" {
		t.Error("a zip the checksum refuses was installed")
	}
	if s := c.State().Addon; !strings.Contains(s.Error, "checksum") || !strings.Contains(s.Error, "as it was") {
		t.Errorf("the page says %+v", s)
	}
}

// slowGithub answers the release list at once and holds the zip's download until its request ends,
// which it reports on cancelled.
type slowGithub struct {
	list      string
	started   chan struct{}
	cancelled chan struct{}
}

func (g *slowGithub) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() == addonReleases {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(g.list)), Header: http.Header{}, Request: r}, nil
	}
	close(g.started)
	<-r.Context().Done()
	close(g.cancelled)
	return nil, r.Context().Err()
}

func TestTurningItOffStopsTheDownloadUnderWay(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	g := &slowGithub{list: addonRelease(sum("zip")), started: make(chan struct{}), cancelled: make(chan struct{})}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	c.owned.Go(func() { c.keepAddonUpdated(ctx) })

	waitFor(t, g.started, "the download to start")
	if err := c.SetAddonUpdates(t.Context(), false, false); err != nil {
		t.Fatal(err)
	}

	waitFor(t, g.cancelled, "turning it off to stop the download")
	if addonupdate.Installed(addOns) != "0.16.0" || c.State().Addon.Error != "" {
		t.Errorf("after turning it off: %q, %+v", addonupdate.Installed(addOns), c.State().Addon)
	}
}

func TestStartingPutsBackAnAddonWhoseInstallWasInterrupted(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	// The companion stopped mid-swap: the old GoldForever is in the backup, the new one not yet in place.
	backup := filepath.Join(addOns, ".GoldForever-backup")
	os.MkdirAll(backup, 0o755)
	if err := os.Rename(filepath.Join(addOns, "GoldForever"), filepath.Join(backup, "GoldForever")); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(addOns, ".GoldForever-update")
	os.MkdirAll(filepath.Join(staging, "GoldForever"), 0o755)
	os.WriteFile(filepath.Join(staging, "folders.txt"), []byte("GoldForever\n"), 0o644)

	c.recoverAddons()

	if addonupdate.Installed(addOns) != "0.16.0" {
		t.Error("the old addon is not back")
	}
	if s := c.State().Addon; !strings.Contains(s.Error, "interrupted") {
		t.Errorf("the page says %+v", s)
	}
}

func waitFor(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("waited 5 s for %s", what)
	}
}

func TestAnInstallThatAddsAnAddonFolderSaysToRestartTheGame(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"GoldForever/GoldForever.toc", "GoldForever_Auctions/GoldForever_Auctions.toc"} {
		f, _ := w.Create(name)
		f.Write([]byte(addonToc("0.16.1")))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	z := buf.String()
	g := &github{files: map[string]string{addonReleases: addonRelease(sum(z)), addonZip: z}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)

	c.checkAddon(t.Context())

	if addonupdate.Installed(filepath.Join(addOns)) != "0.16.1" {
		t.Fatalf("installed: %q", addonupdate.Installed(addOns))
	}
	if s := c.State().Addon; s.Status != "Addon updated to v0.16.1 — restart the game: /reload does not load a new addon folder" {
		t.Errorf("the page says %+v", s)
	}
}

const (
	addonSnapshotRelease = "https://api.github.com/repos/gold-forever/addon/releases/tags/addon-snapshot"
	addonSnapshotVersion = "0.16.1-snapshot.202611031530.2ceabf9"
	addonSnapshotZip     = "https://github.com/gold-forever/addon/releases/download/addon-snapshot/GoldForever-" + addonSnapshotVersion + ".zip"
)

// addonZipAt is a zip of the addon whose .toc names version.
func addonZipAt(t *testing.T, version string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("GoldForever/GoldForever.toc")
	f.Write([]byte(addonToc(version)))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func addonSnapshot(zipSum string) string {
	return `{"tag_name":"addon-snapshot","prerelease":true,"draft":false,"assets":[{"name":"GoldForever-` + addonSnapshotVersion +
		`.zip","browser_download_url":"` + addonSnapshotZip + `","digest":"sha256:` + zipSum + `"}]}`
}

func setChannel(t *testing.T, c *App, channel string) {
	t.Helper()
	if err := c.SetAddonChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
}

func TestTheAddonChannelIsReleaseByDefaultAndTheOldPreReleaseSettingReadsAsBeta(t *testing.T) {
	c, _ := fixture(t)
	if got := c.State().Addon.Channel; got != "release" {
		t.Errorf("by default the channel is %q", got)
	}

	if err := c.settings.Update(func(cfg *config.Config) error { cfg.AddonPrereleases = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := c.State().Addon.Channel; got != "beta" {
		t.Errorf("with addonPrereleases the channel is %q", got)
	}

	setChannel(t, c, "release")
	if cfg := c.settings.Get(); c.State().Addon.Channel != "release" || cfg.AddonPrereleases {
		t.Errorf("picking release: %q, %+v", c.State().Addon.Channel, cfg)
	}
}

func TestPickingAChannelTheCompanionDoesNotKnowIsRefused(t *testing.T) {
	c, _ := fixture(t)

	err := c.SetAddonChannel(t.Context(), "nightly")

	var p interface{ PlayerError() }
	if !errors.As(err, &p) || c.State().Addon.Channel != "release" {
		t.Errorf("an unknown channel: %v, now %q", err, c.State().Addon.Channel)
	}
}

func TestTheBetaChannelInstallsTheNewestPreRelease(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	const betaZip = "https://github.com/gold-forever/addon/releases/download/addon-v0.17.0-beta.1/GoldForever-0.17.0-beta.1.zip"
	z := addonZipAt(t, "0.17.0-beta.1")
	list := `[{"tag_name":"addon-v0.17.0-beta.1","prerelease":true,"draft":false,"assets":[{"name":"GoldForever-0.17.0-beta.1.zip","browser_download_url":"` +
		betaZip + `","digest":"sha256:` + sum(z) + `"}]},` + strings.TrimPrefix(addonRelease(sum("release")), "[")
	g := &github{files: map[string]string{addonReleases: list, betaZip: z}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)
	setChannel(t, c, "beta")

	c.checkAddon(t.Context())

	if got := addonupdate.Installed(addOns); got != "0.17.0-beta.1" {
		t.Errorf("on beta, installed %q, asked %v", got, g.asked)
	}
}

func TestTheSnapshotChannelInstallsTheSnapshotAndThenOnlyWhenItChanges(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	z := addonZipAt(t, addonSnapshotVersion)
	g := &github{files: map[string]string{addonSnapshotRelease: addonSnapshot(sum(z)), addonSnapshotZip: z}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)
	setChannel(t, c, "snapshot")

	c.checkAddon(t.Context())

	if got := addonupdate.Installed(addOns); got != addonSnapshotVersion {
		t.Fatalf("on snapshot, installed %q, asked %v", got, g.asked)
	}
	if s := c.State().Addon; s.Status != "Addon updated to v"+addonSnapshotVersion+" — /reload in game" {
		t.Errorf("the page says %+v", s)
	}
	g.asked = nil
	c.checkAddon(t.Context())
	if len(g.asked) != 1 || g.asked[0] != addonSnapshotRelease {
		t.Errorf("the same snapshot again, the companion asked %v", g.asked)
	}
}

func TestSwitchingBackToReleaseReturnsASnapshotToTheNewestRelease(t *testing.T) {
	c, save := fixture(t)
	addOns := addonInstalled(t, save)
	snap, rel := addonZipAt(t, addonSnapshotVersion), addonZipOf(t)
	g := &github{files: map[string]string{
		addonSnapshotRelease: addonSnapshot(sum(snap)), addonSnapshotZip: snap,
		addonReleases: addonRelease(sum(rel)), addonZip: rel,
	}}
	c.addons = addonupdate.Source{Client: &http.Client{Transport: g}}
	addonOn(t, c)
	setChannel(t, c, "snapshot")
	c.checkAddon(t.Context())
	if got := addonupdate.Installed(addOns); got != addonSnapshotVersion {
		t.Fatalf("on snapshot, installed %q", got)
	}

	setChannel(t, c, "release")
	c.checkAddon(t.Context())

	if got := addonupdate.Installed(addOns); got != "0.16.1" {
		t.Errorf("back on release, installed %q", got)
	}
}

func TestChangingTheChannelRestartsTheCheck(t *testing.T) {
	c, _ := fixture(t)
	addonOn(t, c)
	wish, changed := wishOf(c.settings.Get()), make(chan struct{})
	go func() { c.untilAddonWishChanges(t.Context(), wish); close(changed) }()

	setChannel(t, c, "snapshot")

	waitFor(t, changed, "the new channel to restart the check")
}
