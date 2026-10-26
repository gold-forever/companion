package find

import "golang.org/x/sys/windows/registry"

// RegistryInstall is the folder Battle.net recorded for World of Warcraft, or "".
func RegistryInstall() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	path, _, err := key.GetStringValue("InstallPath")
	if err != nil {
		return ""
	}
	return path
}
