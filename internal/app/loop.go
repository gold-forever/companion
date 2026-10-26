package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/gold-forever/companion/internal/server"
)

// loop runs the uploads until ctx ends: with the current token and folders, starting again whenever
// they change, and waiting for a change while there is no token or the server refused it.
func (c *App) loop(ctx context.Context) {
	for ctx.Err() == nil {
		changed := c.settings.Changes()
		token := c.settings.Get().Token
		if token == "" {
			c.status.Set("Not linked yet: open Settings and press \"Link this computer\".")
		} else {
			run, stop := context.WithCancel(ctx)
			var started sync.WaitGroup
			started.Go(func() {
				select {
				case <-changed:
					stop()
				case <-run.Done():
				}
			})
			client := &server.Client{BaseURL: c.url(), Token: token, Version: c.version, OS: goos}
			started.Go(func() { c.followAccount(run, client) })
			err := NewUploads(c.parts(client)).Run(run)
			stop()
			started.Wait() // the next round, or the caller, owns the data folder alone
			if err == nil {
				continue // a change, or the companion is stopping
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
		}
	}
}

// lastLine keeps the last line the upload loop wrote, for the tray and the page, and passes every
// line on to standard output.
type lastLine struct {
	mu   sync.Mutex
	line string
	out  io.Writer
}

func (l *lastLine) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lines := bytes.Split(bytes.TrimRight(p, "\n"), []byte("\n")); len(lines) > 0 {
		l.line = string(lines[len(lines)-1])
	}
	if l.out != nil {
		l.out.Write(p)
	}
	return len(p), nil
}

func (l *lastLine) Set(line string) { fmt.Fprintln(l, line) }

func (l *lastLine) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}
