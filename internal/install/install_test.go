package install

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func env(t *testing.T, goos string) (Env, string) {
	t.Helper()
	home := t.TempDir()
	vars := map[string]string{
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
	}
	return Env{GOOS: goos, Home: home, Getenv: func(k string) string { return vars[k] }, RunKey: &fakeRunKey{}}, home
}

type fakeRunKey struct{ value string }

func (k *fakeRunKey) Enable(exe string) error { k.value = `"` + exe + `" --autostart`; return nil }
func (k *fakeRunKey) Disable() error          { k.value = ""; return nil }
func (k *fakeRunKey) Enabled() bool           { return k.value != "" }

// downloaded writes a stand-in for the binary the player double-clicked.
func downloaded(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "GoldForever-Companion.exe")
	if err := os.WriteFile(path, []byte("the companion"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLinuxInstallCopiesTheBinaryAddsALauncherAndStartsWithTheComputer(t *testing.T) {
	e, home := env(t, "linux")
	l := For(e)
	running := downloaded(t)
	if !l.NeedsInstall(running) {
		t.Fatal("a binary in Downloads must install itself")
	}
	if err := l.Install(running); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(home, ".local", "bin", "gold-forever-companion")
	if read(t, exe) != "the companion" {
		t.Error("the binary was not copied to ~/.local/bin")
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o100 == 0 {
		t.Error("the installed binary is not executable")
	}
	launcher := read(t, filepath.Join(home, ".local", "share", "applications", "gold-forever-companion.desktop"))
	if !strings.Contains(launcher, `Exec="`+exe+`"`) {
		t.Errorf("the launcher does not start the installed copy:\n%s", launcher)
	}
	auto := read(t, filepath.Join(home, ".config", "autostart", "gold-forever-companion.desktop"))
	if !strings.Contains(auto, `Exec="`+exe+`" --autostart`) {
		t.Errorf("autostart does not start the installed copy quietly:\n%s", auto)
	}
	if !l.Autostart.Enabled() {
		t.Error("autostart is on by default after installing")
	}
	if l.NeedsInstall(exe) {
		t.Error("the installed copy must not install itself again")
	}
}

func TestExecQuotesWhatTheDesktopSpecReserves(t *testing.T) {
	if got := quoteExec(`/home/a "b"/$x`); got != `"/home/a \"b\"/\$x"` {
		t.Errorf("quoteExec = %s", got)
	}
}

func TestUninstallRemovesEverythingAndKeepsBackupsOnlyWhenAsked(t *testing.T) {
	for _, keep := range []bool{true, false} {
		e, home := env(t, "linux")
		l := For(e)
		if err := l.Install(downloaded(t)); err != nil {
			t.Fatal(err)
		}
		for _, dir := range append([]string{l.Config}, l.Data...) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600)
		}
		if err := l.Uninstall(l.Exe, keep); err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{l.Exe, l.Shortcut, filepath.Join(home, ".config", "autostart", "gold-forever-companion.desktop"), l.Config} {
			if _, err := os.Stat(gone); !os.IsNotExist(err) {
				t.Errorf("keep=%v: %s is still there", keep, gone)
			}
		}
		for _, dir := range l.Data {
			_, err := os.Stat(dir)
			if kept := err == nil; kept != keep {
				t.Errorf("keep=%v: data folder %s kept=%v", keep, dir, kept)
			}
		}
	}
}

func TestWindowsInstallGoesToLocalAppDataWithAStartMenuShortcutAndTheRunKey(t *testing.T) {
	e, home := env(t, "windows")
	l := For(e)
	running := downloaded(t)
	if err := l.Install(running); err != nil {
		t.Fatal(err)
	}
	exe := winJoin(filepath.Join(home, "AppData", "Local"), "Gold Forever", "GoldForever-Companion.exe")
	if l.Exe != exe {
		t.Fatalf("installed to %s, want %s", l.Exe, exe)
	}
	if got := e.RunKey.(*fakeRunKey).value; got != `"`+exe+`" --autostart` {
		t.Errorf("Run key = %s", got)
	}
	if !strings.HasSuffix(l.Shortcut, `Start Menu\Programs\Gold Forever Companion.lnk`) {
		t.Errorf("shortcut at %s", l.Shortcut)
	}
	if !bytes.Contains([]byte(read(t, l.Shortcut)), utf16le(exe)) {
		t.Error("the shortcut does not point at the installed copy")
	}
}

func TestWindowsComparesPathsWithoutCase(t *testing.T) {
	e, _ := env(t, "windows")
	l := For(e)
	if l.NeedsInstall(strings.ToUpper(l.Exe)) {
		t.Error(`C:\USERS\… and C:\Users\… are the same file on Windows`)
	}
}

func TestWindowsUninstallMovesTheRunningBinaryAside(t *testing.T) {
	e, _ := env(t, "windows")
	l := For(e)
	if err := l.Install(downloaded(t)); err != nil {
		t.Fatal(err)
	}
	var moved string
	l.MoveAside = func(path string) error { moved = path; return os.Remove(path) }
	if err := l.Uninstall(l.Exe, false); err != nil {
		t.Fatal(err)
	}
	if moved != l.Exe {
		t.Errorf("Windows cannot delete a running binary: it must be moved aside, got %q", moved)
	}
	if e.RunKey.Enabled() {
		t.Error("the Run key is still set")
	}
}

func TestMacOSLeavesTheAppWhereItIsAndAddsALaunchAgent(t *testing.T) {
	e, home := env(t, "darwin")
	l := For(e)
	app := "/Applications/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
	if l.NeedsInstall(app) {
		t.Error("on macOS the player drags the app to Applications; it never copies itself")
	}
	if err := l.Autostart.Enable(app); err != nil {
		t.Fatal(err)
	}
	plist := read(t, filepath.Join(home, "Library", "LaunchAgents", "com.evensberget.gold-forever-companion.plist"))
	for _, want := range []string{"<string>" + app + "</string>", "<string>--autostart</string>", "<key>RunAtLoad</key><true/>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("the LaunchAgent lacks %s:\n%s", want, plist)
		}
	}
}

