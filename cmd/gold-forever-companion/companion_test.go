package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/config"
	"github.com/gold-forever/companion/internal/install"
	"github.com/gold-forever/companion/internal/page"
)

const validToken = "gf_0123456789abcdefghijABCDEFGHIJklmnopqrstuvw"

// fixture is a companion over a temporary home with one WoW install holding one Forever save.
func fixture(t *testing.T) (*companion, string) {
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
	c := newCompanion(companionOptions{
		version:    "v0.1.0",
		site:       "https://gold-forever.example",
		running:    layout.Exe,
		layout:     layout,
		cfgPath:    filepath.Join(layout.Config, "config.json"),
		dataDir:    data,
		candidates: []string{wow},
		quit:       func() {},
	})
	return c, save
}

func TestLinkTakesOnlyATokenAndKeepsItForItsOwner(t *testing.T) {
	c, _ := fixture(t)
	var bad interface{ Error() string }
	if err := c.Link("my password"); err == nil || !errors.As(err, &bad) {
		t.Fatalf("anything but a token must be refused, got %v", err)
	}
	if c.State().Linked {
		t.Fatal("a refused token linked the companion")
	}
	before := c.changes()
	if err := c.Link(validToken); err != nil {
		t.Fatal(err)
	}
	if !c.State().Linked {
		t.Error("not linked")
	}
	if info, err := os.Stat(c.cfgPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the token's file must be the owner's only: %v %v", info.Mode(), err)
	}
	select {
	case <-before:
	default:
		t.Error("linking must restart the upload loop with the new token")
	}
}

func TestTheFirstRunPanelSaysWhatWasInstalledUntilThePlayerHasSeenIt(t *testing.T) {
	c, _ := fixture(t)
	s := c.State()
	if !s.FirstRun || !strings.Contains(strings.Join(s.Installed, "\n"), c.layout.Exe) ||
		!strings.Contains(strings.Join(s.Installed, "\n"), c.layout.AutostartAt) {
		t.Fatalf("the first-run panel must say where the companion and its autostart are: %+v", s)
	}
	if err := c.Welcomed(); err != nil {
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
	if err := c.SetSaveOn(save, false); err != nil || c.State().Saves[0].On || !c.isOff(save) {
		t.Errorf("switching off: %v %+v", err, c.State().Saves)
	}
	if err := c.SetSaveOn("/etc/passwd", false); err == nil {
		t.Error("only a save the companion found can be switched")
	}
}

func TestAddFolderWantsAWoWFolderWithASave(t *testing.T) {
	c, _ := fixture(t)
	if err := c.AddFolder(t.TempDir()); err == nil {
		t.Error("a folder with no GoldForever.lua under it is refused, with advice")
	}
	other := filepath.Join(t.TempDir(), "World of Warcraft")
	path := filepath.Join(other, "_retail_", "WTF", "Account", "B", "SavedVariables", "GoldForever.lua")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("GoldForeverDB = {\n}\n"), 0o644)
	if err := c.AddFolder(other); err != nil {
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
	copies := c.History().Copies
	if len(copies) != 1 || strings.Contains(copies[0].ID, string(os.PathSeparator)) {
		t.Fatalf("copies: %+v", copies)
	}
	if data, ok := c.CopyBytes(copies[0].ID); !ok || string(data) != string(original) {
		t.Error("view bytes must show the copy exactly")
	}
	if err := c.Link(validToken); err != nil { // so the config file exists, holding a token
		t.Fatal(err)
	}
	if data, ok := c.CopyBytes(c.cfgPath); ok || strings.Contains(string(data), validToken) {
		t.Error("a path is not an ID: the config holds the token")
	}
	if err := c.Restore(copies[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(save); string(got) != string(original) {
		t.Error("restore did not write the copy back")
	}
}

func TestAutostartFollowsTheCheckbox(t *testing.T) {
	c, _ := fixture(t)
	if err := c.SetAutostart(true); err != nil || !c.State().Autostart {
		t.Fatalf("on: %v", err)
	}
	if err := c.SetAutostart(false); err != nil || c.State().Autostart {
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
	if err := c.Uninstall(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(instanceFile); !os.IsNotExist(err) {
		t.Error("the instance file names a page that no longer runs")
	}
	if _, err := os.Stat(c.cfgPath); !os.IsNotExist(err) {
		t.Error("the token stays on the computer after uninstalling")
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Error("uninstall must stop the companion")
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
func linkingState(t *testing.T, c *companion, want string) {
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

var testCode = api.DeviceCode{UserCode: "BCDF-2345", VerificationURI: "https://gold-forever.example/link",
	VerificationURIComplete: "https://gold-forever.example/link?code=BCDF2345", ExpiresIn: 600, Interval: 5}

func TestLinkingByCodeShowsTheCodeOpensTheSiteAndSavesTheToken(t *testing.T) {
	c, _ := fixture(t)
	approved := make(chan struct{})
	var opened []string
	c.openURL = func(u string) { opened = append(opened, u) }
	c.linker = func(ctx context.Context, show func(api.DeviceCode), save func(string) error) error {
		show(testCode)
		<-approved
		return save(validToken)
	}

	if err := c.StartDeviceLink(); err != nil {
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
	if !c.State().Linked || c.settings().Token != validToken {
		t.Error("an approved link must leave the companion linked")
	}
}

func TestLinkingByCodeThatIsDeniedOrExpiresSavesNothing(t *testing.T) {
	for want, err := range map[string]error{"denied": api.ErrLinkDenied, "expired": api.ErrLinkExpired} {
		c, _ := fixture(t)
		c.openURL = func(string) {}
		c.linker = func(ctx context.Context, show func(api.DeviceCode), save func(string) error) error {
			show(testCode)
			return err
		}
		if err := c.StartDeviceLink(); err != nil {
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
	c.linker = func(ctx context.Context, show func(api.DeviceCode), save func(string) error) error {
		starts++
		show(testCode)
		<-release
		return ctx.Err()
	}
	c.StartDeviceLink()
	linkingState(t, c, "waiting")
	c.StartDeviceLink()
	time.Sleep(20 * time.Millisecond)
	if starts != 1 {
		t.Errorf("a second press started %d links in all, want 1", starts)
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
	r := c.refresher(pricesServer{twoItems}, nil)

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
	if got[0].Dings != "" {
		t.Errorf("a file without dings shows %q", got[0].Dings)
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
		if err := c.refresher(pricesServer{twoItems}, dingsServer{body}).Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := c.State().Prices; len(got) != 1 || got[0].Dings != want {
			t.Errorf("%s: shows %+v, want %q", body, got, want)
		}
	}
}

func TestTheUploadLoopEndsOnlyOnceItsPriceLoopHasEnded(t *testing.T) {
	c, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/prices" {
			cancel()
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"status":"imported","saves":1}`)
	}))
	defer srv.Close()
	if err := c.update(func(cfg *config.Config) error { cfg.Token, cfg.URL = validToken, srv.URL; return nil }); err != nil {
		t.Fatal(err)
	}

	c.loop(ctx)

	if left := stillRunning(); left != "" {
		t.Fatalf("the loop returned with its price loop still running:\n%s", left)
	}
}
