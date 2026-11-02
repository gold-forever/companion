package app

import (
	"path/filepath"
	"time"

	"github.com/gold-forever/companion/internal/saves/backup"
	"github.com/gold-forever/companion/internal/saves/refused"
	"github.com/gold-forever/companion/internal/upload/sendlog"
)

// InstancePath is the file in dataDir that says which copy runs and where its page is.
func InstancePath(dataDir string) string { return filepath.Join(dataDir, instanceFile) }

// Records is what the data folder keeps across runs: copies of the saves, the upload log and the saves
// the server refused as too large.
type Records struct {
	Backups *backup.Store
	Log     *sendlog.Log
	Refused *refused.List
}

func OpenRecords(dataDir string) Records {
	return Records{
		Backups: &backup.Store{Dir: filepath.Join(dataDir, "backups"), Keep: keepCopies, Now: time.Now},
		Log:     &sendlog.Log{Path: filepath.Join(dataDir, "sent.jsonl"), Keep: keepLogFor},
		Refused: refused.Load(filepath.Join(dataDir, refusedFile)),
	}
}
