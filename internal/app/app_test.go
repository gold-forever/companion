package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/platform/install"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/server"
	"github.com/gold-forever/companion/internal/ui/page"
)

const validToken = "gf_0123456789abcdefghijABCDEFGHIJklmnopqrstuvw"

// fixture is a companion over a temporary home with one WoW install holding one Forever save.
func fixture(t *testing.T) (*App, string) {
	t.Helper()
	home := t.TempDir()
	wow := filepath.Join(home, "Games", "World of Warcraft")
	save := filepath.Join(wow, "_forever_", "WTF", "Account", "ACCOUNT1", "SavedVariables", "GoldForever.lua")
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(save, []byte("GoldForeverDB = {\n[\"client\"] = {\n[\"interface\"] = 16001,\n},\n}\n"), 0o644)
	layout := install.For(install.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
	data := layout.Data[0]
	c := newApp(appOptions{
		version:    "v0.1.0",
		site:       "https://gold-forever.example",
		running:    layout.Exe,
		layout:     layout,
		cfgPath:    filepath.Join(layout.Config, "config.json"),
		dataDir:    data,
		candidates: []string{wow},
		quit:       func() {},
		life:       t.Context(),
	})
	t.Cleanup(func() { waitForOwned(t, c) })
	return c, save
}

// The next change of a setting would write an empty config over the token, the folders and the
// switched-off saves; the unreadable file is kept aside instead, and the page says so.
func TestAnUnreadableSettingsFileIsKeptAsideAndThePageSaysSo(t *testing.T) {
	home := t.TempDir()
	layout := install.For(install.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }})
	cfgPath := filepath.Join(layout.Config, "config.json")
	damaged := `{"token":"gf_x","folders":["/games/World of Warcraft"`
	if err := os.MkdirAll(layout.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newApp(appOptions{running: layout.Exe, layout: layout, cfgPath: cfgPath, dataDir: layout.Data[0], quit: func() {}})
	if err := c.Welcomed(t.Context()); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(cfgPath + ".bad"); string(data) != damaged {
		t.Errorf("the damaged settings were not kept: %q", data)
	}
	if w := c.State().Warning; !strings.Contains(w, cfgPath+".bad") {
		t.Errorf("the page does not say where they are: %q", w)
	}
}

func TestAMacAppOnItsDiskImageSaysToMoveItAndRecordsNoAutostart(t *testing.T) {
	home := t.TempDir()
	layout := install.For(install.Env{GOOS: "darwin", Home: home, Getenv: func(string) string { return "" }})
	onDiskImage := "/Volumes/Gold Forever Companion/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
	c := newApp(appOptions{running: onDiskImage, layout: layout, cfgPath: filepath.Join(home, "config.json"), dataDir: layout.Data[0], quit: func() {}})

	if err := c.SetAutostart(t.Context(), true); !errors.Is(err, ErrMoveToApplications) || layout.Autostart.Enabled() {
		t.Errorf("autostart recorded for a disk image: %v", err)
	}
	if !strings.Contains(strings.Join(c.State().Installed, "\n"), "Drag the app to Applications") {
		t.Errorf("the first-run panel does not say to move the app: %q", c.State().Installed)
	}
}

func TestLinkTakesOnlyATokenAndKeepsItForItsOwner(t *testing.T) {
	c, _ := fixture(t)
	if err := c.Link(t.Context(), "my password"); !errors.Is(err, ErrNotAToken) {
		t.Fatalf("anything but a token must be refused, got %v", err)
	}
	if c.State().Linked {
		t.Fatal("a refused token linked the companion")
	}
	before := c.settings.Changes()
	if err := c.Link(t.Context(), validToken); err != nil {
		t.Fatal(err)
	}
	if !c.State().Linked {
		t.Error("not linked")
	}
	if info, err := os.Stat(c.settings.Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the token's file must be the owner's only: %v %v", info.Mode(), err)
	}
	select {
	case <-before:
	default:
		t.Error("linking must restart the upload loop with the new token")
	}
}

func TestThePageSaysWhatEveryUploadShares(t *testing.T) {
	c, _ := fixture(t)
	if got := c.State().Sharing; got != link.SharingLine {
		t.Errorf("sharing = %q", got)
	}
}

