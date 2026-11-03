package addonupdate

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// zipOfFiles is a release zip holding files, by slash-separated path.
func zipOfFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func toc(version string) string {
	return "## Interface: 16001\n## Title: Gold Forever\n## Version: " + version + "\n"
}

// addOns is an AddOns folder holding files, by slash-separated path.
func addOns(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return "(missing)"
	}
	return string(data)
}

func present(dir, name string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
	return err == nil
}

// installed016 is an AddOns folder with addon 0.16.0, its auctions addon, the companion's prices and
// another addon.
func installed016(t *testing.T) string {
	return addOns(t, map[string]string{
		"GoldForever/GoldForever.toc":                   toc("0.16.0"),
		"GoldForever/Old.lua":                           "old",
		"GoldForever_Auctions/GoldForever_Auctions.toc": toc("0.16.0"),
		"GoldForever_Data/Data.lua":                     "prices",
		"Details/Details.toc":                           "another addon",
	})
}

func release0161(t *testing.T) []byte {
	return zipOfFiles(t, map[string]string{
		"GoldForever/GoldForever.toc":                   toc("0.16.1"),
		"GoldForever/New.lua":                           "new",
		"GoldForever_Auctions/GoldForever_Auctions.toc": toc("0.16.1"),
		"GoldForever_Data/Data.lua":                     "from the zip",
	})
}

func TestInstalledReadsTheVersionFromTheAddonsToc(t *testing.T) {
	dir := addOns(t, map[string]string{"GoldForever/GoldForever.toc": "## Interface: 16001\r\n## Version: 0.16.1\r\n"})

	if v := Installed(dir); v != "0.16.1" {
		t.Errorf("installed: %q", v)
	}
	if v := Installed(t.TempDir()); v != "" {
		t.Errorf("no addon installed: %q", v)
	}
}

func TestInstallReplacesEveryAddonFolderButThePrices(t *testing.T) {
	dir := installed016(t)

	added, err := Install(dir, release0161(t))
	if err != nil {
		t.Fatal(err)
	}

	if len(added) != 0 {
		t.Errorf("folders the game already had are reported as added: %v", added)
	}
	if Installed(dir) != "0.16.1" || read(t, dir, "GoldForever/New.lua") != "new" || present(dir, "GoldForever/Old.lua") {
		t.Error("GoldForever was not replaced whole")
	}
	if read(t, dir, "GoldForever_Auctions/GoldForever_Auctions.toc") != toc("0.16.1") {
		t.Error("GoldForever_Auctions was not replaced")
	}
	if read(t, dir, "GoldForever_Data/Data.lua") != "prices" {
		t.Error("the companion's prices were replaced")
	}
	if read(t, dir, "Details/Details.toc") != "another addon" {
		t.Error("another addon was touched")
	}
	if read(t, dir, backupDir+"/GoldForever/Old.lua") != "old" {
		t.Error("the old addon is not kept as a backup")
	}
	if present(dir, stagingDir) {
		t.Error("the unpacked release is left beside the addons")
	}
}

func TestInstallRefusesAZipWithFoldersThatAreNotTheAddons(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"another addon":   {"GoldForever/GoldForever.toc": toc("0.16.1"), "Details/Details.toc": "x"},
		"out of AddOns":   {"GoldForever/GoldForever.toc": toc("0.16.1"), "../evil.lua": "x"},
		"a loose file":    {"GoldForever/GoldForever.toc": toc("0.16.1"), "readme.txt": "x"},
		"no Gold Forever": {"GoldForever_Auctions/GoldForever_Auctions.toc": toc("0.16.1")},
	} {
		t.Run(name, func(t *testing.T) {
			dir := installed016(t)

			if _, err := Install(dir, zipOfFiles(t, files)); err == nil {
				t.Error("installed")
			}
			if Installed(dir) != "0.16.0" || present(dir, stagingDir) || present(dir, "Details/Details.toc") && read(t, dir, "Details/Details.toc") != "another addon" {
				t.Error("the installed addons changed")
			}
		})
	}
}

