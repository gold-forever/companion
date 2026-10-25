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

type vars map[string]string

func (v vars) get(k string) string { return v[k] }

type fakeRunKey struct{ value string }

func (k *fakeRunKey) Enable(exe string) error  { k.value = `"` + exe + `" --autostart`; return nil }
func (k *fakeRunKey) Disable() error           { k.value = ""; return nil }
func (k *fakeRunKey) Enabled() bool            { return k.value != "" }
func (k *fakeRunKey) StartsAt(exe string) bool { return k.value == `"`+exe+`" --autostart` }

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

// The Desktop Entry spec unescapes a value as a string before it applies the quoting rules, so a
// reserved character inside quotes takes two backslashes, a backslash four, and % is written %%.
func TestExecQuotesWhatTheDesktopSpecReserves(t *testing.T) {
	cases := map[string]string{
		`/home/a "b"/x`: `"/home/a \\"b\\"/x"`,
		`/home/$x`:      `"/home/\\$x"`,
		"/home/`x`":     "\"/home/\\\\`x\\\\`\"",
		`/home/a\b`:     `"/home/a\\\\b"`,
		`/home/100%`:    `"/home/100%%"`,
	}
	for path, want := range cases {
		if got := quoteExec(path); got != want {
			t.Errorf("quoteExec(%s) = %s, want %s", path, got, want)
		}
	}
}

// On macOS the settings and the data share one folder, so keeping the data must not remove it.
func TestUninstallRemovesEverythingAndKeepsBackupsOnlyWhenAsked(t *testing.T) {
	for _, goos := range []string{"linux", "windows", "darwin"} {
		for _, keep := range []bool{true, false} {
			e, _ := env(t, goos)
			l := For(e)
			l.MoveAside = os.Remove
			download := downloaded(t)
			if err := l.Install(download); err != nil {
				t.Fatal(err)
			}
			running := l.Target(download)
			settings := filepath.Join(l.Config, "config.json")
			setAside := filepath.Join(l.Config, "config.json.bad")
			files := map[string]string{settings: `{"token":"gf_x"}`, setAside: `{"token":"gf_y"`, filepath.Join(l.Data[0], "sent.jsonl"): "{}\n"}
			for path, text := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := l.Uninstall(running, keep); err != nil {
				t.Fatal(err)
			}
			for _, gone := range []string{l.Exe, l.Shortcut, settings, setAside} {
				if _, err := os.Stat(gone); gone != "" && !os.IsNotExist(err) {
					t.Errorf("%s keep=%v: %s is still there", goos, keep, gone)
				}
			}
			if l.Autostart.Enabled() {
				t.Errorf("%s keep=%v: autostart is still on", goos, keep)
			}
			_, err := os.Stat(filepath.Join(l.Data[0], "sent.jsonl"))
			if kept := err == nil; kept != keep {
				t.Errorf("%s keep=%v: the upload log kept=%v", goos, keep, kept)
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

func TestWindowsComparesTwoSpellingsOfOnePath(t *testing.T) {
	e := Env{GOOS: "windows", Getenv: vars{"LOCALAPPDATA": `C:\Users\A\AppData\Local\`}.get}
	l := For(e)
	for _, running := range []string{
		`C:\Users\A\AppData\Local\Gold Forever\GoldForever-Companion.exe`,
		`C:/Users/A/AppData/Local/Gold Forever/GoldForever-Companion.exe`,
	} {
		if l.NeedsInstall(running) {
			t.Errorf("%s is the installed copy %s", running, l.Exe)
		}
	}
	if !l.NeedsInstall(`C:\Users\A\Downloads\GoldForever-Companion.exe`) {
		t.Error("a download must still install itself")
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
	w, whome := env(t, "windows")
	wl := For(w)
	if own := winJoin(filepath.Join(whome, "AppData", "Local"), "Gold Forever"); wl.OwnDir != own {
		t.Fatalf("OwnDir = %q, want %q", wl.OwnDir, own)
	}
	if err := wl.Install(downloaded(t)); err != nil {
		t.Fatal(err)
	}
	// On Windows installing makes the folder; here the \-joined path is one name, so the test does.
	if err := os.MkdirAll(wl.OwnDir, 0o755); err != nil {
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

const (
	inApplications = "/Applications/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
	inDownloads    = "/Users/a/Downloads/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
	onDiskImage    = "/Volumes/Gold Forever Companion/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
	translocated   = "/private/var/folders/x/T/AppTranslocation/0A1B/d/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion"
)

func TestMacOSTurnsAutostartOnAtTheFirstRunOnly(t *testing.T) {
	e, _ := env(t, "darwin")
	l := For(e)
	if err := l.FollowApp(inApplications, false); err != nil || l.Autostart.Enabled() {
		t.Fatalf("a player who turned it off keeps it off: %v", err)
	}
	if err := l.FollowApp(inApplications, true); err != nil || !l.Autostart.Enabled() || !l.Autostart.StartsAt(inApplications) {
		t.Fatalf("the first run turns it on for the app: %v", err)
	}
}

// Opened once from Downloads and then dragged to Applications, the app must start from where it is now.
func TestMacOSAutostartFollowsTheAppWhenItMoves(t *testing.T) {
	e, _ := env(t, "darwin")
	l := For(e)
	if err := l.Autostart.Enable(inDownloads); err != nil {
		t.Fatal(err)
	}

	if err := l.FollowApp(inApplications, false); err != nil {
		t.Fatal(err)
	}

	if !l.Autostart.StartsAt(inApplications) {
		t.Error("autostart still starts the app where it was")
	}
}

func TestMacOSRecordsNoAutostartForAnAppThatIsGoneAtTheNextLogin(t *testing.T) {
	for _, running := range []string{onDiskImage, translocated} {
		e, _ := env(t, "darwin")
		l := For(e)
		if !l.Transient(running) {
			t.Errorf("%s is not where the app stays", running)
		}
		if err := l.FollowApp(running, true); err != nil || l.Autostart.Enabled() {
			t.Errorf("%s: autostart recorded for a path gone at the next login (%v)", running, err)
		}
	}
	if l := For(Env{GOOS: "darwin"}); l.Transient(inApplications) {
		t.Error("Applications is where the app stays")
	}
	if l := For(Env{GOOS: "linux", Getenv: vars{}.get}); l.Transient("/Volumes/x/gold-forever-companion") {
		t.Error("only macOS mounts disk images under /Volumes")
	}
}

func TestInstalledSystemsLeaveAutostartToInstalling(t *testing.T) {
	e, _ := env(t, "linux")
	l := For(e)
	if err := l.FollowApp(l.Exe, true); err != nil || l.Autostart.Enabled() {
		t.Errorf("Install turns autostart on where there is an install step: %v", err)
	}
}
