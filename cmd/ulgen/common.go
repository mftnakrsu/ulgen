package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
	"github.com/mftnakrsu/ulgen/internal/termsafe"
	"github.com/mftnakrsu/ulgen/internal/version"
)

// evrenAccount is the credential store account that holds the EVREN key.
const evrenAccount = "evren"

// configDirVar is the environment variable that moves Ülgen's own
// configuration directory.
const configDirVar = "ULGEN_CONFIG_DIR"

// Seams the tests replace: the credential store, Ülgen's configuration
// directory as the operating system gives it, the clock and the zone.
var (
	openStore        = credential.Open
	defaultConfigDir = credential.DefaultDir
	now              = time.Now
	localZone        = func() *time.Location { return time.Local }
)

// configDirError says the configuration directory the operating system gave
// is not an absolute path (HOME, or AppData on Windows, holds a relative
// one).
type configDirError struct{ Dir string }

// Error names the directory.
func (e *configDirError) Error() string {
	return "the configuration directory " + e.Dir + " is not an absolute path"
}

// configDir returns Ülgen's configuration directory, where the fallback
// credentials file and the backups live. It is always an absolute path: a
// relative one would put the key's file and the backups wherever the command
// happens to run. A relative ULGEN_CONFIG_DIR is a *setup.RelativePathError
// that names the variable.
func configDir(getenv func(string) string) (string, error) {
	if err := checkConfigDirVar(getenv); err != nil {
		return "", err
	}
	dir, err := defaultConfigDir(getenv)
	if err != nil {
		if fallback, ok := xdgFallback(getenv); ok {
			return fallback, nil
		}
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", &configDirError{Dir: dir}
	}
	return dir, nil
}

// xdgFallback returns $HOME/.config/ulgen for the machine whose
// XDG_CONFIG_HOME is a relative path. The XDG rule is to treat a relative
// XDG_CONFIG_HOME as not set, so the directory is the one under the home
// directory; the operating system's own answer for it, on the system that
// reads the variable, is an error. Only a non-empty, relative
// XDG_CONFIG_HOME together with an absolute HOME qualifies: every other error
// stays the system's to report, and on a system that does not read
// XDG_CONFIG_HOME there is no error to fall back from.
func xdgFallback(getenv func(string) string) (string, bool) {
	xdg := getenv("XDG_CONFIG_HOME")
	home := getenv("HOME")
	if xdg == "" || filepath.IsAbs(xdg) || home == "" || !filepath.IsAbs(home) {
		return "", false
	}
	return filepath.Join(home, ".config", "ulgen"), true
}

// checkConfigDirVar refuses a ULGEN_CONFIG_DIR that holds a relative path.
func checkConfigDirVar(getenv func(string) string) error {
	if v := strings.TrimSpace(getenv(configDirVar)); v != "" && !filepath.IsAbs(v) {
		return &setup.RelativePathError{Var: configDirVar, Value: v}
	}
	return nil
}

// openKeyStore opens the credential store with the configuration directory
// already checked (see configDir).
func openKeyStore(getenv func(string) string) (credential.Store, error) {
	dir, err := configDir(getenv)
	if err != nil {
		return nil, err
	}
	return openStore(dir, getenv)
}

// describeEnvError words the errors about a directory that cannot be used
// because it is unknown or relative: the home directory, a variable that
// names a directory, Ülgen's configuration directory. ok is false for any
// other error.
func describeEnvError(err error, tr *i18n.Translator) (msg string, ok bool) {
	var (
		home     *setup.HomeError
		relative *setup.RelativePathError
		config   *configDirError
	)
	switch {
	case errors.As(err, &home):
		if home.Home == "" {
			return tr.T("error.home_unknown"), true
		}
		return tr.T("error.home_relative", safeArg(home.Home)), true
	case errors.As(err, &relative):
		return tr.T("error.relative_dir", relative.Var, safeArg(relative.Value)), true
	case errors.As(err, &config):
		return tr.T("error.config_dir_relative", safeArg(config.Dir)), true
	}
	return "", false
}

// describeStoreError words a failure to open or read the credential store.
// The store's own text is English and may name a file; it is put on one line
// inside a translated sentence.
func describeStoreError(err error, tr *i18n.Translator) string {
	return describeStoreErrorKey(err, "error.key_store", tr)
}

// describeStoreSaveError is the same for a failure to write the store: a
// store that cannot be opened for saving must not be reported with the
// message about reading the key.
func describeStoreSaveError(err error, tr *i18n.Translator) string {
	return describeStoreErrorKey(err, "error.key_store_save", tr)
}

