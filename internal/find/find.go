// Package find locates World of Warcraft installs and the GoldForever.lua each WoW account folder in
// them holds. It reads folder names only, never file contents, except Game, which reads the save it
// is given.
package find

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Save is one GoldForever.lua: the game writes one per WoW account folder in each flavour.
type Save struct {
	Path    string
	Install string // the World of Warcraft folder
	Flavour string // _retail_, _forever_, …
	Account string // the WTF account folder's name: shown locally, never sent
}

// Label names a save for the player: install › flavour › account.
func (s Save) Label() string {
	return filepath.Base(s.Install) + " › " + s.Flavour + " › " + s.Account
}

// Where Wine, Lutris, Bottles and Steam put a Windows install on Linux, as globs under the home folder.
var linuxPrefixes = []string{
	"Games/*/drive_c",
	".wine/drive_c",
	".local/share/bottles/bottles/*/drive_c",
	".var/app/com.usebottles.bottles/data/bottles/bottles/*/drive_c",
	".local/share/Steam/steamapps/compatdata/*/pfx/drive_c",
	".steam/steam/steamapps/compatdata/*/pfx/drive_c",
}

var programFolders = []string{"Program Files (x86)", "Program Files"}

const wowFolder = "World of Warcraft"

// Candidates lists the folders WoW is usually installed in on goos, most likely first. registry
// returns Battle.net's recorded InstallPath on Windows, or "". Folders that don't exist are left in:
// Installs drops them.
func Candidates(goos, home string, getenv func(string) string, registry func() string) []string {
	switch goos {
	case "windows":
		var out []string
		if recorded := strings.TrimRight(registry(), `\`); recorded != "" {
			// Battle.net records a flavour folder (…\World of Warcraft\_retail_); the install is its parent.
			if i := strings.LastIndex(recorded, `\`); i > 0 && isFlavour(recorded[i+1:]) {
				recorded = recorded[:i]
			}
			out = append(out, recorded)
		}
		if programs := getenv("ProgramFiles(x86)"); programs != "" {
			out = append(out, programs+`\`+wowFolder)
		}
		return out
	case "darwin":
		return []string{"/Applications/" + wowFolder}
	default:
		var out []string
		for _, prefix := range linuxPrefixes {
			for _, drive := range globUnder(home, prefix) {
				for _, programs := range programFolders {
					out = append(out, filepath.Join(drive, programs, wowFolder))
				}
			}
		}
		return out
	}
}

// Installs keeps the folders that exist, each once, in the order given.
func Installs(candidates []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range candidates {
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			out = append(out, dir)
		}
	}
	return out
}

// Saves lists every GoldForever.lua under the installs, sorted by path.
func Saves(installs []string) []Save {
	var out []Save
	for _, install := range Installs(installs) {
		for _, path := range globUnder(install, "_*_/WTF/Account/*/SavedVariables/GoldForever.lua") {
			out = append(out, At(path))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// globUnder matches pattern, slash-separated, under dir. Only pattern is a pattern: a folder named
// "Games [SSD]" is matched as it is.
func globUnder(dir, pattern string) []string {
	matches, _ := fs.Glob(os.DirFS(dir), pattern)
	for i, m := range matches {
		matches[i] = filepath.Join(dir, filepath.FromSlash(m))
	}
	return matches
}

// At reads which install, flavour and account a save belongs to from its path:
// <install>/<flavour>/WTF/Account/<account>/SavedVariables/GoldForever.lua.
func At(path string) Save {
	account := filepath.Dir(filepath.Dir(path))
	flavour := filepath.Dir(filepath.Dir(filepath.Dir(account)))
	return Save{Path: path, Install: filepath.Dir(flavour), Flavour: filepath.Base(flavour), Account: filepath.Base(account)}
}

func isFlavour(name string) bool {
	return len(name) > 2 && strings.HasPrefix(name, "_") && strings.HasSuffix(name, "_")
}

var interfaceLine = regexp.MustCompile(`\["interface"\]\s*=\s*(\d+)`)

// Game reads which game wrote a save from its interface number, as the server does: forever, retail
// or unknown. It only labels the save locally; the server decides for itself.
func Game(save []byte) string {
	m := interfaceLine.FindSubmatch(save)
	if m == nil {
		return "unknown"
	}
	n, _ := strconv.Atoi(string(m[1]))
	switch {
	case n >= 16000 && n <= 16999:
		return "forever"
	case n >= 110000:
		return "retail"
	default:
		return "unknown"
	}
}
