package credential

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	dbus "github.com/godbus/dbus/v5"
	"github.com/zalando/go-keyring"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ulgen") // does not exist yet
	s := NewFileStore(dir)
	if _, err := s.Get("evren"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty store: %v, want ErrNotFound", err)
	}
	if err := s.Set("evren", "evren_llm_test"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("evren")
	if err != nil || got != "evren_llm_test" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	path := s.(Locator).Path()
	if path != filepath.Join(dir, "credentials.json") {
		t.Errorf("Path = %q", path)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v, %v; want 0600", info.Mode(), err)
		}
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %v, %v; want 0700", info.Mode(), err)
		}
	}
	if s.(Describer).Describe() != "file" {
		t.Errorf("Describe = %q", s.(Describer).Describe())
	}
	if err := s.Delete("evren"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("empty credentials file should be removed, stat: %v", err)
	}
	if err := s.Delete("evren"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete: %v, want ErrNotFound", err)
	}
}

func TestFileStoreKeepsOtherAccounts(t *testing.T) {
	s := NewFileStore(t.TempDir())
	for _, acc := range []string{"a", "b"} {
		if err := s.Set(acc, "secret-"+acc); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set("a", "secret-a2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("b"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a")
	if err != nil || got != "secret-a2" {
		t.Errorf("Get a = %q, %v", got, err)
	}
	if _, err := s.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get b = %v, want ErrNotFound", err)
	}
}

func TestFileStoreRefusesLooseMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"evren":"evren_llm_test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewFileStore(dir).Get("evren")
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "0644") {
		t.Errorf("err = %v, want a refusal naming the path and the mode", err)
	}
	if strings.Contains(err.Error(), "evren_llm_test") {
		t.Error("the error leaks the secret")
	}
}

func TestFileStoreRejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"evren":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir).Get("evren"); err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want a parse error naming the path", err)
	}
}

// fakeKeyring is an in-memory keyring. When fail is set every call returns it.
type fakeKeyring struct {
	m    map[string]string
	fail error
}

func (k *fakeKeyring) Describe() string { return "keyring" }

func (k *fakeKeyring) Get(account string) (string, error) {
	if k.fail != nil {
		return "", k.fail
	}
	s, ok := k.m[account]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}

func (k *fakeKeyring) Set(account, secret string) error {
	if k.fail != nil {
		return k.fail
	}
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[account] = secret
	return nil
}

func (k *fakeKeyring) Delete(account string) error {
	if k.fail != nil {
		return k.fail
	}
	if _, ok := k.m[account]; !ok {
		return ErrNotFound
	}
	delete(k.m, account)
	return nil
}

func TestCombinedFallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	dir := t.TempDir()
	c := newCombined(&fakeKeyring{fail: errKeyringUnavailable}, NewFileStore(dir))
	if err := c.Set("evren", "evren_llm_test"); err != nil {
		t.Fatal(err)
	}
	if c.Describe() != "file" {
		t.Errorf("Describe after Set = %q, want file", c.Describe())
	}
	got, err := c.Get("evren")
	if err != nil || got != "evren_llm_test" || c.Describe() != "file" {
		t.Errorf("Get = %q, %v, Describe %q", got, err, c.Describe())
	}
	if c.Path() != filepath.Join(dir, "credentials.json") {
		t.Errorf("Path = %q", c.Path())
	}
	if err := c.Delete("evren"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("evren"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := c.Delete("evren"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete on empty = %v, want ErrNotFound", err)
	}
}

func TestCombinedPrefersKeyringAndClearsFileCopy(t *testing.T) {
	file := NewFileStore(t.TempDir())
	if err := file.Set("evren", "old-file-copy"); err != nil {
		t.Fatal(err)
	}
	k := &fakeKeyring{}
	c := newCombined(k, file)
	if err := c.Set("evren", "evren_llm_new"); err != nil {
		t.Fatal(err)
	}
	if k.m["evren"] != "evren_llm_new" || c.Describe() != "keyring" {
		t.Errorf("keyring = %v Describe = %q", k.m, c.Describe())
	}
	if _, err := file.Get("evren"); !errors.Is(err, ErrNotFound) {
		t.Errorf("stale file copy still present: %v", err)
	}
	got, err := c.Get("evren")
	if err != nil || got != "evren_llm_new" || c.Describe() != "keyring" {
		t.Errorf("Get = %q, %v, Describe %q", got, err, c.Describe())
	}
}

func TestCombinedFindsOlderFileCopy(t *testing.T) {
	file := NewFileStore(t.TempDir())
	if err := file.Set("evren", "evren_llm_file"); err != nil {
		t.Fatal(err)
	}
	c := newCombined(&fakeKeyring{}, file) // keyring works but is empty
	got, err := c.Get("evren")
	if err != nil || got != "evren_llm_file" || c.Describe() != "file" {
		t.Errorf("Get = %q, %v, Describe %q", got, err, c.Describe())
	}
}

func TestCombinedReportsRealKeyringFailure(t *testing.T) {
	boom := errors.New("keychain locked")
	c := newCombined(&fakeKeyring{fail: boom}, NewFileStore(t.TempDir()))
	if _, err := c.Get("evren"); !errors.Is(err, boom) {
		t.Errorf("Get = %v, want the keyring's own error when the file has nothing", err)
	}
	// Set still succeeds through the file, so a broken keychain never blocks login.
	if err := c.Set("evren", "evren_llm_test"); err != nil || c.Describe() != "file" {
		t.Errorf("Set = %v Describe %q", err, c.Describe())
	}
}

func TestMapKeyringError(t *testing.T) {
	tests := []struct {
		name         string
		in           error
		wantUnavail  bool
		wantNotFound bool
	}{
		// wantUnavail is the expectation off Linux (keyring_other.go): only
		// the unsupported-platform error counts. On Linux keyring_linux.go
		// treats every error except "not found" as unavailable, so the same
		// rows are recomputed below.
		{"unsupported platform", keyring.ErrUnsupportedPlatform, true, false},
		{"dbus env missing", errors.New("dbus: DBUS_SESSION_BUS_ADDRESS not set"), false, false},
		{"dbus error value", dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, false, false},
		{"dbus-launch missing", errors.New(`exec: "dbus-launch": executable file not found in $PATH`), false, false},
		{"not found", keyring.ErrNotFound, false, true},
		{"keychain locked", errors.New("keychain locked"), false, false},
		{"nil", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// On Linux every error except "not found" means the keyring is
			// unavailable (see keyring_linux.go), so a locked keychain and a
			// missing session bus are classified the same way. Everywhere
			// else only the unsupported-platform error is (keyring_other.go).
			// The Linux behaviour also has direct tests in keyring_linux_test.go.
			wantUnavail := tt.wantUnavail
			if runtime.GOOS == "linux" {
				wantUnavail = tt.in != nil && !tt.wantNotFound
			}
			got := mapKeyringError(tt.in)
			if tt.in == nil {
				if got != nil {
					t.Fatalf("mapKeyringError(nil) = %v, want nil", got)
				}
				return
			}
			if errors.Is(got, errKeyringUnavailable) != wantUnavail {
				t.Errorf("mapKeyringError(%v) = %v; errors.Is errKeyringUnavailable = %v, want %v",
					tt.in, got, errors.Is(got, errKeyringUnavailable), wantUnavail)
			}
			if errors.Is(got, ErrNotFound) != tt.wantNotFound {
				t.Errorf("mapKeyringError(%v) = %v; errors.Is ErrNotFound = %v, want %v",
					tt.in, got, errors.Is(got, ErrNotFound), tt.wantNotFound)
			}
			if tt.name == "keychain locked" && runtime.GOOS != "linux" {
				if !strings.Contains(got.Error(), "keychain locked") {
					t.Errorf("error text lost the original: %v", got)
				}
				if !errors.Is(got, tt.in) {
					t.Errorf("error does not wrap the original: %v", got)
				}
			}
		})
	}
}

