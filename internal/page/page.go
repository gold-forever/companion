// Package page is the companion's settings page: a web page served on 127.0.0.1 only, opened from the
// tray with a secret in its link. Every request must come for that exact address, from no other site,
// with the secret; posts must be small JSON, which no other page can send without the browser asking
// first, and this server never says yes (design 006, "The settings page and tray").
package page

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gold-forever/companion/internal/instance"
	"github.com/gold-forever/companion/internal/sendlog"
)

const maxBody = 4 << 10

//go:embed static
var static embed.FS

// State is everything the page shows.
type State struct {
	Version   string   `json:"version"`
	Site      string   `json:"site"`
	Linked    bool     `json:"linked"`
	Sharing   string   `json:"sharing"` // what every upload shares, shown where the player links
	Status    string   `json:"status"`
	FirstRun  bool     `json:"firstRun"`
	Installed []string `json:"installed"` // what installing did, one line each, for the first-run panel
	Autostart bool     `json:"autostart"`
	Saves     []Save   `json:"saves"`
	Folders   []string `json:"folders"`
	Notice    *Notice  `json:"notice,omitempty"`
	Linking   *Linking `json:"linking,omitempty"`
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
}

// Notice says a newer release exists; the player downloads it themselves.
type Notice struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// History is the upload log and the copies still kept.
type History struct {
	Entries []sendlog.Entry `json:"entries"`
	Copies  []Copy          `json:"copies"`
}

// Copy is one kept copy of a save, named by an ID the page sends back, never by its path.
type Copy struct {
	ID      string    `json:"id"`
	Save    string    `json:"save"`
	Taken   time.Time `json:"taken"`
	Pending bool      `json:"pending"`
}

// App is what the page shows and changes.
type App interface {
	State() State
	Link(token string) error
	StartDeviceLink() error
	Unlink() error
	SetSaveOn(path string, on bool) error
	AddFolder(path string) error
	SetAutostart(on bool) error
	Welcomed() error
	History() History
	CopyBytes(id string) ([]byte, bool)
	Restore(id string) error
	Uninstall(keepData bool) error
	Quit()
}

// Listen opens the page on a free port of 127.0.0.1.
func Listen() (net.Listener, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
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

func (h *handler) routes() {
	m := h.mux
	m.Handle("GET /", http.FileServerFS(mustSub(static, "static")))
	m.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": h.app.State().Version})
	})
	m.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.app.State()) })
	m.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.app.History()) })
	m.HandleFunc("GET /api/copy", func(w http.ResponseWriter, r *http.Request) {
		data, ok := h.app.CopyBytes(r.URL.Query().Get("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	})
	post(m, "/api/link", func(b struct{ Token string }) error { return h.app.Link(b.Token) })
	post(m, "/api/link/device", func(struct{}) error { return h.app.StartDeviceLink() })
	post(m, "/api/unlink", func(struct{}) error { return h.app.Unlink() })
	post(m, "/api/save", func(b struct {
		Path string
		On   bool
	}) error {
		return h.app.SetSaveOn(b.Path, b.On)
	})
	post(m, "/api/folder", func(b struct{ Path string }) error { return h.app.AddFolder(b.Path) })
	post(m, "/api/autostart", func(b struct{ On bool }) error { return h.app.SetAutostart(b.On) })
	post(m, "/api/welcomed", func(struct{}) error { return h.app.Welcomed() })
	post(m, "/api/restore", func(b struct {
		ID        string
		WoWClosed bool
	}) error {
		if !b.WoWClosed {
			return errBadRequest("close World of Warcraft first: it overwrites the save when you log out")
		}
		return h.app.Restore(b.ID)
	})
	post(m, "/api/uninstall", func(b struct{ KeepData bool }) error { return h.app.Uninstall(b.KeepData) })
	post(m, "/api/quit", func(struct{}) error { go h.app.Quit(); return nil })
}

// errBadRequest is a request the page should not have sent; anything else the app returns is a 500.
type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

// post decodes a small JSON body into B and runs act; the answer is {"ok":true} or the error.
func post[B any](m *http.ServeMux, path string, act func(B) error) {
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
		if err := act(body); err != nil {
			status := http.StatusInternalServerError
			var bad errBadRequest
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

// BadRequest marks an error as the player's to fix, so the page shows it as such.
func BadRequest(format string, args ...any) error { return errBadRequest(fmt.Sprintf(format, args...)) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
