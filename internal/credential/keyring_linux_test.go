//go:build linux

package credential

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestLinuxIsUnavailable(t *testing.T) {
	if !isUnavailable(errors.New("dial unix /run/user/1000/bus: connect: no such file or directory")) {
		t.Error("an unreachable session bus must mean the keyring is unavailable")
	}
	if isUnavailable(keyring.ErrNotFound) {
		t.Error("not found must not count as unavailable")
	}
	if isUnavailable(nil) {
		t.Error("nil must not count as unavailable")
	}
}

func TestLinuxKeyringUsable(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	// Point runUserRoot at a temporary directory so the real /run/user, which
	// usually holds a live session bus on a desktop, cannot make this test
	// answer true by accident.
	root := t.TempDir()
	saved := runUserRoot
	runUserRoot = root
	t.Cleanup(func() { runUserRoot = saved })
	uid := strconv.Itoa(os.Getuid())
	busDir := filepath.Join(root, uid)

	// Nothing set and no socket: the keyring must not be tried, or the
	// library would start a stray dbus-daemon through dbus-launch.
	if keyringUsable(env(map[string]string{})) {
		t.Error("with nothing set and no socket, keyringUsable should be false")
	}
	// A DBUS_SESSION_BUS_ADDRESS that names a bus is used as is.
	if !keyringUsable(env(map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/x"})) {
		t.Error("with DBUS_SESSION_BUS_ADDRESS naming a bus, keyringUsable should be true")
	}
	// An "autolaunch:" address means starting a bus, which we refuse.
	if keyringUsable(env(map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"})) {
		t.Error("with an autolaunch: address and no socket, keyringUsable should be false")
	}
	// A socket at <runUserRoot>/<uid>/bus names the bus too.
	if err := os.MkdirAll(busDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busDir, "bus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !keyringUsable(env(map[string]string{})) {
		t.Error("with a <runUserRoot>/<uid>/bus file, keyringUsable should be true")
	}
	// With "autolaunch:" and a socket the library uses the socket and starts
	// nothing, so the keyring is usable.
	if !keyringUsable(env(map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"})) {
		t.Error("with an autolaunch: address and a socket, keyringUsable should be true")
	}
}

func TestLinuxGuardTimesOut(t *testing.T) {
	saved := keyringTimeout
	keyringTimeout = 20 * time.Millisecond
	t.Cleanup(func() { keyringTimeout = saved })

	block := make(chan struct{})
	defer close(block)
	_, err := guard(func() (string, error) {
		<-block
		return "late", nil
	})
	if !errors.Is(err, errKeyringUnavailable) {
		t.Errorf("guard = %v, want errKeyringUnavailable", err)
	}
}
