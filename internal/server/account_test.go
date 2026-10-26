package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestTheAccountIsAskedForWithTheToken(t *testing.T) {
	c, seen, _ := server(t, http.StatusOK, `{"publicName":null,"battleTag":"Goldsmith#1234"}`, nil)

	a, err := c.Account(context.Background())

	if err != nil || a.PublicName != "" || a.BattleTag != "Goldsmith#1234" {
		t.Fatalf("Account = %+v, %v", a, err)
	}
	if seen.Method != "GET" || seen.URL.Path != "/api/v1/companion/account" {
		t.Errorf("asked %s %s", seen.Method, seen.URL.Path)
	}
	if seen.Header.Get("Authorization") != "Bearer gf_secret" || seen.Header.Get("User-Agent") != "gold-forever-companion/0.0.1 (linux)" {
		t.Errorf("headers = %v", seen.Header)
	}
}

func TestAServerWithoutTheAccountRouteSaysSo(t *testing.T) {
	c, _, _ := server(t, http.StatusNotFound, `{"reason":"not found"}`, nil)
	if _, err := c.Account(context.Background()); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("err = %v, want ErrNoAccount", err)
	}
}

func TestTheAccountIsRefusedAsPricesAre(t *testing.T) {
	c, _, _ := server(t, http.StatusUnauthorized, `{"reason":"unknown token"}`, nil)
	if _, err := c.Account(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: err = %v", err)
	}
	c, _, _ = server(t, http.StatusForbidden, `{"reason":"suspended"}`, nil)
	var forbidden *ForbiddenError
	if _, err := c.Account(context.Background()); !errors.As(err, &forbidden) {
		t.Errorf("403: err = %v", err)
	}
	c, _, _ = server(t, http.StatusOK, `<html>captive portal</html>`, nil)
	if _, err := c.Account(context.Background()); err == nil {
		t.Error("an answer that is not JSON is not an account")
	}
}