func TestTheFirstRunPanelSaysWhatWasInstalledUntilThePlayerHasSeenIt(t *testing.T) {
	c, _ := fixture(t)
	s := c.State()
	if !s.FirstRun || !strings.Contains(strings.Join(s.Installed, "\n"), c.layout.Exe) ||
		!strings.Contains(strings.Join(s.Installed, "\n"), c.layout.AutostartAt) {
		t.Fatalf("the first-run panel must say where the companion and its autostart are: %+v", s)
	}
	if err := c.Welcomed(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.State().FirstRun {
		t.Error("the panel shows once")
	}
}

func TestSavesAreFoundWithTheirGameAndCanBeSwitchedOff(t *testing.T) {
	c, save := fixture(t)
	saves := c.State().Saves
	if len(saves) != 1 || saves[0].Path != save || saves[0].Game != "forever" || !saves[0].On {
		t.Fatalf("saves: %+v", saves)
	}
	if err := c.SetSaveOn(t.Context(), save, false); err != nil || c.State().Saves[0].On || !c.isOff(save) {
		t.Errorf("switching off: %v %+v", err, c.State().Saves)
	}
	if err := c.SetSaveOn(t.Context(), "/etc/passwd", false); !errors.Is(err, ErrUnknownSave) {
		t.Error("only a save the companion found can be switched")
	}
}

func TestASaveRefusedAsTooLargeSaysItIsRetriedDaily(t *testing.T) {
	c, save := fixture(t)
	if note := c.State().Saves[0].Note; note != "" {
		t.Fatalf("note before any refusal: %q", note)
	}

	if err := c.refused.Refuse(save, "0123", time.Now()); err != nil {
		t.Fatal(err)
	}

	if note := c.State().Saves[0].Note; note != "refused as too large, retrying daily" {
		t.Fatalf("note = %q", note)
	}
	if !refused.Load(filepath.Join(c.dataDir, refusedFile)).Holds(save) {
		t.Fatal("the refusal is not kept in the data folder for the next start")
	}
}

func TestAddFolderWantsAWoWFolderWithASave(t *testing.T) {
	c, _ := fixture(t)
	var none *NoSaveError
	if err := c.AddFolder(t.Context(), t.TempDir()); !errors.As(err, &none) {
		t.Error("a folder with no GoldForever.lua under it is refused, with advice")
	}
	other := filepath.Join(t.TempDir(), "World of Warcraft")
	path := filepath.Join(other, "_retail_", "WTF", "Account", "B", "SavedVariables", "GoldForever.lua")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("GoldForeverDB = {\n}\n"), 0o644)
	if err := c.AddFolder(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if n := len(c.State().Saves); n != 2 {
		t.Errorf("%d saves after adding a folder", n)
	}
}

func TestCopiesAreReachedByIDOnlyAndRestoreWritesTheSaveBack(t *testing.T) {
	c, save := fixture(t)
	original, _ := os.ReadFile(save)
	if _, _, err := c.backups.Add(save, original); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(save, []byte("GoldForeverDB = {\n-- the game lost everything\n}\n"), 0o644)
	copies := c.History(1).Copies
	if len(copies) != 1 || strings.Contains(copies[0].ID, string(os.PathSeparator)) {
		t.Fatalf("copies: %+v", copies)
	}
	if data, ok := c.CopyBytes(copies[0].ID); !ok || string(data) != string(original) {
		t.Error("view bytes must show the copy exactly")
	}
	if err := c.Link(t.Context(), validToken); err != nil { // so the config file exists, holding a token
		t.Fatal(err)
	}
	if data, ok := c.CopyBytes(c.settings.Path()); ok || strings.Contains(string(data), validToken) {
		t.Error("a path is not an ID: the config holds the token")
	}
	if err := c.Restore(t.Context(), copies[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(save); string(got) != string(original) {
		t.Error("restore did not write the copy back")
	}
	if err := c.Restore(t.Context(), "no-such-copy"); !errors.Is(err, ErrCopyGone) {
		t.Errorf("restoring a copy no longer kept: %v", err)
	}
}

func TestAutostartFollowsTheCheckbox(t *testing.T) {
	c, _ := fixture(t)
	if err := c.SetAutostart(t.Context(), true); err != nil || !c.State().Autostart {
		t.Fatalf("on: %v", err)
	}
	if err := c.SetAutostart(t.Context(), false); err != nil || c.State().Autostart {
		t.Errorf("off: %v", err)
	}
}

func TestUninstallRemovesTheInstanceFileAndQuits(t *testing.T) {
	c, _ := fixture(t)
	quit := make(chan struct{})
	c.quit = func() { close(quit) }
	os.MkdirAll(c.dataDir, 0o700)
	instanceFile := filepath.Join(c.dataDir, "instance.json")
	os.WriteFile(instanceFile, []byte("{}"), 0o600)
	if err := c.Uninstall(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(instanceFile); !os.IsNotExist(err) {
		t.Error("the instance file names a page that no longer runs")
	}
	if _, err := os.Stat(c.settings.Path()); !os.IsNotExist(err) {
		t.Error("the token stays on the computer after uninstalling")
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Error("uninstall must stop the companion")
	}
}

// Uninstalling quits a moment later, so the page gets its answer first; a companion that stops sooner
// leaves no goroutine waiting to quit it.
func TestUninstallLeavesNothingRunningOnceTheCompanionStops(t *testing.T) {
	c, _ := fixture(t)
	life, stop := context.WithCancel(t.Context())
	c.life = life
	os.MkdirAll(c.dataDir, 0o700)
	if err := c.Uninstall(t.Context(), true); err != nil {
		t.Fatal(err)
	}

	stopped(t, c, stop)

	if left := stillRunning(); left != "" {
		t.Fatalf("the companion stopped with an uninstall still running:\n%s", left)
	}
}

// stopped stops the companion c and waits for what it started.
func stopped(t *testing.T, c *App, stop context.CancelFunc) {
	t.Helper()
	stop()
	waitForOwned(t, c)
}

// waitForOwned waits for the goroutines the stopped companion c started.
func waitForOwned(t *testing.T, c *App) {
	t.Helper()
	done := make(chan struct{})
	go func() { c.owned.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("the companion's goroutines still run after it stopped:\n%s", stillRunning())
	}
}

func TestTheStatusIsTheLatestLineTheLoopWrote(t *testing.T) {
	c, _ := fixture(t)
	c.status.Write([]byte("12:00:00 sent _forever_: imported\n"))
	if got := c.State().Status; got != "12:00:00 sent _forever_: imported" {
		t.Errorf("status %q", got)
	}
}

// linkingState waits until the page would show want for the link in progress.
func linkingState(t *testing.T, c *App, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if l := c.State().Linking; l != nil && l.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the link never reached %q: %+v", want, c.State().Linking)
}

var testCode = server.DeviceCode{UserCode: "BCDF-2345", VerificationURI: "https://gold-forever.example/link",
	VerificationURIComplete: "https://gold-forever.example/link?code=BCDF2345", ExpiresIn: 600, Interval: 5}

func TestLinkingByCodeShowsTheCodeOpensTheSiteAndSavesTheToken(t *testing.T) {
	c, _ := fixture(t)
	approved := make(chan struct{})
	var opened []string
	c.openURL = func(u string) { opened = append(opened, u) }
	c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
		show(testCode)
		<-approved
		return save(validToken)
	}

	if err := c.StartDeviceLink(t.Context()); err != nil {
		t.Fatal(err)
	}
	linkingState(t, c, "waiting")
	if l := c.State().Linking; l.UserCode != "BCDF-2345" || l.URL != testCode.VerificationURIComplete {
		t.Errorf("the page must show the code and its link: %+v", l)
	}
	if len(opened) != 1 || opened[0] != testCode.VerificationURIComplete {
		t.Errorf("the browser must open the approval page once: %v", opened)
	}
	close(approved)
	linkingState(t, c, "linked")
	if !c.State().Linked || c.settings.Get().Token != validToken {
		t.Error("an approved link must leave the companion linked")
	}
}

func TestLinkingByCodeThatIsDeniedOrExpiresSavesNothing(t *testing.T) {
	for want, err := range map[string]error{"denied": server.ErrLinkDenied, "expired": server.ErrLinkExpired} {
		c, _ := fixture(t)
		c.openURL = func(string) {}
		c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
			show(testCode)
			return err
		}
		if err := c.StartDeviceLink(t.Context()); err != nil {
			t.Fatal(err)
		}
		linkingState(t, c, want)
		if c.State().Linked {
			t.Errorf("%s: a link that was not approved linked the companion", want)
		}
	}
}

func TestASecondLinkWhileOneWaitsStartsNone(t *testing.T) {
	c, _ := fixture(t)
	release := make(chan struct{})
	defer close(release)
	starts := 0
	c.openURL = func(string) {}
	c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
		starts++
		show(testCode)
		<-release
		return ctx.Err()
	}
	c.StartDeviceLink(t.Context())
	linkingState(t, c, "waiting")
	c.StartDeviceLink(t.Context())
	time.Sleep(20 * time.Millisecond)
	if starts != 1 {
		t.Errorf("a second press started %d links in all, want 1", starts)
	}
}

// A link outlives the press that started it but not the companion: it waits for an approval on a
// goroutine the companion stops and waits for.
func TestALinkWaitingForApprovalEndsWhenTheCompanionStops(t *testing.T) {
	c, _ := fixture(t)
	life, stop := context.WithCancel(t.Context())
	c.life = life
	c.linker = func(ctx context.Context, show func(server.DeviceCode), save func(string) error) error {
		show(testCode)
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // a linker takes a moment to give up
		return ctx.Err()
	}
	press, released := context.WithCancel(t.Context())
	if err := c.StartDeviceLink(press); err != nil {
		t.Fatal(err)
	}
	linkingState(t, c, "waiting")
	released()
	time.Sleep(100 * time.Millisecond) // longer than a linker takes to give up
	if l := c.State().Linking; l.State != "waiting" {
		t.Fatalf("the link ended with the press that started it: %+v", l)
	}

	stopped(t, c, stop)

	if left := stillRunning(); left != "" {
		t.Fatalf("the companion stopped with a link still waiting:\n%s", left)
	}
}

// pricesServer answers the account's markets for every game with body.
type pricesServer struct{ body string }

func (p pricesServer) Prices(context.Context, string, string, string) ([]byte, string, error) {
	return []byte(p.body), `"e1"`, nil
}

const twoItems = `{"v":1,"game":"forever","at":5,"markets":[{"market":"forever-EU-normal-horde","items":[` +
	`{"id":1,"value":1,"minBuyout":1,"players":1,"scannedAt":1},{"id":2,"value":1,"minBuyout":1,"players":1,"scannedAt":1}]}]}`

func TestThePageShowsEachFlavoursPriceFileAndHowItsLastFetchWent(t *testing.T) {
	c, save := fixture(t)
	if got := c.State().Prices; len(got) != 1 || got[0].Fetched != nil || got[0].Outcome != "" || got[0].Size != 0 {
		t.Fatalf("before a fetch: %+v", got)
	}
	r := c.refresher(pricesServer{twoItems}, nil, nil)

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := c.State().Prices
	flavour := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(save)))))
	lua, _ := os.ReadFile(filepath.Join(flavour, "Interface", "AddOns", "GoldForever_Data", "Data.lua"))
	if len(got) != 1 || got[0].Flavour != "World of Warcraft › _forever_" || got[0].Game != "forever" || got[0].Items != 2 ||
		len(got[0].Markets) != 1 || got[0].Markets[0] != (page.PriceMarket{Market: "forever-EU-normal-horde", Items: 2}) ||
		got[0].Size != int64(len(lua)) || got[0].Fetched == nil || got[0].Outcome != "ok" {
		t.Fatalf("after a fetch: %+v", got)
	}
	if got[0].Dings != "" || got[0].Shared != "" {
		t.Errorf("a file without dings or shared state shows %q, %q", got[0].Dings, got[0].Shared)
	}
}