func TestShellLinkFollowsTheFormat(t *testing.T) {
	target := `C:\Users\Bjørn\AppData\Local\Gold Forever\GoldForever-Companion.exe`
	link := shellLink(target, `C:\Users\Bjørn\AppData\Local\Gold Forever`)
	if binary.LittleEndian.Uint32(link) != 0x4C {
		t.Fatal("the header size must be 0x4C")
	}
	if !bytes.Equal(link[4:20], linkCLSID[:]) {
		t.Error("wrong link CLSID")
	}
	if flags := binary.LittleEndian.Uint32(link[20:]); flags&hasLinkInfo == 0 || flags&isUnicode == 0 {
		t.Errorf("flags %#x lack HasLinkInfo or IsUnicode", flags)
	}
	info := link[0x4C:]
	if size := binary.LittleEndian.Uint32(info); int(size) > len(info) {
		t.Fatalf("LinkInfoSize %d is past the end", size)
	}
	if !bytes.Contains(link, utf16le(target)) {
		t.Error("the Unicode target path is missing, so a name like Bjørn would break")
	}
	if !bytes.HasSuffix(link, []byte{0, 0, 0, 0}) {
		t.Error("the link must end with the terminal block")
	}
	// Header, LinkInfo, then the working folder (a count and its UTF-16 units) and the terminal block.
	dir := len(utf16le(`C:\Users\Bjørn\AppData\Local\Gold Forever`))
	if want := 0x4C + int(binary.LittleEndian.Uint32(info)) + 2 + dir + 4; len(link) != want {
		t.Errorf("the link is %d bytes, its parts add up to %d", len(link), want)
	}
}

func utf16le(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

func TestUninstallingTwiceOrWithPiecesMissingIsFine(t *testing.T) {
	e, _ := env(t, "linux")
	l := For(e)
	if err := l.Uninstall(l.Exe, false); err != nil {
		t.Errorf("nothing installed: %v", err)
	}
}

func TestUninstallLeavesSharedFoldersAndRemovesOnlyItsOwn(t *testing.T) {
	e, home := env(t, "linux")
	l := For(e)
	if err := l.Install(downloaded(t)); err != nil {
		t.Fatal(err)
	}
	if err := l.Uninstall(l.Exe, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin")); err != nil {
		t.Error("~/.local/bin is shared with other programs and the PATH: it stays, even empty")
	}
	w, _ := env(t, "windows")
	wl := For(w)
	if err := wl.Install(downloaded(t)); err != nil {
		t.Fatal(err)
	}
	if err := wl.Uninstall(downloaded(t), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wl.OwnDir); !os.IsNotExist(err) {
		t.Error("the Gold Forever folder in LocalAppData is the companion's own: it goes")
	}
}

// Fedora Atomic and Bazzite reach /home through a symlink to /var/home: $HOME says one, the running
// binary's resolved path says the other. Taken for a download, the installed copy started itself again
// and again (found on the owner's machine with v0.1.0-rc.2).
func TestTheInstalledCopyIsRecognisedThroughASymlinkedHome(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	e, _ := env(t, "linux")
	e.Home = link
	l := For(e)
	if err := l.Install(downloaded(t)); err != nil {
		t.Fatal(err)
	}
	resolved := filepath.Join(real, ".local", "bin", "gold-forever-companion")
	if l.NeedsInstall(resolved) {
		t.Error("the installed copy, seen through the symlink's target, must not install itself again")
	}
}
