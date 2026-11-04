package main

import "golang.org/x/sys/windows"

// openBrowser asks the shell to open url, as a click on a link does. Not rundll32's
// url.dll,FileProtocolHandler: antivirus behaviour rules flag that as a way to run programs.
func openBrowser(url string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