// dingsServer answers the account's dings state with body.
type dingsServer struct{ body string }

func (d dingsServer) DingsState(context.Context, string) ([]byte, string, error) {
	return []byte(d.body), `"d1"`, nil
}

func TestThePageShowsTheDingsSwitchStateEachPriceFileHolds(t *testing.T) {
	for body, want := range map[string]string{
		`{"v":1,"at":7,"webhook":true,"characters":{"Player-1-0A":true,"Player-1-0B":false,"Player-1-0C":true}}`: "Dings: webhook on · 2 characters on",
		`{"v":1,"at":7,"webhook":false,"characters":{"Player-1-0A":true}}`:                                       "Dings: webhook off · 1 character on",
		`{"v":1,"at":7,"webhook":true,"characters":{}}`:                                                          "Dings: webhook on · 0 characters on",
	} {
		c, _ := fixture(t)
		if err := c.refresher(pricesServer{twoItems}, dingsServer{body}, nil).Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := c.State().Prices; len(got) != 1 || got[0].Dings != want {
			t.Errorf("%s: shows %+v, want %q", body, got, want)
		}
	}
}

// sharedServer answers the account's shared lists and profiles with body.
type sharedServer struct{ body string }

func (s sharedServer) SharedState(context.Context, string) ([]byte, string, error) {
	return []byte(s.body), `"s1"`, nil
}

