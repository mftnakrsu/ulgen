package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/termsafe"
)

// Seams the tests replace: what kind of terminal stdin is (see terminalOf),
// and the hidden read.
var (
	stdinTerminal = func() terminalKind { return terminalOf(os.Stdin) }
	readSecret    = func() ([]byte, error) { return term.ReadPassword(os.Stdin.Fd()) }
)

// saveTerminal remembers the terminal's settings and returns a function that
// puts them back: the hidden read switches echo off, and an interrupted read
// would leave it off.
var saveTerminal = func() (restore func()) {
	fd := os.Stdin.Fd()
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() { _ = term.Restore(fd, state) }
}

// interruptGrace is how long a read that already failed waits for a
// cancellation that may be arriving at the same instant. On a Windows console
// a Ctrl+C makes the pending read return end-of-input before the cancellation
// is seen, and elsewhere a read that fails at the same moment as the
// cancellation may win the select; without this wait either case would end
// the program with a plain error instead of the interrupt exit code. It is a
// variable so tests can widen it (so a slow machine cannot miss the race) or
// shrink it (so the case that waits it out does not sleep).
var interruptGrace = 100 * time.Millisecond

// interruptible runs a blocking read and returns ctx.Err() as soon as ctx
// ends; onCancel, when not nil, runs first in that case. A read that returns
// with an error is given interruptGrace to let a cancellation that arrives in
// the same instant be preferred over the read's own error. A read that
// returns without an error is returned at once. A read that is abandoned this
// way ends with the process.
func interruptible[T any](ctx context.Context, read func() (T, error), onCancel func()) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		if onCancel != nil {
			onCancel()
		}
		return zero, err
	}
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := read()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		if r.err == nil {
			return r.val, nil
		}
		timer := time.NewTimer(interruptGrace)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			if onCancel != nil {
				onCancel()
			}
			return zero, ctx.Err()
		case <-timer.C:
			return r.val, r.err
		}
	case <-ctx.Done():
		if onCancel != nil {
			onCancel()
		}
		return zero, ctx.Err()
	}
}

