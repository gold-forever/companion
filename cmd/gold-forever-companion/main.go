// Command gold-forever-companion uploads the GoldForever.lua the game writes to Gold Forever.
// This first version uploads one file and exits; watching every install comes next (design 006).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/magnusev/gold-forever-companion/internal/api"
)

// version is set at build time (-X main.version=…); a plain `go build` reports dev.
var version = "dev"

const defaultURL = "https://gold-forever.evensberget.com"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is the whole program: 0 on success, 1 when the upload failed, 2 for a usage error.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
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
		fmt.Fprintln(stderr, "only --once --file <GoldForever.lua> is built yet; watching your installs comes next")
		return 2
	}
	return uploadOnce(*file, &api.Client{BaseURL: *url, Token: *token, Version: version, OS: runtime.GOOS}, stdout, stderr)
}

func uploadOnce(path string, client *api.Client, stdout, stderr io.Writer) int {
	save, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	answer, err := client.Upload(context.Background(), save)
	if err != nil {
		var refused *api.RefusedError
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