func TestThePageCountsTheListsAndProfilesEachPriceFileHolds(t *testing.T) {
	for body, want := range map[string]string{
		`{"v":1,"at":7,"lists":[{"id":"a","edited":1,"string":"GFL1:x"},{"id":"b","edited":1,"string":"GFL1:y"}],"profiles":[{"id":"a","edited":1,"string":"GFP1:z"}]}`: "Lists: 2 · Profiles: 1",
		`{"v":1,"at":7,"lists":[],"profiles":[]}`: "Lists: 0 · Profiles: 0",
	} {
		c, _ := fixture(t)
		if err := c.refresher(pricesServer{twoItems}, nil, sharedServer{body}).Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := c.State().Prices; len(got) != 1 || got[0].Shared != want {
			t.Errorf("%s: shows %+v, want %q", body, got, want)
		}
	}
}

func TestTheUploadLoopEndsOnlyOnceItsPriceLoopHasEnded(t *testing.T) {
	c, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv := &fakeServer{
		prices: func(context.Context) ([]byte, string, error) { cancel(); return nil, "", errNotServed },
		upload: func(context.Context, []byte) (server.Answer, error) {
			return server.Answer{Status: "imported", Saves: 1}, nil
		},
	}
	srv.connects(c)
	if err := c.settings.Update(func(cfg *config.Config) error { cfg.Token = validToken; return nil }); err != nil {
		t.Fatal(err)
	}

	c.loop(ctx)

	if left := stillRunning(); left != "" {
		t.Fatalf("the loop returned with its price loop still running:\n%s", left)
	}
}

// The page tells the player's own mistakes from the companion's failures by PlayerError: every refusal
// the use cases return is one.
func TestEveryRefusalIsThePlayersToFix(t *testing.T) {
	for _, err := range []error{ErrNotAToken, ErrUnknownSave, ErrCopyGone, ErrMoveToApplications, &NoSaveError{Folder: "/games"}} {
		var mistake page.PlayerError
		if !errors.As(fmt.Errorf("wrapped: %w", err), &mistake) {
			t.Errorf("%v is not the player's to fix", err)
		}
	}
}

func TestCheckingForAnUpdateShowsANewerReleaseOnly(t *testing.T) {
	for latest, notice := range map[string]bool{"v0.2.0": true, "v0.1.0": false} {
		c, _ := fixture(t)
		srv := &fakeServer{latest: func() (server.Release, error) {
			return server.Release{Version: latest, URL: "https://github.com/gold-forever/companion/releases/tag/" + latest}, nil
		}}
		srv.connects(c)

		err := c.CheckForUpdate(t.Context())

		if err != nil || (c.State().Notice != nil) != notice {
			t.Errorf("latest %s: notice %+v, err %v", latest, c.State().Notice, err)
		}
	}
}
