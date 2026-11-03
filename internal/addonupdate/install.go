package addonupdate

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gold-forever/companion/internal/addondata"
)

// The addon's own folder, and the folders an install keeps beside the addons in the AddOns folder. The
// game loads only folders holding a .toc of their own name, so it never loads these.
const (
	addonFolder = "GoldForever"
	// stagingDir holds the release unpacked, and while it holds manifest, an install that has not
	// finished: the next start puts the old folders back.
	stagingDir = ".GoldForever-update"
	manifest   = "folders.txt"
	// backupDir holds the folders an install replaced, until the companion next starts.
	backupDir   = ".GoldForever-backup"
	maxUnpacked = 128 << 20
)

// rename moves a folder; tests make it fail.
var rename = os.Rename

// Installed is the version the installed addon's .toc names, or "" when none is installed or it names
// none.
func Installed(addOns string) string {
	data, err := os.ReadFile(filepath.Join(addOns, addonFolder, addonFolder+".toc"))
	if err != nil {
		return ""
	}
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "## Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Install puts a release's addon folders into addOns: unpacked beside them first, then each folder
// swapped in, the old one moved to the backup. A failure puts every old folder back. The companion's
// price folder is never replaced, and nothing else in addOns is touched.
func Install(addOns string, archive []byte) error {
	if _, err := Recover(addOns); err != nil {
		return err
	}
	staging := filepath.Join(addOns, stagingDir)
	folders, err := unpack(archive, staging)
	if err == nil {
		err = os.WriteFile(filepath.Join(staging, manifest), []byte(strings.Join(folders, "\n")+"\n"), 0o644)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(staging))
	}
	if err := swap(addOns, folders); err != nil {
		return errors.Join(err, rollback(addOns))
	}
	_ = os.RemoveAll(staging)
	return nil
}

// swap moves each old folder into the backup and the new one into its place; removing the manifest
// then finishes the install.
func swap(addOns string, folders []string) error {
	staging, backup := filepath.Join(addOns, stagingDir), filepath.Join(addOns, backupDir)
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return err
	}
	for _, f := range folders {
		installed := filepath.Join(addOns, f)
		if exists(installed) {
			if err := rename(installed, filepath.Join(backup, f)); err != nil {
				return err
			}
		}
		if err := rename(filepath.Join(staging, f), installed); err != nil {
			return err
		}
	}
	return os.Remove(filepath.Join(staging, manifest))
}

// Recover finishes what an install left at start: one that did not finish has its old folders put back
// (restored is true); one that did loses its backup, now that the companion has started after it.
func Recover(addOns string) (restored bool, err error) {
	staging := filepath.Join(addOns, stagingDir)
	if exists(filepath.Join(staging, manifest)) {
		return true, rollback(addOns)
	}
	err = os.RemoveAll(staging)
	if Installed(addOns) != "" {
		err = errors.Join(err, os.RemoveAll(filepath.Join(addOns, backupDir)))
	}
	return false, err
}

// rollback puts back each folder the manifest names: the old one from the backup, or none where the
// release added the folder.
func rollback(addOns string) error {
	staging, backup := filepath.Join(addOns, stagingDir), filepath.Join(addOns, backupDir)
	list, err := os.ReadFile(filepath.Join(staging, manifest))
	if err != nil {
		return err
	}
	var errs []error
	for _, f := range strings.Fields(string(list)) {
		if !isAddonFolder(f) {
			continue
		}
		installed, old := filepath.Join(addOns, f), filepath.Join(backup, f)
		switch {
		case exists(old):
			errs = append(errs, os.RemoveAll(installed), rename(old, installed))
		case !exists(filepath.Join(staging, f)):
			errs = append(errs, os.RemoveAll(installed))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err // the manifest stays, so the next start tries again
	}
	return errors.Join(os.RemoveAll(staging), os.RemoveAll(backup))
}

// unpack writes the release's addon folders under dir and lists them, sorted. A zip with anything but
// Gold Forever's addon folders in it is refused; the companion's price folder in it is left out.
func unpack(archive []byte, dir string) ([]string, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("the addon download is not a zip: %w", err)
	}
	var folders []string
	budget := int64(maxUnpacked)
	for _, f := range r.File {
		top, rest, _ := strings.Cut(f.Name, "/")
		if !filepath.IsLocal(f.Name) || strings.Contains(f.Name, `\`) || rest == "" && !f.FileInfo().IsDir() || !isAddonFolder(top) {
			return nil, fmt.Errorf("the addon release holds %q, which is not one of its addon folders", f.Name)
		}
		if top == addondata.AddonName {
			continue
		}
		if !slices.Contains(folders, top) {
			folders = append(folders, top)
		}
		if budget, err = unpackFile(f, filepath.Join(dir, filepath.FromSlash(f.Name)), budget); err != nil {
			return nil, err
		}
	}
	if !slices.Contains(folders, addonFolder) {
		return nil, errors.New("the addon release holds no " + addonFolder + " folder")
	}
	slices.Sort(folders)
	return folders, nil
}

func unpackFile(f *zip.File, path string, budget int64) (int64, error) {
	if f.FileInfo().IsDir() {
		return budget, os.MkdirAll(path, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return budget, err
	}
	in, err := f.Open()
	if err != nil {
		return budget, err
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return budget, err
	}
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	if err == nil && n > budget {
		err = errors.New("the addon release unpacks larger than any addon")
	}
	return budget - n, errors.Join(err, out.Close())
}

// isAddonFolder says whether name is Gold Forever's addon folder or one of its own (GoldForever_…).
func isAddonFolder(name string) bool {
	return name == addonFolder || strings.HasPrefix(name, addonFolder+"_")
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
