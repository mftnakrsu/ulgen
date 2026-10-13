//go:build linux

package credential

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// keyringTimeout bounds one keyring call. An unlock prompt that nobody can
// answer (an SSH session into a desktop) must not hang the command for ever.
var keyringTimeout = 10 * time.Second

// isUnavailable reports whether err means this machine has no usable keyring.
// On Linux every error except "not found" means so: an unreachable session
// bus, a missing Secret Service, a refused or timed-out prompt, dbus.SessionBus
// failures and raw D-Bus Error values (such as
// org.freedesktop.DBus.Error.ServiceUnknown) all mean the 0600 file is the
// right place.
func isUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return false
	}
	return true
}

// runUserRoot is the directory under which the per-user runtime directory
// (and its session-bus socket) lives. It is a variable so tests can point it
// at a temporary directory instead of the real /run/user.
var runUserRoot = "/run/user"

// keyringUsable reports whether the keyring should be tried at all. It is
// false when no session bus can be found without starting one: go-keyring
// would otherwise run dbus-launch, which starts a stray dbus-daemon that has
// no connection to the desktop session.
//
// This matches what the D-Bus library really does: a DBUS_SESSION_BUS_ADDRESS
// that names a bus directly is used as is. Without one, or with the value
// "autolaunch:", the library looks for the socket at <runUserRoot>/<uid>/bus
// and starts a bus only when there is none, which is the case we refuse.
// $XDG_RUNTIME_DIR/bus is not consulted, because the library does not look
// there.
func keyringUsable(getenv func(string) string) bool {
	if addr := strings.TrimSpace(getenv("DBUS_SESSION_BUS_ADDRESS")); addr != "" && !strings.HasPrefix(addr, "autolaunch:") {
		return true
	}
	if uid := os.Getuid(); uid >= 0 && fileExists(runUserRoot+"/"+strconv.Itoa(uid)+"/bus") {
		return true
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// guard runs one keyring call in a goroutine and gives up after
// keyringTimeout. A call that has not answered by then is reported as an
// unavailable keyring, so the file fallback is used instead of hanging.
func guard[T any](call func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := call()
		ch <- result{v, err}
	}()
	timer := time.NewTimer(keyringTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.val, r.err
	case <-timer.C:
		var zero T
		return zero, errKeyringUnavailable
	}
}
