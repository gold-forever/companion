package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openSettings(t *testing.T, c Config) *Settings {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAChangeMadeThroughOnePartReachesASubscriber(t *testing.T) {
	s := openSettings(t, Config{Folders: []string{"/wow"}})
	loop := s.Changes() // the part that waits, as the upload loop does
	link := s           // the part that changes them, as linking does

	if err := link.Update(func(c *Config) error { c.Token = "gf_new"; return nil }); err != nil {
		t.Fatal(err)
	}

	select {
	case <-loop:
	default:
		t.Fatal("the subscriber was not told of the change")
	}
	if got := s.Get(); got.Token != "gf_new" || len(got.Folders) != 1 {
		t.Errorf("settings = %+v", got)
	}
	if onDisk, _ := Load(s.Path()); onDisk.Token != "gf_new" || len(onDisk.Folders) != 1 {
		t.Errorf("file = %+v", onDisk)
	}
}

func TestEachChangeIsAnnouncedToTheSubscribersOfThatMoment(t *testing.T) {
	s := openSettings(t, Config{})
	first := s.Changes()
	_ = s.Update(func(c *Config) error { c.Welcomed = true; return nil })
	second := s.Changes()

	select {
	case <-second:
		t.Fatal("a subscriber after the change was told of it")
	default:
	}
	_ = s.Update(func(c *Config) error { c.Market = "forever-EU-normal-horde"; return nil })
	select {
	case <-first:
	default:
		t.Fatal("the first change was not announced")
	}
	select {
	case <-second:
	default:
		t.Fatal("the second change was not announced")
	}
}

func TestAFailedChangeChangesNothingAndAnnouncesNothing(t *testing.T) {
	s := openSettings(t, Config{Off: []string{"/wow/a"}})
	waiting := s.Changes()
	refused := errors.New("no")

	err := s.Update(func(c *Config) error { c.Off[0] = "/wow/b"; c.Token = "gf_x"; return refused })

	if !errors.Is(err, refused) {
		t.Errorf("err = %v", err)
	}
	if got := s.Get(); got.Token != "" || got.Off[0] != "/wow/a" {
		t.Errorf("settings = %+v, want them as they were", got)
	}
	select {
	case <-waiting:
		t.Fatal("a failed change was announced")
	default:
	}
}

func TestTheFileIsReadOnceWhenOpened(t *testing.T) {
	s := openSettings(t, Config{Token: "gf_old"})
	if err := os.WriteFile(s.Path(), []byte(`{"token":"gf_elsewhere"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := s.Get().Token; got != "gf_old" {
		t.Errorf("token = %q: the owner read the file again", got)
	}
}

func TestAnUnreadableFileDoesNotOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"token":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("an unreadable file opened")
	}
}
