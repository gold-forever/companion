//go:build !windows

package instance

import (
	"errors"
	"testing"
)

// Two launches at once (the login autostart and a click on the shortcut) must not both run.
func TestOnlyOneCopyHoldsTheClaimUntilItLetsGo(t *testing.T) {
	dir := t.TempDir()
	release, err := Claim(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Claim(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second claim: err = %v, want ErrRunning", err)
	}
	release()
	again, err := Claim(dir)
	if err != nil {
		t.Fatalf("the claim was not let go: %v", err)
	}
	again()
}
