package tray

import "fyne.io/systray"

// lookBeforeStart gives the item its icon, title and tooltip before systray puts it on the bus. Set
// from build, which runs alongside that, they can land after systray has read them and before it can
// pass them on, and are lost: an item with no icon, which the desktop does not draw.
func lookBeforeStart() {
	systray.SetIcon(pngIcon())
	systray.SetTitle(name)
	systray.SetTooltip(name)
}

// lookOnceUp does nothing: the item has its look from the start.
func lookOnceUp() {}
