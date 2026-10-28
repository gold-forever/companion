// Package page is the companion's settings page: a web page served on 127.0.0.1 only, opened from the
// tray with a secret in its link. Every request must come for that exact address, from no other site,
// with the secret (but the ping, which proves the secret without it); posts must be small JSON, which no
// other page can send without the browser asking first, and this server never says yes (design 006,
// "The settings page and tray").
package page

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gold-forever/companion/internal/platform/instance"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

const maxBody = 4 << 10

//go:embed static
var static embed.FS

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

// App is what the page shows and changes. An error the player can fix is a PlayerError; anything else
// is the companion's failure.
type App interface {
	State() State
	Link(ctx context.Context, token string) error
	StartDeviceLink(ctx context.Context) error
	Unlink(ctx context.Context) error
	SetSaveOn(ctx context.Context, path string, on bool) error
	AddFolder(ctx context.Context, path string) error
	SetAutostart(ctx context.Context, on bool) error
	Welcomed(ctx context.Context) error
	History(page int) History
	CopyBytes(id string) ([]byte, bool)
	Restore(ctx context.Context, id string) error
	Uninstall(ctx context.Context, keepData bool) error
	Quit()
	CheckForUpdate(ctx context.Context) error
	Update(ctx context.Context) error
}

// PlayerError is an error the player can fix, which the page shows as theirs (a 400) rather than the
// companion's (a 500).
type PlayerError interface {
	error
	PlayerError()
}

// Listen opens the page on a free port of 127.0.0.1.
func Listen() (net.Listener, int, error) { return ListenAt(0) }

// ListenAt opens the page on port of 127.0.0.1, where a tab may already wait for it; 0 is a free port.
func ListenAt(port int) (net.Listener, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, 0, err
	}
	return l, l.Addr().(*net.TCPAddr).Port, nil
}

// New is the page's handler for the companion listening on port with secret.
func New(app App, secret string, port int) http.Handler {
	h := &handler{app: app, secret: []byte(secret), port: port, host: "127.0.0.1:" + strconv.Itoa(port), mux: http.NewServeMux()}
	h.routes()
	return h
}

type handler struct {
	app    App
	secret []byte
	port   int
	host   string
	mux    *http.ServeMux
}

func cookieName(port int) string { return "companion_" + strconv.Itoa(port) }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cache-Control", "no-store")
	// Another name for 127.0.0.1 (DNS rebinding) or another site's page: refused before anything else.
	if r.Host != h.host {
		http.Error(w, "wrong address", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+h.host {
		http.Error(w, "other sites may not use this page", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == instance.PingPath {
		h.ping(w, r)
		return
	}
	// The tray opens /?s=<secret>: it becomes a cookie, and the address bar loses it.
	if r.URL.Path == "/" && r.URL.Query().Has("s") {
		if !h.matches(r.URL.Query().Get("s")) {
			http.Error(w, "open the settings from the tray icon", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName(h.port), Value: string(h.secret), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !h.authorized(r) {
		http.Error(w, "open the settings from the tray icon", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "JSON only", http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	h.mux.ServeHTTP(w, r)
}

func (h *handler) authorized(r *http.Request) bool {
	if c, err := r.Cookie(cookieName(h.port)); err == nil && h.matches(c.Value) {
		return true
	}
	return h.matches(r.Header.Get(instance.SecretHeader))
}

func (h *handler) matches(s string) bool {
	return subtle.ConstantTimeCompare([]byte(s), h.secret) == 1
}

// The longest nonce a ping may ask about.
const maxNonce = 64

// ping lets another launch tell this copy from whatever else may listen on its port.
func (h *handler) ping(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	if nonce == "" || len(nonce) > maxNonce {
		http.Error(w, "a ping names a nonce", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"version": h.app.State().Version, "proof": instance.Proof(string(h.secret), nonce)})
}

func (h *handler) routes() {
	m := h.mux
	m.Handle("GET /", http.FileServerFS(mustSub(static, "static")))
	m.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.app.State()) })
	m.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			n = 1
		}
		writeJSON(w, h.app.History(n))
	})
	m.HandleFunc("GET /api/copy", func(w http.ResponseWriter, r *http.Request) {
		data, ok := h.app.CopyBytes(r.URL.Query().Get("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	})
	post(m, "/api/link", func(ctx context.Context, b struct{ Token string }) error { return h.app.Link(ctx, b.Token) })
	post(m, "/api/link/device", func(ctx context.Context, _ struct{}) error { return h.app.StartDeviceLink(ctx) })
	post(m, "/api/unlink", func(ctx context.Context, _ struct{}) error { return h.app.Unlink(ctx) })
	post(m, "/api/save", func(ctx context.Context, b struct {
		Path string
		On   bool
	}) error {
		return h.app.SetSaveOn(ctx, b.Path, b.On)
	})
	post(m, "/api/folder", func(ctx context.Context, b struct{ Path string }) error { return h.app.AddFolder(ctx, b.Path) })
	post(m, "/api/autostart", func(ctx context.Context, b struct{ On bool }) error { return h.app.SetAutostart(ctx, b.On) })
	post(m, "/api/welcomed", func(ctx context.Context, _ struct{}) error { return h.app.Welcomed(ctx) })
	post(m, "/api/restore", func(ctx context.Context, b struct {
		ID        string
		WoWClosed bool
	}) error {
		if !b.WoWClosed {
			return errBadRequest("close World of Warcraft first: it overwrites the save when you log out")
		}
		return h.app.Restore(ctx, b.ID)
	})
	post(m, "/api/update/check", func(ctx context.Context, _ struct{}) error { return h.app.CheckForUpdate(ctx) })
	post(m, "/api/update", func(ctx context.Context, _ struct{}) error { return h.app.Update(ctx) })
	post(m, "/api/uninstall", func(ctx context.Context, b struct{ KeepData bool }) error { return h.app.Uninstall(ctx, b.KeepData) })
	post(m, "/api/quit", func(context.Context, struct{}) error { h.app.Quit(); return nil })
}

// errBadRequest is a request the page should not have sent.
type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }
func (errBadRequest) PlayerError()    {}

// post decodes a small JSON body into B and runs act with the request's context; the answer is
// {"ok":true} or the error.
func post[B any](m *http.ServeMux, path string, act func(context.Context, B) error) {
	m.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		var body B
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "not JSON", http.StatusBadRequest)
			return
		}
		if err := act(r.Context(), body); err != nil {
			status := http.StatusInternalServerError
			var bad PlayerError
			if errors.As(err, &bad) {
				status = http.StatusBadRequest
			}
			w.WriteHeader(status)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
