package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/server"
)

const otherToken = "gf_9876543210abcdefghijABCDEFGHIJklmnopqrstuvw"

// accountServer answers the account route with whatever answer holds for the token asked with.
func accountServer(t *testing.T, answer map[string]func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/companion/account" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if a, ok := answer[r.Header.Get("Authorization")]; ok {
			a(w)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func answers(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(status); io.WriteString(w, body) }
}

// linkedTo is the fixture's companion linked with token to srv.
func linkedTo(t *testing.T, srv *httptest.Server, token string) *App {
	t.Helper()
	c, _ := fixture(t)
	if err := c.settings.Update(func(cfg *config.Config) error { cfg.Token, cfg.URL = token, srv.URL; return nil }); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *App) askAccountWith(t *testing.T, token string) {
	t.Helper()
	_ = c.askAccount(t.Context(), &server.Client{BaseURL: c.url(), Token: token, Version: c.version, OS: goos})
}

func TestTheCompanionShowsTheAccountsBattleTagUntilAPublicNameIsSet(t *testing.T) {
	body := `{"publicName":null,"battleTag":"Goldsmith#1234"}`
	srv := accountServer(t, map[string]func(http.ResponseWriter){
		"Bearer " + validToken: func(w http.ResponseWriter) { io.WriteString(w, body) },
	})
	c := linkedTo(t, srv, validToken)

	c.askAccountWith(t, validToken)

	if got := c.State().Account; got != "Goldsmith#1234" {
		t.Errorf("the page shows %q", got)
	}
	if got := c.Account(); got != "Goldsmith#1234" {
		t.Errorf("the tray shows %q", got)
	}
	body = `{"publicName":"Goldsmith","battleTag":"Goldsmith#1234"}`
	c.askAccountWith(t, validToken)
	if got := c.State().Account; got != "Goldsmith" {
		t.Errorf("with a public name the page shows %q", got)
	}
}

func TestAServerThatCannotSayShowsNoAccountAndChangesNothingElse(t *testing.T) {
	offline := httptest.NewServer(http.NotFoundHandler())
	offline.Close()
	for name, srv := range map[string]*httptest.Server{
		"404":     accountServer(t, map[string]func(http.ResponseWriter){"Bearer " + validToken: answers(404, `{"reason":"not found"}`)}),
		"401":     accountServer(t, nil),
		"403":     accountServer(t, map[string]func(http.ResponseWriter){"Bearer " + validToken: answers(403, `{"reason":"suspended"}`)}),
		"offline": offline,
	} {
		c := linkedTo(t, srv, validToken)
		c.status.Set("Watching 1 save")

		c.askAccountWith(t, validToken)

		s := c.State()
		if s.Account != "" || s.Status != "Watching 1 save" || !s.Linked || c.settings.Get().Token != validToken {
			t.Errorf("%s: account %q, status %q, linked %v", name, s.Account, s.Status, s.Linked)
		}
	}
}

func TestTheAccountShownIsKeptWhenAskingAgainFails(t *testing.T) {
	answer := answers(200, `{"publicName":"Goldsmith"}`)
	srv := accountServer(t, map[string]func(http.ResponseWriter){
		"Bearer " + validToken: func(w http.ResponseWriter) { answer(w) },
	})
	c := linkedTo(t, srv, validToken)
	c.askAccountWith(t, validToken)

	answer = answers(503, "")
	c.askAccountWith(t, validToken)

	if got := c.State().Account; got != "Goldsmith" {
		t.Errorf("a failed ask changed the account shown to %q", got)
	}
}

func TestAnotherTokenShowsNoAccountUntilItsOwnIsKnown(t *testing.T) {
	srv := accountServer(t, map[string]func(http.ResponseWriter){
		"Bearer " + validToken: answers(200, `{"publicName":"Goldsmith"}`),
	})
	c := linkedTo(t, srv, validToken)
	c.askAccountWith(t, validToken)

	if err := c.Link(t.Context(), otherToken); err != nil {
		t.Fatal(err)
	}

	if got := c.State().Account; got != "" {
		t.Errorf("a new token still shows the old account %q", got)
	}
	if err := c.Unlink(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := c.Account(); got != "" {
		t.Errorf("an unlinked companion shows %q", got)
	}
}

// accountShown waits until the page would show want as the account.
func accountShown(t *testing.T, c *App, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.State().Account == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the account shown never became %q: %q", want, c.State().Account)
}

func TestTheUploadLoopAsksTheAccountAtStartAndAfterLinking(t *testing.T) {
	srv := accountServer(t, map[string]func(http.ResponseWriter){
		"Bearer " + validToken: answers(200, `{"publicName":null,"battleTag":"Goldsmith#1234"}`),
		"Bearer " + otherToken: answers(200, `{"publicName":null,"battleTag":"Silversmith#5678"}`),
	})
	c := linkedTo(t, srv, validToken)
	c.owned.Go(func() { c.loop(c.life) })

	accountShown(t, c, "Goldsmith#1234")
	if err := c.Link(t.Context(), otherToken); err != nil {
		t.Fatal(err)
	}
	accountShown(t, c, "Silversmith#5678")
}
