package alert

import (
	"context"

	"golang.org/x/sys/windows"
)

// Show shows a message box: the Windows build has no console to print an error to.
func Show(title, message string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}

// idYes is what MessageBox answers for its Yes button (IDYES).
const idYes = 6

// Ask shows a message box with Yes and No, which stand for yes and no: their own words go into the
// message, as a message box cannot name its buttons. Closing it is no.
func Ask(_ context.Context, title, message, yes, no string) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message + "\n\nYes: " + yes + ". No: " + no + ".")
	answer, _ := windows.MessageBox(0, m, t, windows.MB_YESNO|windows.MB_ICONINFORMATION)
	return answer == idYes
}
