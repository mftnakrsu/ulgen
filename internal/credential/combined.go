package credential

import (
	"errors"
	"sync"
)

// combined uses the keyring and falls back to the file. A secret lives in
// one place: Set prefers the keyring and removes any stale file copy.
type combined struct {
	keyring Store
	file    Store
	mu      sync.Mutex
	last    string // backend that answered the last Get or Set
}

func newCombined(keyring, file Store) *combined {
	return &combined{keyring: keyring, file: file}
}

// Describe returns "keyring" or "file", whichever answered last.
func (c *combined) Describe() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Path returns the fallback file's path.
func (c *combined) Path() string {
	if l, ok := c.file.(Locator); ok {
		return l.Path()
	}
	return ""
}

func (c *combined) remember(backend string) {
	c.mu.Lock()
	c.last = backend
	c.mu.Unlock()
}

// Get asks the keyring, then the file. When the keyring fails for a reason
// other than "nothing there" and the file has nothing either, the keyring's
// error is returned so the user learns about it.
func (c *combined) Get(account string) (string, error) {
	secret, kerr := c.keyring.Get(account)
	if kerr == nil {
		c.remember("keyring")
		return secret, nil
	}
	secret, ferr := c.file.Get(account)
	if ferr == nil {
		c.remember("file")
		return secret, nil
	}
	if errors.Is(kerr, ErrNotFound) || errors.Is(kerr, errKeyringUnavailable) {
		return "", ferr
	}
	return "", kerr
}

// Set writes to the keyring; when that fails for any reason the file takes
// the secret, so a broken keychain never blocks signing in. Once the
// keyring holds the secret, removing any stale file copy is best effort:
// its error is ignored, because Get asks the keyring first and Delete
// clears both places.
func (c *combined) Set(account, secret string) error {
	if err := c.keyring.Set(account, secret); err == nil {
		c.remember("keyring")
		_ = c.file.Delete(account)
		return nil
	}
	if err := c.file.Set(account, secret); err != nil {
		return err
	}
	c.remember("file")
	return nil
}

// Delete removes the secret from both places. ErrNotFound means neither had it.
func (c *combined) Delete(account string) error {
	kerr := c.keyring.Delete(account)
	ferr := c.file.Delete(account)
	if ferr != nil && !errors.Is(ferr, ErrNotFound) {
		return ferr
	}
	if kerr != nil && !errors.Is(kerr, ErrNotFound) && !errors.Is(kerr, errKeyringUnavailable) {
		return kerr
	}
	if kerr == nil || ferr == nil {
		return nil
	}
	return ErrNotFound
}
