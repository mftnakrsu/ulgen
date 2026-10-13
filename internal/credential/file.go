package credential

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const fileName = "credentials.json"

// fileStore keeps secrets in a JSON object, one field per account, in a
// file with mode 0600 inside a directory with mode 0700.
type fileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store that keeps secrets in dir/credentials.json.
// It is the fallback when no system keyring is available, and the store
// tests use.
func NewFileStore(dir string) Store {
	return &fileStore{path: filepath.Join(dir, fileName)}
}

// Describe returns "file".
func (s *fileStore) Describe() string { return "file" }

// Path returns the credentials file path.
func (s *fileStore) Path() string { return s.path }

func (s *fileStore) Get(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return "", err
	}
	secret, ok := m[account]
	if !ok || secret == "" {
		return "", ErrNotFound
	}
	return secret, nil
}

func (s *fileStore) Set(account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[account] = secret
	return s.save(m)
}

func (s *fileStore) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := m[account]; !ok {
		return ErrNotFound
	}
	delete(m, account)
	return s.save(m)
}

// load reads the file. A missing file is an empty store. On Unix a file
// that others could read is refused rather than trusted.
func (s *fileStore) load() (map[string]string, error) {
	info, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("credential: %s has mode %04o; it must be readable only by you (chmod 600)", s.path, info.Mode().Perm())
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if len(bytes.TrimSpace(b)) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("credential: %s is not a valid credentials file: %w", s.path, err)
	}
	return m, nil
}

// save writes the whole map through a temporary file in the same directory
// and renames it into place, so a crash never leaves a half-written file.
// An empty map removes the file.
func (s *fileStore) save(m map[string]string) error {
	if len(m) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
