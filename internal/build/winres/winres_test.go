package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func TestTheProgramSaysWhatItIsAndWhichVersion(t *testing.T) {
	rs, err := resources("v0.2.0-rc.7", "GoldForever-Companion.exe")
	if err != nil {
		t.Fatal(err)
	}
	vi, err := version.FromBytes(rs.Get(winres.RT_VERSION, winres.ID(1), version.LangDefault))
	if err != nil {
		t.Fatalf("no version resource: %v", err)
	}
	if vi.FileVersion != [4]uint16{0, 2, 0, 0} || vi.ProductVersion != [4]uint16{0, 2, 0, 0} {
		t.Errorf("versions %v %v, want 0.2.0.0", vi.FileVersion, vi.ProductVersion)
	}
	strs := vi.Table().GetMainTranslation()
	for key, want := range map[string]string{
		version.ProductName:      "Gold Forever Companion",
		version.CompanyName:      "Gold Forever",
		version.FileVersion:      "v0.2.0-rc.7",
		version.ProductVersion:   "v0.2.0-rc.7",
		version.OriginalFilename: "GoldForever-Companion.exe",
	} {
		if strs[key] != want {
			t.Errorf("%s = %q, want %q", key, strs[key], want)
		}
	}
}

func TestTheProgramAsksForNoAdminRightsAndCarriesTheCoin(t *testing.T) {
	rs, err := resources("v0.2.0-rc.7", "GoldForever-Companion.exe")
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(rs.Get(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault))
	if !strings.Contains(manifest, `level="asInvoker"`) {
		t.Errorf("the manifest must run as the user who starts it:\n%s", manifest)
	}
	if rs.Get(winres.RT_GROUP_ICON, winres.Name("APP"), winres.LCIDNeutral) == nil {
		t.Error("no program icon")
	}
}

// The release is rebuilt byte for byte (VERIFY.md), so the resources must be too.
func TestTheSameVersionMakesTheSameObject(t *testing.T) {
	var a, b bytes.Buffer
	for _, w := range []*bytes.Buffer{&a, &b} {
		rs, err := resources("v0.2.0-rc.7", "GoldForever-Companion.exe")
		if err != nil {
			t.Fatal(err)
		}
		if err := rs.WriteObject(w, winres.ArchAMD64); err != nil {
			t.Fatal(err)
		}
	}
	if a.Len() == 0 || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two objects for one version differ")
	}
}
