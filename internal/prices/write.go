package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/api"
	"github.com/gold-forever/companion/internal/find"
)

// Fetcher gets one market's answer and its ETag from the server, or api.ErrNotModified when etag is
// still current; api.Client is the real one.
type Fetcher interface {
	Prices(ctx context.Context, game, market, etag string) ([]byte, string, error)
}

const game = "forever"

// Refresher keeps one market's prices in every Forever flavour the saves are in.
type Refresher struct {
	Fetcher Fetcher
	Saves   func() []find.Save
	Market  string // empty writes none
	State   string // the file holding the ETag of each market Data.lua holds now
	Out     io.Writer
}

// Run refreshes at once, then every interval and after each imported upload, until ctx ends. A server
// that fails is asked again sooner, waiting as uploads do; after is the clock (time.After).
func (r *Refresher) Run(ctx context.Context, every time.Duration, after func(time.Duration) <-chan time.Time, imported <-chan struct{}) {
	var backoff time.Duration
	for {
		wait := every
		var limited *api.RateLimitedError
		switch err := r.Refresh(ctx); {
		case errors.As(err, &limited):
			wait = limited.RetryAfter
		case api.Retryable(err):
			backoff = api.NextWait(backoff)
			wait = min(backoff, every)
		default:
			backoff = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		case <-imported:
		}
	}
}

// Trigger makes the channel Run listens on and the function that pokes it without waiting: pokes
// while a refresh runs fold into one more.
func Trigger() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	return ch, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Refresh fetches the market once, asking only for a change when every flavour has its file, and
// writes it into each flavour. A 304, a failed fetch or an answer out of shape writes nothing, so the
// last good files stay; the error is the fetch's.
func (r *Refresher) Refresh(ctx context.Context) error {
	flavours := foreverFlavours(r.Saves())
	if r.Market == "" || len(flavours) == 0 {
		return nil
	}
	etags := r.etags()
	etag := etags[r.Market]
	if !allHaveData(flavours) {
		etag = ""
	}
	body, etag, err := r.Fetcher.Prices(ctx, game, r.Market, etag)
	if errors.Is(err, api.ErrNotModified) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(r.Out, "prices for %s not fetched: %v\n", r.Market, err)
		return err
	}
	p, err := Parse(body, game, r.Market)
	if err != nil {
		fmt.Fprintf(r.Out, "prices for %s refused: %v\n", r.Market, err)
		return nil
	}
	written := map[string]string{r.Market: etag}
	for dir, iface := range flavours {
		if err := Write(dir, iface, p); err != nil {
			fmt.Fprintf(r.Out, "prices not written into %s: %v\n", dir, err)
			written = nil // a flavour without this answer must be fetched in full next time
			continue
		}
		fmt.Fprintf(r.Out, "prices for %s written into %s (%d items)\n", r.Market, dir, len(p.Items))
	}
	if err := r.saveETags(written); err != nil {
		fmt.Fprintf(r.Out, "could not keep the prices' ETag: %v\n", err)
	}
	return nil
}

// etags reads the state; anything unreadable is no ETag, which costs one full fetch.
func (r *Refresher) etags() map[string]string {
	var etags map[string]string
	data, err := os.ReadFile(r.State)
	if err == nil {
		_ = json.Unmarshal(data, &etags)
	}
	return etags
}

func (r *Refresher) saveETags(etags map[string]string) error {
	data, err := json.Marshal(etags)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.State), 0o700); err != nil {
		return err
	}
	return replace(r.State, append(data, '\n'))
}

// allHaveData says whether every flavour already has a Data.lua, which a 304 would leave as it is.
func allHaveData(flavours map[string]int) bool {
	for dir := range flavours {
		if _, err := os.Stat(filepath.Join(addonDir(dir), "Data.lua")); err != nil {
			return false
		}
	}
	return true
}

// foreverFlavours maps each Forever flavour folder to its interface number, read from its saves.
func foreverFlavours(saves []find.Save) map[string]int {
	out := map[string]int{}
	for _, s := range saves {
		data, err := os.ReadFile(s.Path)
		if err != nil || find.Game(data) != game {
			continue
		}
		dir := filepath.Join(s.Install, s.Flavour)
		out[dir] = max(out[dir], find.Interface(data))
	}
	return out
}

// Write puts the data addon into a flavour folder, each file through a temporary file renamed into
// place, so the game never loads half a file.
func Write(flavourDir string, iface int, p Prices) error {
	dir := addonDir(flavourDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := replace(filepath.Join(dir, "GoldForever_Data.toc"), TOC(iface)); err != nil {
		return err
	}
	return replace(filepath.Join(dir, "Data.lua"), Lua(p))
}

func addonDir(flavourDir string) string {
	return filepath.Join(flavourDir, "Interface", "AddOns", "GoldForever_Data")
}

func replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; cleans up after a failure
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
