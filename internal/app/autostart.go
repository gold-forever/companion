package app

import "context"

// moveToApplications is why a macOS app run from its disk image, or from Downloads before macOS has
// cleared it, cannot start with the computer.
const moveToApplications = "Drag the app to Applications and open it from there: from where it runs now, it cannot start with your computer."

func (c *App) SetAutostart(ctx context.Context, on bool) error {
	if on && c.layout.Transient(c.layout.Target(c.running)) {
		return ErrMoveToApplications
	}
	if on {
		return c.layout.Autostart.Enable(c.layout.Target(c.running))
	}
	return c.layout.Autostart.Disable()
}
