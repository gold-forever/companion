package main

import "golang.org/x/sys/windows"

// alert shows a message box: the Windows build has no console to print an error to.
func alert(title, message string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}
