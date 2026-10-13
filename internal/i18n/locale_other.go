//go:build !windows

package i18n

// defaultSystemLocale returns "" on Unix-like systems, where the environment
// variables consulted by Detect are the operating system's locale setting.
func defaultSystemLocale() string { return "" }
