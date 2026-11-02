package install

import "strings"

func desktopEntry(exe string) []byte {
	return []byte("[Desktop Entry]\nType=Application\nName=" + appName +
		"\nComment=Uploads your Gold Forever saves\nExec=" + quoteExec(exe) + "\nTerminal=false\nCategories=Game;\n")
}

// quoteExec quotes a path for a desktop entry's Exec key. The specification unescapes the value as a
// string first, so each backslash its quoting rule adds is itself written twice; % starts a field code.
func quoteExec(path string) string {
	return `"` + execEscapes.Replace(path) + `"`
}

var execEscapes = strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`)

func autostartEntry(exe string) []byte {
	return []byte("[Desktop Entry]\nType=Application\nName=" + appName +
		"\nExec=" + quoteExec(exe) + " --autostart\nTerminal=false\nX-GNOME-Autostart-enabled=true\n")
}