// describeStoreErrorKey words a store failure under the given message key,
// after the errors about the configuration directory have had their turn.
func describeStoreErrorKey(err error, key string, tr *i18n.Translator) string {
	if msg, ok := describeEnvError(err, tr); ok {
		return msg
	}
	return tr.T(key, oneLine(err.Error()))
}

// keyLike matches a word that looks like an EVREN key: the prefix every key
// starts with and whatever follows it up to the next space.
var keyLike = regexp.MustCompile(regexp.QuoteMeta(evren.KeyPrefix) + `\S*`)

// safeArg makes a word from the command line or the environment safe to
// print back in a message: on one line, without escape sequences, and with
// anything that looks like an EVREN key replaced by "[key]". A key pasted in
// the wrong place must not come back on the screen or land in a log.
func safeArg(s string) string {
	return keyLike.ReplaceAllString(oneLine(s), "[key]")
}

// showPath is how every message shows a file path: with "~" when it lies
// under the home directory, without escape sequences, and, when it contains
// a space, whole and in double quotes so that it can be pasted into a shell
// ("~" inside quotes would not be expanded).
func showPath(home, path string) string {
	shown := path
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != "." && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			shown = "~" + string(filepath.Separator) + rel
		}
	}
	shown = oneLine(shown)
	if strings.IndexFunc(shown, unicode.IsSpace) >= 0 {
		shown = `"` + oneLine(path) + `"`
	}
	return shown
}

// errUnsafeBaseURL reports a --base-url that would send the key in clear
// text to another machine.
var errUnsafeBaseURL = errors.New("unsafe base url")

// userAgent identifies Ülgen to the API in the User-Agent header.
func userAgent() string { return "ulgen/" + version.Current().Version }

// checkBaseURL allows https anywhere, and http only to this machine. A
// value that is not valid UTF-8 or holds a control character is refused
// whatever it names: "ulgen setup" writes the address into other programs'
// configuration files.
func checkBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	if !utf8.ValidString(raw) || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return errUnsafeBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errUnsafeBaseURL
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return errUnsafeBaseURL
}

// baseURLNotice returns the note to print, on stderr, when --base-url sends
// the key to a host that is not this machine; it is empty when no note is due.
// The argument of the translated message is the host with its port when it has
// one.
func baseURLNotice(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	// Pointing at EVREN's own host is not "elsewhere": no note when the host
	// matches the EVREN preset's base URL.
	if preset, err := url.Parse(provider.EVREN.BaseURL); err == nil && strings.EqualFold(host, preset.Hostname()) {
		return ""
	}
	return u.Host
}

// describeEvrenError turns an error from an EVREN call into the message
// shown to the user. keyInvalid and termsMissing are the command-specific
// messages for 401 and the terms gate.
func describeEvrenError(err error, tr *i18n.Translator, keyInvalid, termsMissing string) string {
	var apiErr *provider.APIError
	switch {
	case errors.Is(err, context.Canceled):
		return tr.T("error.cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return tr.T("error.timeout", provider.EVREN.Name)
	case errors.As(err, &apiErr):
		switch {
		case apiErr.IsUnauthorized():
			return keyInvalid
		case apiErr.IsTermsNotAccepted():
			return termsMissing
		case apiErr.IsQuotaExceeded():
			return tr.T("error.quota", provider.EVREN.Name)
		case apiErr.IsBusy():
			return tr.T("error.busy", provider.EVREN.Name)
		}
		return tr.T("error.request", provider.EVREN.Name, oneLine(apiErr.Error()))
	}
	return tr.T("error.request", provider.EVREN.Name, oneLine(err.Error()))
}

// oneLine cleans a single field that came from the server and puts it on one
// line: after Sanitize, every run of carriage returns and newlines becomes one
// space. Without this a model id, a credit value or an error message could
// carry a line break and forge a whole line (for example a fake "EVREN:
// signed in ..." inside the model list). It is not used for the terms text,
// which is a document and keeps its line breaks.
func oneLine(s string) string {
	s = termsafe.Sanitize(s)
	var b strings.Builder
	b.Grow(len(s))
	inBreak := false
	for _, r := range s {
		if r == '\r' || r == '\n' {
			if !inBreak {
				b.WriteByte(' ')
				inBreak = true
			}
			continue
		}
		inBreak = false
		b.WriteRune(r)
	}
	return b.String()
}

// sourcePhrase says where the key came from, for the status line.
func sourcePhrase(tr *i18n.Translator, source string) string {
	switch source {
	case "env":
		return tr.T("status.source.env", provider.EVREN.KeyEnv)
	case "keyring":
		return tr.T("status.source.keyring")
	}
	return tr.T("status.source.file")
}

// subUsage renders a subcommand's usage with one line per option. A
// subcommand without options gets the plain header, with no "Options:" line.
func subUsage(tr *i18n.Translator, name string, fs *flag.FlagSet) string {
	rows := optionRows(tr, fs)
	if rows == "" {
		return tr.T("usage.sub.header_plain", name) + "\n"
	}
	return tr.T("usage.sub.header", name) + "\n" + rows
}

// optionRows renders the options of a flag set, one line each, with the
// descriptions starting in the same column.
func optionRows(tr *i18n.Translator, fs *flag.FlagSet) string {
	type row struct{ left, right string }
	var rows []row
	width := 0
	fs.VisitAll(func(f *flag.Flag) {
		kind, _ := flag.UnquoteUsage(f)
		left := "-" + f.Name
		switch kind {
		case "string":
			left += " " + tr.T("usage.value.string")
		case "int":
			left += " " + tr.T("usage.value.int")
		}
		if n := utf8.RuneCountInString(left); n > width {
			width = n
		}
		rows = append(rows, row{left, f.Usage})
	})
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, r.left, r.right)
	}
	return b.String()
}

