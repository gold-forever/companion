package app

import (
	"errors"
	"testing"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/server"
)

const otherToken = "gf_9876543210abcdefghijABCDEFGHIJklmnopqrstuvw"

// accounts answers the account route with whatever answer holds for the token asked with, and refuses
// any other token.
func accounts(answer map[string]func() (server.Account, error)) *fakeServer {
	return &fakeServer{account: func(token string) (server.Account, error) {
		if a, ok := answer[token]; ok {
			return a()
		}
		return server.Account{}, server.ErrUnauthorized
	}}
}

func answers(a server.Account, err error) func() (server.Account, error) {
	return func() (server.Account, error) { return a, err }
}

// linkedTo is the fixture's companion linked with token, asking srv.
func linkedTo(t *testing.T, srv *fakeServer, token string) *App {
	t.Helper()
	c, _ := fixture(t)
	if err := c.settings.Update(func(cfg *config.Config) error { cfg.Token = token; return nil }); err != nil {
		t.Fatal(err)
	}
	srv.connects(c)
	return c
}

func (c *App) askAccountWith(t *testing.T, token string) {
	t.Helper()
	_ = c.askAccount(t.Context(), token, c.connect(token))
}

func TestTheCompanionShowsTheAccountsBattleTagUntilAPublicNameIsSet(t *testing.T) {
	account := server.Account{BattleTag: "Goldsmith#1234"}
	srv := accounts(map[string]func() (server.Account, error){
		validToken: func() (server.Account, error) { return account, nil },
	})
	c := linkedTo(t, srv, validToken)

	c.askAccountWith(t, validToken)

	if got := c.State().Account; got != "Goldsmith#1234" {
		t.Errorf("the page shows %q", got)
	}
	if got := c.Account(); got != "Goldsmith#1234" {
		t.Errorf("the tray shows %q", got)
	}
	account.PublicName = "Goldsmith"
	c.askAccountWith(t, validToken)
	if got := c.State().Account; got != "Goldsmith" {
		t.Errorf("with a public name the page shows %q", got)
	}
}

func TestAServerThatCannotSayShowsNoAccountAndChangesNothingElse(t *testing.T) {
	for name, srv := range map[string]*fakeServer{
		"404":     accounts(map[string]func() (server.Account, error){validToken: answers(server.Account{}, server.ErrNoAccount)}),
		"401":     accounts(nil),
		"403":     accounts(map[string]func() (server.Account, error){validToken: answers(server.Account{}, &server.ForbiddenError{Reason: "suspended"})}),
		"offline": accounts(map[string]func() (server.Account, error){validToken: answers(server.Account{}, errors.New("connection refused"))}),
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
	answer := answers(server.Account{PublicName: "Goldsmith"}, nil)
	srv := accounts(map[string]func() (server.Account, error){
		validToken: func() (server.Account, error) { return answer() },
	})
	c := linkedTo(t, srv, validToken)
	c.askAccountWith(t, validToken)

	answer = answers(server.Account{}, errors.New("server unavailable"))
	c.askAccountWith(t, validToken)

	if got := c.State().Account; got != "Goldsmith" {
		t.Errorf("a failed ask changed the account shown to %q", got)
	}
}

func TestAnotherTokenShowsNoAccountUntilItsOwnIsKnown(t *testing.T) {
	srv := accounts(map[string]func() (server.Account, error){
		validToken: answers(server.Account{PublicName: "Goldsmith"}, nil),
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
	srv := accounts(map[string]func() (server.Account, error){
		validToken: answers(server.Account{BattleTag: "Goldsmith#1234"}, nil),
		otherToken: answers(server.Account{BattleTag: "Silversmith#5678"}, nil),
	})
	c := linkedTo(t, srv, validToken)
	c.owned.Go(func() { c.loop(c.life) })

	accountShown(t, c, "Goldsmith#1234")
	if err := c.Link(t.Context(), otherToken); err != nil {
		t.Fatal(err)
	}
	accountShown(t, c, "Silversmith#5678")
}