// runLogin signs in to EVREN: "ulgen login [options]".
func runLogin(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noBrowser := fs.Bool("no-browser", false, tr.T("flag.no_browser"))
	baseURL := fs.String("base-url", "", tr.T("flag.evren_base_url"))
	if code, done := parseSub(fs, "login", args, stdout, stderr, tr); done {
		return code
	}
	if err := checkBaseURL(*baseURL); err != nil {
		return usageError(fs, "login", tr.T("error.base_url_unsafe"), stderr, tr)
	}
	if notice := baseURLNotice(*baseURL); notice != "" {
		fmt.Fprintln(stderr, tr.T("notice.base_url", notice))
	}
	report := func(msg string) int {
		fmt.Fprintln(stderr, "ulgen: "+msg)
		return exitError
	}
	// failEvren reports an EVREN call that failed, and turns a cancellation
	// into the interrupt exit code so one Ctrl+C ends the program cleanly.
	failEvren := func(err error, keyInvalid string) int {
		fmt.Fprintln(stderr, "ulgen: "+describeEvrenError(err, tr, keyInvalid, tr.T("status.terms_missing")))
		if errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		return exitError
	}
	// cancelled ends an input read that Ctrl+C interrupted. The cursor sits
	// after the prompt, so an empty line puts the message on its own line.
	cancelled := func() int {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stderr, "ulgen: "+tr.T("error.cancelled"))
		return exitInterrupted
	}

	// 1. The environment variable would hide what we are about to save.
	if strings.TrimSpace(getenv(provider.EVREN.KeyEnv)) != "" {
		fmt.Fprintln(stderr, tr.T("login.env_notice", provider.EVREN.KeyEnv))
	}

	// 2. The keys page.
	fmt.Fprintln(stdout, tr.T("login.open_page", evren.KeysURL))
	if !*noBrowser {
		if err := openBrowser(evren.KeysURL); err != nil {
			fmt.Fprintln(stderr, tr.T("login.browser_failed"))
		}
	}

	// 3. The key, hidden when a person types it.
	in := bufio.NewReader(stdin)
	key, err := promptKey(ctx, in, stdout, tr)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return cancelled()
		}
		return report(err.Error())
	}
	if key == "" {
		return report(tr.T("login.no_key"))
	}
	if !strings.HasPrefix(key, evren.KeyPrefix) {
		fmt.Fprintln(stderr, tr.T("login.odd_key", evren.KeyPrefix))
	}

	// 4. Validate the key; learn whether the terms are accepted.
	client := evren.New(key, *baseURL, userAgent())
	keyRejected := tr.T("login.key_rejected", evren.KeysURL)
	accepted, version := false, 0
	st, err := client.TermsStatus(ctx)
	if err != nil {
		var apiErr *provider.APIError
		switch {
		case errors.As(err, &apiErr) && apiErr.IsTermsNotAccepted():
			version, _ = strconv.Atoi(apiErr.TermsVersion)
		default:
			return failEvren(err, keyRejected)
		}
	} else {
		accepted, version = st.Accepted, st.CurrentVersion
	}

	// 5. The terms, shown in full and accepted only on a yes.
	if !accepted {
		if code, ok := confirmTerms(ctx, client, in, stdout, stderr, version, tr); !ok {
			return code
		}
	}

	// 6. Store the key. A Ctrl+C that arrived while the key was pending ends
	// the command before anything is written; one that lands while the store
	// is working undoes the write.
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("error.cancelled"))
		return exitInterrupted
	}
	store, err := openKeyStore(getenv)
	if err != nil {
		return report(describeStoreSaveError(err, tr))
	}
	// The store step itself may take a moment; a cancellation that arrives
	// while it opens must not write anything.
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("error.cancelled"))
		return exitInterrupted
	}
	// Read what is already stored, so a cancelled re-login can put the earlier
	// key back. "Not found" means nothing was there; any other error also
	// counts as nothing, so a failure to read never blocks signing in.
	previous, err := store.Get(evrenAccount)
	hadPrevious := err == nil
	if err := store.Set(evrenAccount, key); err != nil {
		return report(describeStoreSaveError(err, tr))
	}
	if ctx.Err() != nil {
		if hadPrevious {
			_ = store.Set(evrenAccount, previous)
		} else {
			_ = store.Delete(evrenAccount)
		}
		fmt.Fprintln(stderr, "ulgen: "+tr.T("error.cancelled"))
		return exitInterrupted
	}
	source := "file"
	if d, ok := store.(credential.Describer); ok && d.Describe() != "" {
		source = d.Describe()
	}
	if source == "keyring" {
		fmt.Fprintln(stdout, tr.T("login.saved_keyring"))
	} else {
		path := ""
		if l, ok := store.(credential.Locator); ok {
			path = l.Path()
		}
		fmt.Fprintln(stdout, tr.T("login.saved_file", path))
	}

	// 7. The report. The key is already saved, so a failed report is a note,
	// not a failed sign-in; a cancellation still ends with the interrupt code.
	rep, err := fetchReport(ctx, client)
	if err != nil {
		msg := describeEvrenError(err, tr, tr.T("status.key_invalid"), tr.T("status.terms_missing"))
		fmt.Fprintln(stderr, tr.T("login.report_failed", msg))
		if errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		return exitOK
	}
	fmt.Fprintln(stdout)
	writeStatusText(stdout, rep, source, tr)
	return exitOK
}

