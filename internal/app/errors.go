package app

import "fmt"

// The use cases' refusals: each is the player's to fix (page.PlayerError), and says how.
var (
	ErrNotAToken          error = refusal("that is not a token: a token starts with gf_ and is 46 letters and digits, from your tokens page")
	ErrUnknownSave        error = refusal("the companion has not found that save")
	ErrCopyGone           error = refusal("that copy is no longer kept")
	ErrMoveToApplications error = refusal(moveToApplications)
)

type refusal string

func (r refusal) Error() string { return string(r) }
func (refusal) PlayerError()    {}

// NoSaveError refuses a folder with no GoldForever.lua under it.
type NoSaveError struct{ Folder string }

func (e *NoSaveError) Error() string {
	return fmt.Sprintf("no GoldForever.lua under %s yet: choose the World of Warcraft folder, the one with _retail_ or _forever_ in it, after playing once with the addon", e.Folder)
}

func (*NoSaveError) PlayerError() {}
