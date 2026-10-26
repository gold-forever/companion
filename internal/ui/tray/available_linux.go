package tray

import "github.com/godbus/dbus/v5"

// Available says whether the desktop shows tray icons: one that does runs a StatusNotifierWatcher on
// the session bus. Plain GNOME does not, so there the settings page opens at start instead.
func Available() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}