func TestCombinedSetIgnoresStaleFileRemovalError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	// A loose-mode file that the file store refuses to read, so its Delete
	// fails while the keyring Set succeeds.
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"evren":"evren_llm_old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	k := &fakeKeyring{}
	c := newCombined(k, NewFileStore(dir))
	if err := c.Set("evren", "evren_llm_x"); err != nil {
		t.Fatalf("Set = %v, want nil when the keyring succeeded", err)
	}
	if k.m["evren"] != "evren_llm_x" {
		t.Errorf("keyring = %v, want the new secret", k.m)
	}
	if c.Describe() != "keyring" {
		t.Errorf("Describe = %q, want keyring", c.Describe())
	}
}

func TestResolve(t *testing.T) {
	store := NewFileStore(t.TempDir())
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if _, err := Resolve(store, "evren", "EVREN_API_KEY", getenv); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty: %v, want ErrNotFound", err)
	}
	if err := store.Set("evren", "from-store"); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(store, "evren", "EVREN_API_KEY", getenv)
	if err != nil || r.Secret != "from-store" || r.Source != "file" {
		t.Errorf("store: %+v, %v", r, err)
	}
	env["EVREN_API_KEY"] = " from-env "
	r, err = Resolve(store, "evren", "EVREN_API_KEY", getenv)
	if err != nil || r.Secret != "from-env" || r.Source != "env" {
		t.Errorf("env: %+v, %v", r, err)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir(func(string) string { return "" })
	if err != nil || filepath.Base(dir) != "ulgen" {
		t.Errorf("DefaultDir = %q, %v", dir, err)
	}
	custom := filepath.Join(t.TempDir(), "custom")
	dir, err = DefaultDir(func(k string) string {
		if k == "ULGEN_CONFIG_DIR" {
			return custom
		}
		return ""
	})
	if err != nil || dir != custom {
		t.Errorf("DefaultDir with override = %q, %v", dir, err)
	}
}

func TestOpenReturnsStore(t *testing.T) {
	s, err := Open(t.TempDir(), func(string) string { return "" })
	if err != nil || s == nil {
		t.Fatalf("Open = %v, %v", s, err)
	}
	if _, ok := s.(Describer); !ok {
		t.Error("store does not implement Describer")
	}
	if _, ok := s.(Locator); !ok {
		t.Error("store does not implement Locator")
	}
}

// TestOpenWithNoKeyring covers the ULGEN_NO_KEYRING switch: with it set, Open
// returns the file store alone, whatever the machine and whether or not a
// keyring would work.
func TestOpenWithNoKeyring(t *testing.T) {
	dir := t.TempDir()
	// A test-only account name: if the switch ever stopped working, Open would
	// write through the real keyring and must not overwrite the developer's
	// saved EVREN key under "evren".
	const account = "ulgen-test-no-keyring"
	s, err := Open(dir, func(k string) string {
		if k == "ULGEN_NO_KEYRING" {
			return "1"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Delete(account) })
	if err := s.Set(account, "evren_llm_test"); err != nil {
		t.Fatal(err)
	}
	if d, ok := s.(Describer); !ok || d.Describe() != "file" {
		t.Errorf("Describe = %q, want file", s.(Describer).Describe())
	}
	if got, err := s.Get(account); err != nil || got != "evren_llm_test" {
		t.Errorf("Get = %q, %v", got, err)
	}
}
