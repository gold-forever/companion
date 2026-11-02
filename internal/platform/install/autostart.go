package install

import (
	"bytes"
	"html"
	"os"
)

// fileAutostart is a file the desktop reads at login: a LaunchAgent, or an XDG autostart entry.
type fileAutostart struct {
	path    string
	content func(exe string) []byte
}

func (a *fileAutostart) Enable(exe string) error { return writeFile(a.path, a.content(exe), 0o644) }

func (a *fileAutostart) Disable() error { return removeIfThere(a.path) }

func (a *fileAutostart) Enabled() bool {
	_, err := os.Stat(a.path)
	return err == nil
}

func (a *fileAutostart) StartsAt(exe string) bool {
	data, err := os.ReadFile(a.path)
	return err == nil && bytes.Equal(data, a.content(exe))
}

func launchAgent(exe string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + agentID + `</string>
<key>ProgramArguments</key><array><string>` + html.EscapeString(exe) + `</string><string>--autostart</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`)
}
