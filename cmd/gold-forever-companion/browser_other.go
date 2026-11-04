//go:build !windows

package main

func openBrowser(url string) error {
	if goos == "darwin" {
		return start("open", url)
	}
	return start("xdg-open", url)
}
