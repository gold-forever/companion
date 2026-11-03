// Package page is the companion's settings page: a web page served on 127.0.0.1 only, opened from the
// tray with a secret in its link. Every request must come for that exact address, from no other site,
// with the secret (but the ping, which proves the secret without it); posts must be small JSON, which no
// other page can send without the browser asking first, and this server never says yes.
package page

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/gold-forever/companion/internal/platform/instance"
)

const maxBody = 4 << 10

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
	// SetAddonUpdates turns keeping the addon up to date on (only once the player confirmed the
	// warning) or off.
	SetAddonUpdates(ctx context.Context, on, confirmed bool) error
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
	post(m, "/api/addon", func(ctx context.Context, b struct{ On, Confirmed bool }) error {
		return h.app.SetAddonUpdates(ctx, b.On, b.Confirmed)
	})
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
