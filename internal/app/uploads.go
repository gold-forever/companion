package app

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/backup"
	"github.com/gold-forever/companion/internal/core"
	"github.com/gold-forever/companion/internal/find"
	"github.com/gold-forever/companion/internal/prices"
	"github.com/gold-forever/companion/internal/refused"
	"github.com/gold-forever/companion/internal/sendlog"
	"github.com/gold-forever/companion/internal/watch"
)

// Parts is what the uploads are built from: the server, what the data folder keeps, and the saves.
type Parts struct {
	Server  *api.Client
	Backups *backup.Store
	Log     *sendlog.Log
	Refused *refused.List
	Saves   func() []find.Save
	IsOff   func(path string) bool
	// Market is one market whose prices to write instead of the account's ("" for the account's).
	Market  string
	DataDir string
	Out     io.Writer
	Report  *prices.Report // may be nil
}

// Uploads is the upload core and, beside it, the refresher that keeps prices, dings and shared lists
// in the game, fetching again whenever an upload was imported.
type Uploads struct {
	core      *core.Core
	refresher *prices.Refresher
	imported  <-chan struct{}
}

// NewUploads builds the upload core and the price refresher from p.
func NewUploads(p Parts) *Uploads {
	imported, announce := prices.Trigger()
	return &Uploads{
		core: &core.Core{
			Uploader: p.Server,
			Backups:  p.Backups, Log: p.Log, Refused: p.Refused, Watcher: watch.New(PollEvery),
			Saves: p.Saves, IsOff: p.IsOff, Out: p.Out, Imported: announce,
		},
		refresher: p.refresher(p.Server, p.Server, p.Server),
		imported:  imported,
	}
}

// refresher keeps the account's prices, dings state and shared lists and profiles in the game, fetched
// with f, d and s.
func (p Parts) refresher(f prices.Fetcher, d prices.DingsFetcher, s prices.SharedFetcher) *prices.Refresher {
	return &prices.Refresher{
		Fetcher: f, Dings: d, Shared: s, Saves: p.Saves, Market: p.Market,
		State: filepath.Join(p.DataDir, PricesState), Out: p.Out, Report: p.Report,
	}
}

// Run uploads and refreshes until ctx ends or uploading fails. When it returns, nothing it started
// writes into the data folder any more.
func (u *Uploads) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	var refreshing sync.WaitGroup
	refreshing.Go(func() { u.refresher.Run(ctx, RefreshPricesEvery, time.After, u.imported) })
	err := u.core.Run(ctx, PollEvery)
	stop()
	refreshing.Wait()
	return err
}