// resolveKey finds the EVREN key: the environment variable wins when it is
// set, otherwise the keyring answers, otherwise the file. Every command that
// needs the key uses it, so the same key is used everywhere. When there is
// none it prints the reason and returns the exit code to end with.
func resolveKey(stderr io.Writer, getenv func(string) string, tr *i18n.Translator) (credential.Resolved, int) {
	store, err := openKeyStore(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeStoreError(err, tr))
		return credential.Resolved{}, exitError
	}
	cred, err := credential.Resolve(store, evrenAccount, provider.EVREN.KeyEnv, getenv)
	if errors.Is(err, credential.ErrNotFound) {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("status.not_signed_in"))
		return credential.Resolved{}, exitError
	}
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeStoreError(err, tr))
		return credential.Resolved{}, exitError
	}
	return cred, exitOK
}

// usageError prints a usage mistake of a subcommand: the message, then the
// subcommand's usage, on stderr. It returns the exit code for it.
func usageError(fs *flag.FlagSet, name, msg string, stderr io.Writer, tr *i18n.Translator) int {
	fmt.Fprintln(stderr, "ulgen: "+msg)
	io.WriteString(stderr, subUsage(tr, name, fs))
	return exitUsage
}

// parseSub parses a subcommand's options. done is true when the command
// ends here with code: after -h (usage on stdout, 0), and after a bad option
// or a stray argument (message and usage on stderr, 2).
func parseSub(fs *flag.FlagSet, name string, args []string, stdout, stderr io.Writer, tr *i18n.Translator) (code int, done bool) {
	err := fs.Parse(args)
	if err == nil {
		if rest := fs.Args(); len(rest) > 0 {
			return usageError(fs, name, tr.T("error.unknown_arguments", safeArg(strings.Join(rest, " "))), stderr, tr), true
		}
		return exitOK, false
	}
	if errors.Is(err, flag.ErrHelp) {
		io.WriteString(stdout, subUsage(tr, name, fs))
		return exitOK, true
	}
	return usageError(fs, name, translateFlagError(err, tr), stderr, tr), true
}

// groupDigits writes n with sep between thousands.
func groupDigits(n int64, sep string) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// formatDuration renders a positive duration as hours and minutes.
func formatDuration(tr *i18n.Translator, d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return tr.T("format.duration_m", m)
	}
	return tr.T("format.duration_hm", h, m)
}

var invalidValueRE = regexp.MustCompile(`^invalid (?:\w+ )?value "(.*)" for (?:flag )?(-\S+?):`)

// translateFlagError maps the flag package's English messages to translated ones.
func translateFlagError(err error, tr *i18n.Translator) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "flag provided but not defined: "); ok {
		return tr.T("error.unknown_option", safeArg(rest))
	}
	if rest, ok := strings.CutPrefix(msg, "flag needs an argument: "); ok {
		return tr.T("error.option_needs_value", safeArg(rest))
	}
	if m := invalidValueRE.FindStringSubmatch(msg); m != nil {
		return tr.T("error.invalid_value", safeArg(m[1]), safeArg(m[2]))
	}
	return safeArg(msg)
}
