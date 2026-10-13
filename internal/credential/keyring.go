package credential

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// errKeyringUnavailable says this machine has no usable system keyring
// (for example a Linux session without the Secret Service).
var errKeyringUnavailable = errors.New("credential: the system keyring is not available")

// keyringStore is the operating system's credential store: Keychain on
// macOS, Credential Manager on Windows, the Secret Service on Linux.
type keyringStore struct{}

// Describe returns "keyring".
func (keyringStore) Describe() string { return "keyring" }

func (keyringStore) Get(account string) (string, error) {
	s, err := guard(func() (string, error) { return keyring.Get(Service, account) })
	return s, mapKeyringError(err)
}

func (keyringStore) Set(account, secret string) error {
	_, err := guard(func() (struct{}, error) {
		return struct{}{}, keyring.Set(Service, account, secret)
	})
	return mapKeyringError(err)
}

func (keyringStore) Delete(account string) error {
	_, err := guard(func() (struct{}, error) {
		return struct{}{}, keyring.Delete(Service, account)
	})
	return mapKeyringError(err)
}

// mapKeyringError translates go-keyring's errors. "Not found" becomes
// ErrNotFound and an unusable keyring becomes errKeyringUnavailable; the
// combined store uses both to decide whether to fall back to the file.
// Anything else is wrapped and reported as is.
func mapKeyringError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case isUnavailable(err):
		return errKeyringUnavailable
	}
	return fmt.Errorf("credential: keyring: %w", err)
}
