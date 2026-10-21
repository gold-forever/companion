//go:build !windows

package find

// RegistryInstall is the folder Battle.net recorded for World of Warcraft: there is no registry here.
func RegistryInstall() string { return "" }
