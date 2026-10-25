package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesAnOlderLooserFileWithAnOwnerOnlyOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new")); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode(), err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Errorf("content = %q", data)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("a temporary file was left: %v", entries)
	}
}

func TestWriteMakesTheFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "sent.jsonl")

	if err := Write(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("folder: %v %v", info, err)
	}
}
