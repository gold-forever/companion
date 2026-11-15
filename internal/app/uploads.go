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

// Server is what the uploads ask of the Gold Forever server: to take a save, the prices, shared
// lists and the account's characters they keep in the game, and the stream that signals their changes.
type Server interface {
	core.Uploader
	addondata.Fetcher
	addondata.SharedFetcher
	addondata.CharactersFetcher
	eventSource
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
	Stream  *streamState      // may be nil
}

// Uploads is the upload core and, beside it, the refresher that keeps prices, shared lists and the
// account's characters in the game, fetching again whenever an upload was imported or the event stream
// signals a change.
type Uploads struct {
	core      *core.Core
	refresher *addondata.Refresher
	listener  listener
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
		listener:  p.listener(announce),
		imported:  imported,
	}
}

// listener follows the server's event stream, poking the refresh with poke.
func (p Parts) listener(poke func()) listener {
	state := p.Stream
	if state == nil {
		state = &streamState{}
	}
	return listener{events: p.Server, poke: poke, carries: p.carries, state: state, after: time.After}
}

// carries says whether the game's data holds market's prices: so it is when no Data.lua holds any
// market yet, as before the first write.
func (p Parts) carries(market string) bool {
	if p.Report == nil {
		return true
	}
	none := true
	for _, f := range p.Report.Files(p.Saves()) {
		for _, m := range f.Markets {
			if m.Key == market {
				return true
			}
			none = false
		}
	}
	return none
}

// refresher keeps the account's prices, shared lists and profiles and characters in the game, fetched
// with f, s and ch.
func (p Parts) refresher(f addondata.Fetcher, s addondata.SharedFetcher, ch addondata.CharactersFetcher) *addondata.Refresher {
	return &addondata.Refresher{
		Fetcher: f, Shared: s, Characters: ch, Saves: p.Saves, Market: p.Market,
		State: filepath.Join(p.DataDir, pricesState), Out: p.Out, Report: p.Report, Gap: rewriteGap,
	}
}

// Run uploads and refreshes until ctx ends or uploading fails. When it returns, nothing it started
// writes into the data folder any more.
func (u *Uploads) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	var refreshing sync.WaitGroup
	refreshing.Go(func() { u.refresher.Run(ctx, refreshPricesEvery, time.After, u.imported) })
	refreshing.Go(func() { u.listener.Run(ctx) })
	err := u.core.Run(ctx, pollEvery)
	stop()
	refreshing.Wait()
	return err
}
