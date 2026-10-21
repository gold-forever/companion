//go:build !windows

package main

import (
	"fmt"
	"os"
)

// alert prints the error; started from a desktop, it goes to the session's log.
func alert(title, message string) { fmt.Fprintf(os.Stderr, "%s: %s\n", title, message) }
