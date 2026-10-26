// Command gold-forever-companion uploads the GoldForever.lua saves World of Warcraft writes to Gold
// Forever. Double-clicked, it installs itself and runs in the tray with a settings page; `link`
// connects it to your account, `run` watches every install it finds without either, and
// `--once --file` uploads one file and exits.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/gold-forever/companion/internal/account/config"
	"github.com/gold-forever/companion/internal/account/link"
	"github.com/gold-forever/companion/internal/app"
	"github.com/gold-forever/companion/internal/platform/instance"
	"github.com/gold-forever/companion/internal/saves/find"
	"github.com/gold-forever/companion/internal/server"
)

// version is set at build time (-X main.version=…); a plain `go build` reports dev.
var version = "dev"

const defaultURL = "https://gold-forever.app"

const usageAdvice = "usage: gold-forever-companion link   (connect this computer to your Gold Forever account)\n" +
	"       gold-forever-companion run    (watch every WoW install and upload each new save)\n" +
	"       gold-forever-companion --once --file <GoldForever.lua>"

// pollSleep paces the wait for a link; nil is the real clock. Tests make it instant.
var pollSleep func(ctx context.Context, d time.Duration) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is the whole program: 0 on success, 1 when uploading failed, 2 for a usage error.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		return player(ctx, "")
	case args[0] == "--autostart" || args[0] == "--installed":
		return player(ctx, args[0])
	case args[0] == "run":
		return watchAndUpload(ctx, args[1:], getenv, stdout, stderr)
	case args[0] == "link":
		return linkAccount(ctx, args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("gold-forever-companion", flag.ContinueOnError)
	flags.SetOutput(stderr)
	once := flags.Bool("once", false, "upload --file once and exit")
	file := flags.String("file", "", "the GoldForever.lua to upload")
	token := flags.String("token", "", "your upload token (better: set GF_TOKEN, which stays out of shell history)")
	url := flags.String("url", defaultURL, "the Gold Forever site")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "gold-forever-companion", version)
		return 0
	}
	if *token == "" {
		*token = getenv("GF_TOKEN")
	}
	if *token == "" {
		fmt.Fprintln(stderr, "no token: set GF_TOKEN, or pass --token (make one at /me/tokens)")
		return 2
	}
	if !*once || *file == "" {
		fmt.Fprintln(stderr, usageAdvice)
		return 2
	}
	return uploadOnce(ctx, *file, client(*url, *token), stdout, stderr)
}

// watchAndUpload is `run`: settings from the config file, the token from it or GF_TOKEN.
func watchAndUpload(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gold-forever-companion run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	market := flags.String("market", "", "one market (forever-… or retail-…) whose prices to write into its game, instead of the account's markets there (default: the settings' market)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	settings, err := config.Open(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read %s: %v\n", cfgPath, err)
		return 1
	}
	cfg := settings.Get()
	token := cfg.Token
	if token == "" {
		token = getenv("GF_TOKEN")
	}
	if token == "" {
		fmt.Fprintf(stderr, "no token: set GF_TOKEN, or put one in %s (make one at /me/tokens)\n", cfgPath)
		return 2
	}
	url := cfg.URL
	if url == "" {
		url = defaultURL
	}
	home, _ := os.UserHomeDir()
	data := config.DataDir(runtime.GOOS, home, getenv)
	// One copy per user, as the tray app: two would share one backups folder and log.
	release, err := instance.Claim(data)
	if errors.Is(err, instance.ErrRunning) {
		fmt.Fprintln(stderr, "another copy of the companion is already running (the app, or another run): quit it first")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "could not make sure only one companion runs: %v\n", err)
		return 1
	}
	defer release()
	records := app.OpenRecords(data)
	_ = records.Log.Prune(time.Now())
	installs := append(cfg.Folders, find.Candidates(runtime.GOOS, home, getenv, find.RegistryInstall)...)
	if *market == "" {
		*market = cfg.Market
	}
	uploads := app.Parts{
		Server:  client(url, token),
		Records: records,
		Saves:   func() []find.Save { return find.Saves(installs) },
		IsOff:   cfg.IsOff,
		Market:  *market,
		DataDir: data,
		Out:     stdout,
	}
	saves := uploads.Saves()
	if len(saves) == 0 {
		fmt.Fprintln(stdout, "no GoldForever.lua found yet; add your WoW folder to", cfgPath)
	}
	for _, s := range saves {
		fmt.Fprintln(stdout, "watching", s.Label())
	}
	err = app.NewUploads(uploads).Run(ctx) // nothing writes into the data folder once it has returned
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// linkAccount is `link`: a code to approve on the site, then the token saved in the config file. The
// token itself is never printed.
func linkAccount(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gold-forever-companion link", flag.ContinueOnError)
	flags.SetOutput(stderr)
	url := flags.String("url", defaultURL, "the Gold Forever site")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	hostname, _ := os.Hostname()
	c := client(*url, "")
	c.Sleep = pollSleep
	show := func(code server.DeviceCode) {
		fmt.Fprintf(stdout, "Open %s\nand approve the code %s (it is valid for %d minutes).\n",
			code.VerificationURIComplete, code.UserCode, code.ExpiresIn/60)
		fmt.Fprintln(stdout, link.SharingLine)
	}
	if err := link.Link(ctx, c, hostname, cfgPath, show); err != nil {
		fmt.Fprintln(stderr, "not linked:", err)
		return 1
	}
	if *url != defaultURL {
		// Remember a site other than the public one, so `run` uploads where the account was linked.
		settings, err := config.Open(cfgPath)
		if err == nil {
			err = settings.Update(func(cfg *config.Config) error { cfg.URL = *url; return nil })
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "Linked. The token is saved in", cfgPath)
	return 0
}

func client(url, token string) *server.Client {
	return &server.Client{BaseURL: url, Token: token, Version: version, OS: runtime.GOOS}
}

func uploadOnce(ctx context.Context, path string, client *server.Client, stdout, stderr io.Writer) int {
	save, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	answer, err := client.Upload(ctx, save)
	if err != nil {
		var refused *server.RefusedError
		if errors.As(err, &refused) {
			fmt.Fprintln(stderr, "refused:", refused.Reason)
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	fmt.Fprintf(stdout, "%s (%d problems, %d saves on your account)\n", answer.Status, answer.Problems, answer.Saves)
	return 0
}
