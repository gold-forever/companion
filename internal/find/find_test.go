package find

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// install makes <root>/World of Warcraft/<flavour>/WTF/Account/<account>/SavedVariables/GoldForever.lua.
func install(t *testing.T, root, flavour, account string) string {
	t.Helper()
	dir := filepath.Join(root, "World of Warcraft", flavour, "WTF", "Account", account, "SavedVariables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "GoldForever.lua")
	if err := os.WriteFile(path, []byte("GoldForeverDB = {\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSavesFindsEveryAccountInEveryFlavour(t *testing.T) {
	root := t.TempDir()
	a := install(t, root, "_retail_", "ACCOUNT1")
	b := install(t, root, "_retail_", "ACCOUNT2")
	c := install(t, root, "_forever_", "ACCOUNT1")
	// Not saves: a flavour without the addon's file, and a folder that is not a flavour.
	if err := os.MkdirAll(filepath.Join(root, "World of Warcraft", "_ptr_", "WTF", "Account", "X"), 0o755); err != nil {
		t.Fatal(err)
	}
	install(t, root, "Interface", "ACCOUNT1")

	saves := Saves([]string{filepath.Join(root, "World of Warcraft")})

	var paths []string
	for _, s := range saves {
		paths = append(paths, s.Path)
	}
	want := []string{c, a, b}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("saves = %v, want %v", paths, want)
	}
	if saves[0].Flavour != "_forever_" || saves[0].Account != "ACCOUNT1" {
		t.Fatalf("first save = %+v", saves[0])
	}
}

func TestSavesListsAnInstallOnceWhenNamedTwice(t *testing.T) {
	root := t.TempDir()
	install(t, root, "_retail_", "A")
	wow := filepath.Join(root, "World of Warcraft")

	if got := Saves([]string{wow, wow + string(filepath.Separator)}); len(got) != 1 {
		t.Fatalf("got %d saves, want 1", len(got))
	}
}

func TestInstallsKeepsOnlyFoldersThatExist(t *testing.T) {
	root := t.TempDir()
	install(t, root, "_retail_", "A")
	wow := filepath.Join(root, "World of Warcraft")

	got := Installs([]string{filepath.Join(root, "missing"), wow, wow})

	if !reflect.DeepEqual(got, []string{wow}) {
		t.Fatalf("installs = %v", got)
	}
}

func TestCandidatesOnLinuxGlobTheKnownPrefixes(t *testing.T) {
	home := t.TempDir()
	lutris := filepath.Join(home, "Games", "battlenet", "drive_c", "Program Files (x86)", "World of Warcraft")
	steam := filepath.Join(home, ".local", "share", "Steam", "steamapps", "compatdata", "123", "pfx", "drive_c", "Program Files (x86)", "World of Warcraft")
	for _, dir := range []string{lutris, steam} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := Installs(Candidates("linux", home, func(string) string { return "" }, func() string { return "" }))

	if !reflect.DeepEqual(got, []string{lutris, steam}) {
		t.Fatalf("installs = %v", got)
	}
}

func TestCandidatesOnWindowsStartWithTheRegistryInstall(t *testing.T) {
	getenv := func(k string) string {
		if k == "ProgramFiles(x86)" {
			return `C:\Program Files (x86)`
		}
		return ""
	}
	// Battle.net records a flavour folder; the install is its parent.
	registry := func() string { return `D:\Games\World of Warcraft\_retail_\` }

	got := Candidates("windows", `C:\Users\p`, getenv, registry)

	want := []string{`D:\Games\World of Warcraft`, `C:\Program Files (x86)\World of Warcraft`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestCandidatesOnMacAreTheApplicationsFolder(t *testing.T) {
	got := Candidates("darwin", "/Users/p", func(string) string { return "" }, func() string { return "" })

	if !reflect.DeepEqual(got, []string{"/Applications/World of Warcraft"}) {
		t.Fatalf("candidates = %v", got)
	}
}

func TestGameReadsTheInterfaceNumber(t *testing.T) {
	cases := map[string]string{
		`["interface"] = 16001,`:  "forever",
		`["interface"] = 16999,`:  "forever",
		`["interface"] = 120001,`: "retail",
		`["interface"] = 110000,`: "retail",
		`["interface"] = 11507,`:  "unknown",
		`["build"] = "1.60.1",`:   "unknown",
	}
	for line, want := range cases {
		save := "GoldForeverDB = {\n[\"client\"] = {\n" + line + "\n},\n}\n"
		if got := Game([]byte(save)); got != want {
			t.Errorf("Game(%s) = %q, want %q", line, got, want)
		}
	}
}

func TestAtReadsTheSaveFromItsPath(t *testing.T) {
	path := filepath.Join("/games", "World of Warcraft", "_forever_", "WTF", "Account", "ACC", "SavedVariables", "GoldForever.lua")

	got := At(path)

	if got.Install != filepath.Join("/games", "World of Warcraft") || got.Flavour != "_forever_" || got.Account != "ACC" {
		t.Fatalf("At = %+v", got)
	}
	if got.Label() != "World of Warcraft › _forever_ › ACC" {
		t.Fatalf("Label = %q", got.Label())
	}
}
