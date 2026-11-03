package page

import (
	"time"

	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// State is everything the page shows.
type State struct {
	Version   string      `json:"version"`
	Site      string      `json:"site"`
	Linked    bool        `json:"linked"`
	Sharing   string      `json:"sharing"` // what every upload shares, shown where the player links
	Status    string      `json:"status"`
	Account   string      `json:"account,omitempty"` // whom the token uploads to: the public name, else the BattleTag; none until known
	FirstRun  bool        `json:"firstRun"`
	Installed []string    `json:"installed"` // what installing did, one line each, for the first-run panel
	Autostart bool        `json:"autostart"`
	Saves     []Save      `json:"saves"`
	Folders   []string    `json:"folders"`
	Notice    *Notice     `json:"notice,omitempty"`
	Update    Update      `json:"update"`
	Warning   string      `json:"warning,omitempty"` // a problem with the settings the player must know of
	Linking   *Linking    `json:"linking,omitempty"`
	Prices    []PriceFile `json:"prices"`
	Addon     Addon       `json:"addon"`
}

// Addon is the setting that keeps the Gold Forever addon up to date, and how its last check went.
type Addon struct {
	On      bool   `json:"on"`
	Warning string `json:"warning"`          // what the player confirms before turning it on
	Status  string `json:"status,omitempty"` // what the last check or install did
	Error   string `json:"error,omitempty"`  // why the last check or install failed
}

// PriceFile is one flavour's GoldForever_Data/Data.lua and how the latest fetch of its game went.
type PriceFile struct {
	Flavour string        `json:"flavour"`
	Game    string        `json:"game"`
	Markets []PriceMarket `json:"markets,omitempty"`
	Items   int           `json:"items"`
	Size    int64         `json:"size"`              // bytes; 0 when there is no file
	Fetched *time.Time    `json:"fetched,omitempty"` // none before the first fetch
	Outcome string        `json:"outcome,omitempty"` // ok, not modified, or error
	Error   string        `json:"error,omitempty"`
	Shared  string        `json:"shared,omitempty"` // how many lists and profiles the file carries; none from an older companion or server
}

// PriceMarket is one market in a price file and its item count.
type PriceMarket struct {
	Market string `json:"market"`
	Items  int    `json:"items"`
}

// Linking is a link by code in progress or just ended: waiting (the player approves UserCode at URL),
// linked, denied, expired, or failed with Error.
type Linking struct {
	State    string `json:"state"`
	UserCode string `json:"userCode,omitempty"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Save is one GoldForever.lua the companion found.
type Save struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Game  string `json:"game"`
	On    bool   `json:"on"`
	Note  string `json:"note,omitempty"` // what the companion is doing about it, when not the usual
}

// Notice says a newer release exists, for the banner on top of the page.
type Notice struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// Update is the settings' update section: the newest release the last check found, and whether the
// companion can install it here.
type Update struct {
	Latest     string `json:"latest,omitempty"` // none until a check has answered
	URL        string `json:"url,omitempty"`    // the release's page
	Newer      bool   `json:"newer"`
	CanInstall bool   `json:"canInstall"`      // false where the player downloads it from URL themselves
	Installing bool   `json:"installing"`      // an update is downloading, or has started the new version
	Error      string `json:"error,omitempty"` // why the last check or update failed
}

// History is one page of the upload log, newest first, and the copies still kept.
type History struct {
	Entries []sendlog.Entry `json:"entries"`
	Page    int             `json:"page"`  // from 1
	Pages   int             `json:"pages"` // at least 1, for an empty log too
	Copies  []Copy          `json:"copies"`
}

// Copy is one kept copy of a save, named by an ID the page sends back, never by its path.
type Copy struct {
	ID      string    `json:"id"`
	Save    string    `json:"save"`
	Taken   time.Time `json:"taken"`
	Pending bool      `json:"pending"`
}
