package page

import (
	"embed"
	"io/fs"
)

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

//go:embed static
var static embed.FS
