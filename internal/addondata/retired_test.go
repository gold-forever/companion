package addondata

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dings table an older companion wrote into Data.lua, and kept in its state file.
const olderDings = "  dings = {\n    at = 1761150000,\n    webhook = true,\n    characters = {\n      [\"Player-4620-006C0FC1\"] = true,\n    },\n  },\n"

func TestAFileAnOlderCompanionWroteWithDingsIsWrittenAgainWithout(t *testing.T) {
	install, saves := tree(t, map[string]int{"_forever_": 16001})
	state := filepath.Join(t.TempDir(), "prices.json")
	plain := Lua(mustParse(t, canned(t)))
	older := strings.Replace(string(plain), "  markets = {\n", olderDings+"  markets = {\n", 1)
	if err := Write(filepath.Join(install, "_forever_"), 16001, []byte(older)); err != nil {
		t.Fatal(err)
	}
	held, _ := json.Marshal(map[string]any{
		"forever": map[string]string{"ask": market, "etag": `"e1"`, "dings": olderDings},
		"dings":   map[string]any{"ask": "", "etag": `"d1"`, "state": json.RawMessage(`{"v":1}`)},
	})
	if err := os.WriteFile(state, held, 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fetcher{body: canned(t), etag: `"e1"`}

	if err := refresher(f, saves, market, state).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(f.sent) != 1 || f.sent[0] != "" {
		t.Fatalf("prices asked with %q; want them in full, to write the file without its dings", f.sent)
	}
	if got := dataLua(t, install); !bytes.Equal(got, plain) {
		t.Fatalf("Data.lua still holds an older companion's entry:\n%s", got)
	}
}
