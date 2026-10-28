package app

import (
	"context"
	"errors"

	"github.com/gold-forever/companion/internal/server"
)

var errNotServed = errors.New("the fake server does not serve this")

// fakeServer answers as the Gold Forever server would, without the network: each route with what its
// func returns. Left nil, an upload waits for an answer that never comes, the account and shared routes
// are missing, and the others fail.
type fakeServer struct {
	upload  func(ctx context.Context, save []byte) (server.Answer, error)
	prices  func(ctx context.Context) ([]byte, string, error)
	account func(token string) (server.Account, error)
	latest  func() (server.Release, error)
}

// connects makes c ask f, with whatever token c asks with.
func (f *fakeServer) connects(c *App) {
	c.connect = func(token string) connection { return fakeConnection{f, token} }
}

type fakeConnection struct {
	*fakeServer
	token string
}

func (f fakeConnection) Upload(ctx context.Context, save []byte) (server.Answer, error) {
	if f.upload == nil {
		<-ctx.Done()
		return server.Answer{}, ctx.Err()
	}
	return f.upload(ctx, save)
}

func (f fakeConnection) Prices(ctx context.Context, game, market, etag string) ([]byte, string, error) {
	if f.prices == nil {
		return nil, "", errNotServed
	}
	return f.prices(ctx)
}

func (f fakeConnection) SharedState(context.Context, string) ([]byte, string, error) {
	return nil, "", server.ErrNoShared
}

func (f fakeConnection) Account(context.Context) (server.Account, error) {
	if f.account == nil {
		return server.Account{}, server.ErrNoAccount
	}
	return f.account(f.token)
}

func (f fakeConnection) Latest(context.Context) (server.Release, error) {
	if f.latest == nil {
		return server.Release{}, errNotServed
	}
	return f.latest()
}
