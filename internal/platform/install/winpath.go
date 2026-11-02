package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
