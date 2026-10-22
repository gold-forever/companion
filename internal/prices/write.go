package prices

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gold-forever/companion/internal/find"
)

// Fetcher gets one market's answer from the server; api.Client is the real one.
type Fetcher interface {
	Prices(ctx context.Context, game, market string) ([]byte, error)
}

const game = "forever"

// Refresh fetches market once and writes it into every Forever flavour the saves are in. A failed
// fetch or an answer out of shape writes nothing, so the last good files stay.
func Refresh(ctx context.Context, f Fetcher, saves []find.Save, market string, out io.Writer) {
	flavours := foreverFlavours(saves)
	if market == "" || len(flavours) == 0 {
		return
	}
	body, err := f.Prices(ctx, game, market)
	if err != nil {
		fmt.Fprintf(out, "prices for %s not fetched: %v\n", market, err)
		return
	}
	p, err := Parse(body, game, market)
	if err != nil {
		fmt.Fprintf(out, "prices for %s refused: %v\n", market, err)
		return
	}
	for dir, iface := range flavours {
		if err := Write(dir, iface, p); err != nil {
			fmt.Fprintf(out, "prices not written into %s: %v\n", dir, err)
			continue
		}
		fmt.Fprintf(out, "prices for %s written into %s (%d items)\n", market, dir, len(p.Items))
	}
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
	dir := filepath.Join(flavourDir, "Interface", "AddOns", "GoldForever_Data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := replace(filepath.Join(dir, "GoldForever_Data.toc"), TOC(iface)); err != nil {
		return err
	}
	return replace(filepath.Join(dir, "Data.lua"), Lua(p))
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
