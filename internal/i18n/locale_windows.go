//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

// localeNameMaxLength is LOCALE_NAME_MAX_LENGTH from the Windows SDK.
const localeNameMaxLength = 85

// defaultSystemLocale returns the user's default locale name on Windows, such
// as "tr-TR", or "" when it cannot be read. Windows rarely sets LANG, so this
// is how a Turkish Windows user gets a Turkish interface without configuration.
func defaultSystemLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if err := proc.Find(); err != nil {
		return ""
	}
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
