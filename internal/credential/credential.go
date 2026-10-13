// Package credential stores the user's API keys: in the operating system's
// credential store when there is one, otherwise in a file only the user can
// read. Nothing in this package writes a secret into a log or an error.
package credential

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Service is the name under which Ülgen's secrets appear in the keyring.
const Service = "ulgen"

// ErrNotFound reports that no secret is stored for the account.
var ErrNotFound = errors.New("credential: not found")

// Store keeps one secret per account.
type Store interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// Describer names the backend that answered the last Get or Set: "keyring"
// or "file". It returns "" before any call.
type Describer interface {
	Describe() string
}

// Locator tells where the fallback file lives.
type Locator interface {
	Path() string
}

// DefaultDir is the directory for Ülgen's configuration and the fallback
// credentials file: ULGEN_CONFIG_DIR when set, otherwise the operating
// system's user configuration directory plus "ulgen" (~/.config/ulgen on
// Linux, ~/Library/Application Support/ulgen on macOS, %AppData%\ulgen on
// Windows).
func DefaultDir(getenv func(string) string) (string, error) {
	if dir := strings.TrimSpace(getenv("ULGEN_CONFIG_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("credential: no configuration directory: %w", err)
	}
	return filepath.Join(base, "ulgen"), nil
}

// Open returns the store for this machine: the system keyring with the
// file in dir as fallback. An empty dir means DefaultDir. The keyring is
// not touched until the first call, so opening never shows a dialog.
//
// ULGEN_NO_KEYRING set to anything but an empty string skips the keyring
// altogether, and so does a machine (Linux without a session bus) where the
// keyring cannot be reached without starting one.
func Open(dir string, getenv func(string) string) (Store, error) {
	if dir == "" {
		var err error
		if dir, err = DefaultDir(getenv); err != nil {
			return nil, err
		}
	}
	file := NewFileStore(dir)
	if getenv("ULGEN_NO_KEYRING") != "" || !keyringUsable(getenv) {
		return file, nil
	}
	return newCombined(keyringStore{}, file), nil
}

// Resolved is a secret and where it came from: "env", "keyring" or "file".
type Resolved struct {
	Secret string
	Source string
}

// Resolve finds the secret for account: the environment variable envName
// wins when set, otherwise the store answers. ErrNotFound when neither has it.
func Resolve(store Store, account, envName string, getenv func(string) string) (Resolved, error) {
	if envName != "" {
		if v := strings.TrimSpace(getenv(envName)); v != "" {
			return Resolved{Secret: v, Source: "env"}, nil
		}
	}
	secret, err := store.Get(account)
	if err != nil {
		return Resolved{}, err
	}
	source := "file"
	if d, ok := store.(Describer); ok {
		if got := d.Describe(); got != "" {
			source = got
		}
	}
	return Resolved{Secret: secret, Source: source}, nil
}