func TestAFailedSwapPutsEveryOldFolderBack(t *testing.T) {
	dir := installed016(t)
	swaps := 0
	rename = func(from, to string) error {
		if filepath.Base(filepath.Dir(from)) == stagingDir {
			swaps++
			if swaps == 2 {
				return errors.New("the folder is in use")
			}
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })

	if _, err := Install(dir, release0161(t)); err == nil {
		t.Fatal("a failed swap was reported as installed")
	}

	if Installed(dir) != "0.16.0" || read(t, dir, "GoldForever/Old.lua") != "old" || present(dir, "GoldForever/New.lua") {
		t.Error("GoldForever is not the old one again")
	}
	if read(t, dir, "GoldForever_Auctions/GoldForever_Auctions.toc") != toc("0.16.0") {
		t.Error("GoldForever_Auctions is not the old one again")
	}
	if present(dir, stagingDir) || present(dir, backupDir+"/GoldForever") {
		t.Error("the swap's folders are left behind")
	}
}

func TestAFailedSwapRemovesAFolderTheReleaseAdded(t *testing.T) {
	dir := addOns(t, map[string]string{"GoldForever/GoldForever.toc": toc("0.16.0")})
	rename = func(from, to string) error {
		if filepath.Base(filepath.Dir(from)) == stagingDir && filepath.Base(from) == "GoldForever_Mail" {
			return errors.New("the folder is in use")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })

	// GoldForever and the new GoldForever_Auctions are swapped in (sorted first); GoldForever_Mail fails.
	_, err := Install(dir, zipOfFiles(t, map[string]string{
		"GoldForever/GoldForever.toc":                   toc("0.16.1"),
		"GoldForever_Auctions/GoldForever_Auctions.toc": toc("0.16.1"),
		"GoldForever_Mail/GoldForever_Mail.toc":         toc("0.16.1"),
	}))

	if err == nil {
		t.Fatal("a failed swap was reported as installed")
	}
	if Installed(dir) != "0.16.0" || present(dir, "GoldForever_Auctions") || present(dir, "GoldForever_Mail") {
		t.Error("a folder the release added is left after the rollback")
	}
}

func TestStartingAfterAnInterruptedInstallPutsTheOldAddonBack(t *testing.T) {
	// The companion stopped mid-swap: GoldForever is the new one, GoldForever_Auctions is in the backup
	// and not yet replaced.
	dir := addOns(t, map[string]string{
		"GoldForever/GoldForever.toc":                                 toc("0.16.1"),
		backupDir + "/GoldForever/GoldForever.toc":                    toc("0.16.0"),
		backupDir + "/GoldForever_Auctions/GoldForever_Auctions.toc":  toc("0.16.0"),
		stagingDir + "/GoldForever_Auctions/GoldForever_Auctions.toc": toc("0.16.1"),
		stagingDir + "/" + manifest:                                   "GoldForever\nGoldForever_Auctions\n",
	})

	restored, err := Recover(dir)

	if err != nil || !restored {
		t.Fatalf("recover: %v, %v", restored, err)
	}
	if Installed(dir) != "0.16.0" || read(t, dir, "GoldForever_Auctions/GoldForever_Auctions.toc") != toc("0.16.0") {
		t.Error("the old addon is not back")
	}
	if present(dir, stagingDir) || present(dir, backupDir) {
		t.Error("the swap's folders are left behind")
	}
}

func TestStartingAfterAnInstallRemovesItsBackup(t *testing.T) {
	dir := installed016(t)
	if _, err := Install(dir, release0161(t)); err != nil {
		t.Fatal(err)
	}

	restored, err := Recover(dir)

	if err != nil || restored {
		t.Fatalf("recover: %v, %v", restored, err)
	}
	if Installed(dir) != "0.16.1" || present(dir, backupDir) {
		t.Error("the backup outlives the next start, or the new addon is gone")
	}
}
