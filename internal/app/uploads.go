package app

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/gold-forever/companion/internal/addondata"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/saves/watch"
	"github.com/gold-forever/companion/internal/upload/core"
)

// Server is what the uploads ask of the Gold Forever server: to take a save, and the prices, shared
// lists and the account's characters they keep in the game.
type Server interface {
	core.Uploader
	addondata.Fetcher
	addondata.SharedFetcher
	addondata.CharactersFetcher
}

// Parts is what the uploads are built from: the server, what the data folder keeps, and the saves.
type Parts struct {
	Server Server
	Records
	Saves func() []find.Save
	IsOff func(path string) bool
	// Market is one market whose prices to write instead of the account's ("" for the account's).
	Market  string
	DataDir string
	Out     io.Writer
	Report  *addondata.Report // may be nil
}

// Uploads is the upload core and, beside it, the refresher that keeps prices, shared lists and the
// account's characters in the game, fetching again whenever an upload was imported.
type Uploads struct {
	core      *core.Core
	refresher *addondata.Refresher
	imported  <-chan struct{}
}

// NewUploads builds the upload core and the price refresher from p.
func NewUploads(p Parts) *Uploads {
	imported, announce := addondata.Trigger()
	return &Uploads{
		core: &core.Core{
			Uploader: p.Server,
			Backups:  p.Backups, Log: p.Log, Refused: p.Refused, Watcher: watch.New(pollEvery),
			Saves: p.Saves, IsOff: p.IsOff, Out: p.Out, Imported: announce,
		},
		refresher: p.refresher(p.Server, p.Server, p.Server),
		imported:  imported,
	}
}

// refresher keeps the account's prices, shared lists and profiles and characters in the game, fetched
// with f, s and ch.
func (p Parts) refresher(f addondata.Fetcher, s addondata.SharedFetcher, ch addondata.CharactersFetcher) *addondata.Refresher {
	return &addondata.Refresher{
		Fetcher: f, Shared: s, Characters: ch, Saves: p.Saves, Market: p.Market,
		State: filepath.Join(p.DataDir, pricesState), Out: p.Out, Report: p.Report,
	}
}

// Run uploads and refreshes until ctx ends or uploading fails. When it returns, nothing it started
// writes into the data folder any more.
func (u *Uploads) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	var refreshing sync.WaitGroup
	refreshing.Go(func() { u.refresher.Run(ctx, refreshPricesEvery, time.After, u.imported) })
	err := u.core.Run(ctx, pollEvery)
	stop()
	refreshing.Wait()
	return err
}
