package install

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runValue = appName

// RunKey is autostart through the current user's Run key, as most programs that start at login do.
func RunKey() Autostart { return runKey{} }

type runKey struct{}

func (runKey) Enable(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, `"`+exe+`" --autostart`)
}

func (runKey) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runPath, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func (runKey) Enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}
