package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAConfigSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gold-forever-companion", "config.json")
	want := Config{Token: "gf_x", URL: "https://example.test", Off: []string{"/a"}, Folders: []string{"/wow"}}

	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600: it holds the token", info.Mode().Perm())
	}
}

func TestSavingTightensAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Save(path, Config{Token: "gf_x"}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestNoFileIsAnEmptyConfig(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || !reflect.DeepEqual(got, Config{}) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

func TestIsOff(t *testing.T) {
	c := Config{Off: []string{"/a"}}
	if !c.IsOff("/a") || c.IsOff("/b") {
		t.Fatal("IsOff does not follow Off")
	}
}

func TestDataDirFollowsEachSystemsConvention(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\p\AppData\Local`}
	getenv := func(k string) string { return env[k] }
	cases := map[string]string{
		"windows": `C:\Users\p\AppData\Local` + string(filepath.Separator) + "gold-forever-companion",
		"darwin":  filepath.Join("/home/p", "Library", "Application Support", "gold-forever-companion"),
		"linux":   filepath.Join("/home/p", ".local", "share", "gold-forever-companion"),
	}
	for goos, want := range cases {
		if got := DataDir(goos, "/home/p", getenv); got != want {
			t.Errorf("DataDir(%s) = %q, want %q", goos, got, want)
		}
	}
	env["XDG_DATA_HOME"] = "/data"
	if got := DataDir("linux", "/home/p", getenv); got != filepath.Join("/data", "gold-forever-companion") {
		t.Errorf("DataDir ignores XDG_DATA_HOME: %q", got)
	}
}