// confirmTerms shows the terms and records the user's yes. ok is false when
// login must end with code: declined, cancelled, or a failed request.
func confirmTerms(ctx context.Context, client *evren.Client, in *bufio.Reader, stdout, stderr io.Writer, version int, tr *i18n.Translator) (code int, ok bool) {
	keyRejected := tr.T("login.key_rejected", evren.KeysURL)
	// report ends login with a plain error, and failEvren with an EVREN call
	// that failed: a cancellation becomes the interrupt exit code.
	report := func(msg string) int {
		fmt.Fprintln(stderr, "ulgen: "+msg)
		return exitError
	}
	failEvren := func(err error) int {
		fmt.Fprintln(stderr, "ulgen: "+describeEvrenError(err, tr, keyRejected, tr.T("status.terms_missing")))
		if errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		return exitError
	}
	terms, err := client.TermsText(ctx)
	if err != nil {
		return failEvren(err), false
	}
	if terms.Version != 0 {
		version = terms.Version
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, tr.T("login.terms_intro", version))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, termsafe.Sanitize(terms.Content))
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, tr.T("login.accept_prompt"))
	answer, err := interruptible(ctx, func() (string, error) { return readLine(in) }, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// The cursor sits after the prompt; an empty line puts the
			// cancellation message on its own line.
			fmt.Fprintln(stdout)
			fmt.Fprintln(stderr, "ulgen: "+tr.T("error.cancelled"))
			return exitInterrupted, false
		}
		if !errors.Is(err, io.EOF) {
			return report(err.Error()), false
		}
	}
	if !acceptsTerms(answer) {
		return report(tr.T("login.declined")), false
	}
	acc, err := client.AcceptTerms(ctx, version)
	if err != nil {
		var apiErr *provider.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "terms_version_mismatch" {
			return report(tr.T("login.terms_changed")), false
		}
		return failEvren(err), false
	}
	fmt.Fprintln(stdout, tr.T("login.accepted", acc.Version, acc.AcceptedAt.In(localZone()).Format("2006-01-02 15:04")))
	return exitOK, true
}

// promptKey asks for the key. A console gets a hidden read. Anything else
// gives its first line: a pipe or a test, and also the window of Git Bash,
// where a Windows program cannot switch the echo off, so the prompt says
// that the key will be visible instead of promising to hide it.
func promptKey(ctx context.Context, in *bufio.Reader, stdout io.Writer, tr *i18n.Translator) (string, error) {
	kind := stdinTerminal()
	switch kind {
	case consoleTerminal:
		fmt.Fprint(stdout, tr.T("login.key_prompt"))
	case ptyPipeTerminal:
		fmt.Fprint(stdout, tr.T("login.key_prompt_visible"))
	default:
		// Nothing is hidden when stdin is a pipe, so the prompt must not
		// promise it is.
		fmt.Fprint(stdout, tr.T("login.key_prompt_plain"))
	}
	if kind == consoleTerminal {
		restore := saveTerminal()
		b, err := interruptible(ctx, readSecret, restore)
		if err != nil {
			// End of input at the hidden prompt is an empty key, exactly as one
			// on the pipe path. On Unix term.ReadPassword keeps waiting on
			// Ctrl+D, so this branch is reached on Windows consoles and in
			// tests only. On a cancellation the newline comes from the cancel
			// path, so none is printed here.
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(stdout)
				return "", nil
			}
			return "", err
		}
		fmt.Fprintln(stdout) // the Enter was not echoed
		return strings.TrimSpace(string(b)), nil
	}
	line, err := interruptible(ctx, func() (string, error) { return readLine(in) }, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	fmt.Fprintln(stdout)
	return strings.TrimSpace(line), nil
}

// readLine returns the next line without its line ending.
func readLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// acceptsTerms recognises a yes in English or Turkish.
func acceptsTerms(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "e", "evet":
		return true
	}
	return false
}

// runLogout removes the saved key: "ulgen logout".
func runLogout(args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen logout", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if code, done := parseSub(fs, "logout", args, stdout, stderr, tr); done {
		return code
	}
	store, err := openKeyStore(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeStoreError(err, tr))
		return exitError
	}
	switch err := store.Delete(evrenAccount); {
	case err == nil:
		fmt.Fprintln(stdout, tr.T("logout.removed"))
	case errors.Is(err, credential.ErrNotFound):
		fmt.Fprintln(stdout, tr.T("logout.nothing"))
	default:
		fmt.Fprintln(stderr, "ulgen: "+err.Error())
		return exitError
	}
	if strings.TrimSpace(getenv(provider.EVREN.KeyEnv)) != "" {
		fmt.Fprintln(stderr, tr.T("logout.env_notice", provider.EVREN.KeyEnv))
	}
	return exitOK
}
