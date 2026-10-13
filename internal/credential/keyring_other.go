//go:build !linux

package credential

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keyringUsable reports whether the system keyring should be tried at all.
// Everywhere but Linux there is a real per-user store, so it is always true.
func keyringUsable(getenv func(string) string) bool { return true }

// isUnavailable reports whether err means the keyring cannot be used here.
// Outside Linux that is exactly the unsupported-platform error; any other
// failure is a real problem the user should see.
func isUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, keyring.ErrUnsupportedPlatform)
}

// guard runs one keyring call. Outside Linux there is nothing to bound.
func guard[T any](call func() (T, error)) (T, error) { return call() }
