// Package install puts the companion where it belongs on the player's computer, starts it when they
// log in, and takes all of it away again on Uninstall. It installs for the one user only and never
// needs admin rights (design 006, "What a release contains").
package install

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gold-forever/companion/internal/config"
)

const (
	appName  = "Gold Forever Companion"
	fileName = "gold-forever-companion"
	agentID  = "com.evensberget.gold-forever-companion"
	runPath  = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// Autostart starts the companion when the player logs in. It is started with --autostart, so it
// comes up quietly in the tray instead of opening the settings page.
type Autostart interface {
	Enable(exe string) error
	Disable() error
	Enabled() bool
}

// Env is what Layout is worked out from: the system, the home folder and its variables, and on
// Windows the registry's Run key.
type Env struct {
	GOOS   string
	Home   string
	Getenv func(string) string
	RunKey Autostart // Windows only: RunKey()
}

// Layout is where the companion lives on one system.
type Layout struct {
	GOOS      string
	Exe       string // where the binary is copied; "" on macOS, where the .app stays where the player put it
	OwnDir    string // the folder installing made, removed on Uninstall; "" where Exe is in a shared one
	Shortcut  string // the launcher in the Start menu or the applications list; "" for none
	Autostart Autostart
	// AutostartAt says where autostart is recorded, for the first-run panel.
	AutostartAt string
	Config      string   // the settings folder: its settings file, with the token, always goes on Uninstall
	Data        []string // backups and the upload log: kept on Uninstall when the player asks
	// MoveAside removes a binary that may be running. Windows cannot delete one, so there it is moved
	// into the temporary folder instead.
	MoveAside func(path string) error

	shortcut func(exe string) []byte
}

// For works out the Layout on e's system.
func For(e Env) Layout {
	switch e.GOOS {
	case "windows":
		local, roaming := e.Getenv("LOCALAPPDATA"), e.Getenv("APPDATA")
		dir := winJoin(local, "Gold Forever")
		return Layout{
			GOOS:        e.GOOS,
			Exe:         winJoin(dir, "GoldForever-Companion.exe"),
			OwnDir:      dir,
			Shortcut:    winJoin(roaming, `Microsoft\Windows\Start Menu\Programs`, appName+".lnk"),
			Autostart:   e.RunKey,
			AutostartAt: `the Run key in your user's registry (HKEY_CURRENT_USER\` + runPath + `)`,
			Config:      winJoin(roaming, fileName),
			Data:        []string{winJoin(local, fileName)},
			MoveAside:   moveToTemp,
			shortcut:    func(exe string) []byte { return shellLink(exe, dir) },
		}
	case "darwin":
		support := filepath.Join(e.Home, "Library", "Application Support", fileName)
		agent := filepath.Join(e.Home, "Library", "LaunchAgents", agentID+".plist")
		return Layout{
			GOOS:        e.GOOS,
			Autostart:   &fileAutostart{agent, launchAgent},
			AutostartAt: agent,
			Config:      support,
			Data:        []string{support},
			MoveAside:   os.Remove,
		}
	default:
		config := xdg(e, "XDG_CONFIG_HOME", ".config")
		share := xdg(e, "XDG_DATA_HOME", filepath.Join(".local", "share"))
		entry := filepath.Join(config, "autostart", fileName+".desktop")
		return Layout{
			GOOS:        e.GOOS,
			Exe:         filepath.Join(e.Home, ".local", "bin", fileName),
			Shortcut:    filepath.Join(share, "applications", fileName+".desktop"),
			Autostart:   &fileAutostart{entry, autostartEntry},
			AutostartAt: entry,
			Config:      filepath.Join(config, fileName),
			Data:        []string{filepath.Join(share, fileName)},
			MoveAside:   os.Remove,
			shortcut:    desktopEntry,
		}
	}
}

// NeedsInstall says whether the binary at running is a download that must install itself first.
func (l Layout) NeedsInstall(running string) bool {
	if l.Exe == "" {
		return false
	}
	if l.GOOS == "windows" {
		return !strings.EqualFold(winClean(l.Exe), winClean(running))
	}
	return resolved(l.Exe) != resolved(running)
}

// resolved is path with its symlinks followed, as os.Executable's caller resolves the running binary:
// on Fedora Atomic $HOME (/home/…) is a symlink to /var/home/…. A path that does not exist yet is
// only cleaned.
func resolved(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return filepath.Clean(path)
}

// Target is the binary that runs once installed.
func (l Layout) Target(running string) string {
	if l.Exe == "" {
		return running
	}
	return l.Exe
}

// Install copies the binary at running into place, adds the launcher, and turns autostart on: the
// first-run page shows that choice ticked, and the player can untick it there.
func (l Layout) Install(running string) error {
	if l.Exe != "" {
		if err := copyBinary(running, l.Exe); err != nil {
			return fmt.Errorf("copying the companion to %s: %w", l.Exe, err)
		}
	}
	if l.Shortcut != "" {
		if err := writeFile(l.Shortcut, l.shortcut(l.Target(running)), 0o644); err != nil {
			return fmt.Errorf("adding the shortcut %s: %w", l.Shortcut, err)
		}
	}
	return l.Autostart.Enable(l.Target(running))
}

// Uninstall turns autostart off and removes the launcher, the binary and the settings, and the
// backups and log unless keepData. It goes on past a failure and reports them all.
func (l Layout) Uninstall(running string, keepData bool) error {
	errs := []error{l.Autostart.Disable()}
	if l.Shortcut != "" {
		errs = append(errs, removeIfThere(l.Shortcut))
	}
	if l.Exe != "" {
		if l.NeedsInstall(running) {
			errs = append(errs, removeIfThere(l.Exe))
		} else if err := l.MoveAside(l.Exe); !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if l.OwnDir != "" {
		_ = os.Remove(l.OwnDir) // only when empty: a binary moved aside beside it stays visible
	}
	errs = append(errs, l.removeSettings(keepData))
	if !keepData {
		for _, dir := range l.Data {
			errs = append(errs, os.RemoveAll(dir))
		}
	}
	return errors.Join(errs...)
}

// removeSettings removes the settings folder, as it holds the token. Where the data shares it (macOS)
// and the player keeps the data, only the settings file goes.
func (l Layout) removeSettings(keepData bool) error {
	if keepData && slices.Contains(l.Data, l.Config) {
		return removeIfThere(filepath.Join(l.Config, config.FileName))
	}
	return os.RemoveAll(l.Config)
}

// copyBinary writes the binary next to its destination first and renames it into place, so a
// half-written copy is never the one that starts.
func copyBinary(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return writeFile(to, data, 0o755)
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// fileAutostart is a file the desktop reads at login: a LaunchAgent, or an XDG autostart entry.
type fileAutostart struct {
	path    string
	content func(exe string) []byte
}

func (a *fileAutostart) Enable(exe string) error { return writeFile(a.path, a.content(exe), 0o644) }
func (a *fileAutostart) Disable() error          { return removeIfThere(a.path) }
func (a *fileAutostart) Enabled() bool {
	_, err := os.Stat(a.path)
	return err == nil
}

func desktopEntry(exe string) []byte {
	return []byte("[Desktop Entry]\nType=Application\nName=" + appName +
		"\nComment=Uploads your Gold Forever saves\nExec=" + quoteExec(exe) + "\nTerminal=false\nCategories=Game;\n")
}

func autostartEntry(exe string) []byte {
	return []byte("[Desktop Entry]\nType=Application\nName=" + appName +
		"\nExec=" + quoteExec(exe) + " --autostart\nTerminal=false\nX-GNOME-Autostart-enabled=true\n")
}

// quoteExec quotes a path for a desktop entry's Exec key. The specification unescapes the value as a
// string first, so each backslash its quoting rule adds is itself written twice; % starts a field code.
func quoteExec(path string) string {
	return `"` + execEscapes.Replace(path) + `"`
}

var execEscapes = strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`)

func launchAgent(exe string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + agentID + `</string>
<key>ProgramArguments</key><array><string>` + html.EscapeString(exe) + `</string><string>--autostart</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`)
}

func xdg(e Env, variable, fallback string) string {
	if dir := e.Getenv(variable); dir != "" {
		return dir
	}
	return filepath.Join(e.Home, fallback)
}

// winJoin joins a Windows path the same way on every system, so the layout can be tested anywhere.
func winJoin(parts ...string) string {
	return strings.Join(parts, `\`)
}

// winClean reads / as \ and drops repeated and trailing separators, so two spellings of one Windows
// path compare equal on any system. 8.3 short names and links still differ: only --installed and
// --autostart keep a copy from handing off to itself.
func winClean(path string) string {
	path = strings.ReplaceAll(path, "/", `\`)
	unc := strings.HasPrefix(path, `\\`)
	for strings.Contains(path, `\\`) {
		path = strings.ReplaceAll(path, `\\`, `\`)
	}
	if unc {
		path = `\` + path
	}
	return strings.TrimRight(path, `\`)
}

// moveToTemp moves a running binary into the temporary folder, which Windows allows where deleting it
// does not; the system clears that folder in time. Across drives the rename fails, and the file is
// left beside the install with a name that says what it is.
func moveToTemp(path string) error {
	aside := filepath.Join(os.TempDir(), fmt.Sprintf("%s-removed-%d.exe", fileName, os.Getpid()))
	if err := os.Rename(path, aside); err == nil {
		return nil
	}
	return os.Rename(path, path+".removed")
}
