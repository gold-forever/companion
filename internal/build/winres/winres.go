// Command winres writes the Windows build's resources, its version, manifest and icon, as an object
// file the Go linker picks up (a .syso beside the main package). Without them the program carries no
// name, publisher or icon, which is how most malware looks to an antivirus.
//
//	go run ./internal/build/winres -version v0.2.0 -arch amd64 -file GoldForever-Companion.exe -o rsrc_windows_amd64.syso
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/gold-forever/companion/internal/ui/coin"
)

const (
	product   = "Gold Forever Companion"
	company   = "Gold Forever"
	copyright = "Copyright (c) 2026 Magnus Evensberget"
)

// The sizes Windows picks the program's icon from, Explorer's largest view included.
var iconSizes = []int{16, 24, 32, 48, 64, 256}

func main() {
	ver := flag.String("version", "dev", "the version the binary is built with (-X main.version)")
	arch := flag.String("arch", "amd64", "the GOARCH the object is for")
	file := flag.String("file", "", "the name the release publishes the binary under")
	out := flag.String("o", "", "the .syso to write")
	flag.Parse()
	if err := write(*ver, *file, winres.Arch(*arch), *out); err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
}

func write(ver, file string, arch winres.Arch, path string) error {
	rs, err := resources(ver, file)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(rs.WriteObject(f, arch), f.Close())
}

// resources is the program's version, its manifest and its icon. Nothing in them depends on the time
// or the machine, so the same version always makes the same bytes.
func resources(ver, file string) (*winres.ResourceSet, error) {
	rs := &winres.ResourceSet{}
	var images []image.Image
	for _, size := range iconSizes {
		images = append(images, coin.Image(size))
	}
	icon, err := winres.NewIconFromImages(images)
	if err != nil {
		return nil, err
	}
	if err := rs.SetIcon(winres.Name("APP"), icon); err != nil {
		return nil, err
	}
	rs.SetVersionInfo(versionInfo(ver, file))
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "GoldForever.Companion"},
		Description:         product,
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})
	return rs, nil
}

func versionInfo(ver, file string) version.Info {
	vi := version.Info{}
	for key, value := range map[string]string{
		version.ProductName:      product,
		version.FileDescription:  product,
		version.CompanyName:      company,
		version.LegalCopyright:   copyright,
		version.InternalName:     "gold-forever-companion",
		version.OriginalFilename: file,
	} {
		_ = vi.Set(version.LangDefault, key, value)
	}
	// Both take the numbers from the version (v0.2.0-rc.7 is 0.2.0.0) and keep the whole as text.
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	return vi
}
