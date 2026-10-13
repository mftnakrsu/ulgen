package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/evren"
)

// readerFunc adapts a function to io.Reader, so a test can be told when the
// command begins reading.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// useFakeBrowser replaces the browser opener and the terminal check. It
// returns a pointer to the URLs opened.
func useFakeBrowser(t *testing.T, fail error) *[]string {
	t.Helper()
	var opened []string
	savedOpen, savedTerm, savedSecret := openBrowser, stdinTerminal, readSecret
	openBrowser = func(u string) error {
		opened = append(opened, u)
		return fail
	}
	stdinTerminal = func() terminalKind { return notTerminal }
	readSecret = func() ([]byte, error) {
		t.Fatal("readSecret must not be used when stdin is not a console")
		return nil, nil
	}
	t.Cleanup(func() { openBrowser, stdinTerminal, readSecret = savedOpen, savedTerm, savedSecret })
	return &opened
}

func notAccepted() map[string]answer {
	routes := healthy()
	routes["/terms/status"] = answer{body: fxStatusNotAccepted}
	return routes
}

func TestLoginAcceptsTermsAndSaves(t *testing.T) {
	store := useFileStore(t)
	opened := useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, notAccepted())
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\ny\n", "login", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, w := range []string{
		evren.KeysURL,
		"Paste the API key: ",
		"EVREN asks you to accept its terms of use (version 1). The text follows.",
		"TERMS BODY LINE",
		"Do you accept the terms? [y/N]",
		"Terms version 1 accepted on 2026-10-11 21:08.",
		"The key is saved in " + store.(credential.Locator).Path() + " (readable only by you).",
		"EVREN: signed in (key from the credentials file)",
		"Daily tokens: 10,919,755 of 15,000,000 left (72%)",
		"deepseek-v4.1-flash",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
	if strings.Contains(stdout, testKey) || strings.Contains(stderr, testKey) {
		t.Error("output contains the key")
	}
	if got, err := store.Get("evren"); err != nil || got != testKey {
		t.Errorf("stored = %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(store.(credential.Locator).Path()); err != nil {
			t.Errorf("credentials file: %v", err)
		} else if info.Mode().Perm() != 0o600 {
			t.Errorf("credentials file mode = %v, want 0600", info.Mode().Perm())
		}
	}
	if len(*opened) != 1 || (*opened)[0] != evren.KeysURL {
		t.Errorf("browser opened %v", *opened)
	}
	paths := rec.paths()
	if strings.Join(paths, " ") != "/terms/status /terms/text /terms/accept /quota /models" {
		t.Errorf("requests = %v", paths)
	}
	reqs := rec.all()
	if reqs[2].Method != "POST" || reqs[2].Body != `{"version":1}` || reqs[0].Auth != "Bearer "+testKey {
		t.Errorf("accept request = %+v", reqs[2])
	}
}

func TestLoginTermsAlreadyAccepted(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, healthy())
	code, stdout, _ := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || strings.Contains(stdout, "TERMS BODY LINE") || strings.Contains(stdout, "Do you accept") {
		t.Errorf("code %d stdout %q", code, stdout)
	}
	if strings.Join(rec.paths(), " ") != "/terms/status /quota /models" {
		t.Errorf("requests = %v", rec.paths())
	}
	if got, _ := store.Get("evren"); got != testKey {
		t.Error("key not stored")
	}
}

func TestLoginTermsGateOnStatusEndpoint(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	routes := healthy()
	routes["/terms/status"] = answer{status: 403, body: fxTermsGate}
	srv, rec := evrenServer(t, routes)
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\nyes\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "TERMS BODY LINE") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if reqs := rec.all(); len(reqs) < 3 || reqs[2].Body != `{"version":1}` {
		t.Errorf("requests = %+v", reqs)
	}
	if got, _ := store.Get("evren"); got != testKey {
		t.Error("key not stored")
	}
}

func TestLoginDeclinedStoresNothing(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, notAccepted())
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\nn\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.HasPrefix(stderr, "ulgen: the terms were not accepted; the key was not saved\n") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("stored anyway: %v", err)
	}
	if strings.Join(rec.paths(), " ") != "/terms/status /terms/text" {
		t.Errorf("requests = %v", rec.paths())
	}
}

func TestLoginRejectedKey(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, map[string]answer{"/terms/status": {status: 401, body: fxUnauthorized}})
	code, _, stderr := execIn(t, context.Background(), nil, "evren_llm_wrong\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.HasPrefix(stderr, "ulgen: EVREN did not accept this key; copy it again from "+evren.KeysURL+"\n") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Error("rejected key was stored")
	}
	if strings.Contains(stderr, "evren_llm_wrong") {
		t.Error("stderr contains the key")
	}
}

func TestLoginEmptyKey(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, healthy())
	code, _, stderr := execIn(t, context.Background(), nil, "\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.HasPrefix(stderr, "ulgen: no key entered\n") || len(rec.all()) != 0 {
		t.Errorf("code %d stderr %q requests %v", code, stderr, rec.all())
	}
}

func TestLoginWarnsAboutOddKeyButContinues(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())
	code, _, stderr := execIn(t, context.Background(), nil, "sk-something\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stderr, "warning: EVREN keys start with evren_llm_; this one does not") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestLoginNoBrowserAndBrowserFailure(t *testing.T) {
	useFileStore(t)
	opened := useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || len(*opened) != 0 || !strings.Contains(stdout, evren.KeysURL) {
		t.Errorf("no-browser: code %d opened %v stdout %q", code, *opened, stdout)
	}

	useFileStore(t)
	opened = useFakeBrowser(t, errors.New("no display"))
	code, _, stderr := execIn(t, context.Background(), nil, testKey+"\n", "login", "--base-url", srv.URL)
	if code != 0 || len(*opened) != 1 || !strings.Contains(stderr, "the browser could not be opened; open the address above yourself") {
		t.Errorf("browser failure: code %d opened %v stderr %q", code, *opened, stderr)
	}
}

func TestLoginEnvironmentNotice(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())
	code, _, stderr := execIn(t, context.Background(), map[string]string{"EVREN_API_KEY": "evren_llm_env"}, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stderr, "note: EVREN_API_KEY is set in the environment and overrides the saved key as long as it is set") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestLoginTurkish(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, notAccepted())
	code, stdout, stderr := execIn(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, testKey+"\nevet\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, w := range []string{
		"API anahtarını yapıştırın: ",
		"Koşulları kabul ediyor musunuz? [e/H]",
		"Koşulların 1. sürümü 2026-10-11 21:08 tarihinde kabul edildi.",
		"EVREN: giriş yapılmış (anahtar kimlik bilgisi dosyasından)",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if got, _ := store.Get("evren"); got != testKey {
		t.Error("key not stored")
	}
}

func TestLoginTermsVersionChanged(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	routes := notAccepted()
	routes["/terms/accept"] = answer{status: 400, body: `{"error":{"message":"geçersiz versiyon - mevcut yayınlanan sürüm: 2","type":"invalid_request_error","code":"terms_version_mismatch"}}`}
	srv, _ := evrenServer(t, routes)
	code, _, stderr := execIn(t, context.Background(), nil, testKey+"\ny\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.HasPrefix(stderr, "ulgen: EVREN published a new version of the terms meanwhile; run ulgen login again\n") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
}

func TestLoginUsage(t *testing.T) {
	useFileStore(t)
	code, stdout, _ := exec(t, context.Background(), nil, "login", "-h")
	if code != 0 || !strings.HasPrefix(stdout, "Usage:\n  ulgen login [options]") || !strings.Contains(stdout, "-no-browser") {
		t.Errorf("code %d stdout %q", code, stdout)
	}
	code, _, stderr := exec(t, context.Background(), nil, "login", "--base-url", "http://example.com")
	if code != exitUsage || !strings.Contains(stderr, "--base-url must use https") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestLogout(t *testing.T) {
	store := useFileStore(t)
	if err := store.Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := exec(t, context.Background(), nil, "logout")
	if code != 0 || stdout != "The saved key was removed.\n" || stderr != "" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("still stored: %v", err)
	}
	code, stdout, stderr = exec(t, context.Background(), map[string]string{"EVREN_API_KEY": "x"}, "logout")
	if code != 0 || stdout != "No key was saved.\n" || !strings.Contains(stderr, "note: EVREN_API_KEY is still set in the environment") {
		t.Errorf("second: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "logout")
	if code != 0 || stdout != "Kayıtlı anahtar yoktu.\n" {
		t.Errorf("Turkish: code %d stdout %q", code, stdout)
	}
	code, _, stderr = exec(t, context.Background(), nil, "logout", "extra")
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: unknown arguments: extra\n") {
		t.Errorf("extra: code %d stderr %q", code, stderr)
	}
}

// TestLogoutUsage covers a subcommand with no options: -h prints the usage
// line alone, with no empty "Options:" heading.
func TestLogoutUsage(t *testing.T) {
	useFileStore(t)
	code, stdout, stderr := exec(t, context.Background(), nil, "logout", "-h")
	if code != 0 || stdout != "Usage:\n  ulgen logout\n" || stderr != "" {
		t.Errorf("English: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Options:") {
		t.Errorf("empty Options: heading printed:\n%s", stdout)
	}
	code, stdout, _ = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "logout", "-h")
	if code != 0 || stdout != "Kullanım:\n  ulgen logout\n" {
		t.Errorf("Turkish: code %d stdout %q", code, stdout)
	}
}

func TestAcceptsTerms(t *testing.T) {
	for _, yes := range []string{"y", "Y", "yes", "YES", "e", "E", "evet", "Evet", " y "} {
		if !acceptsTerms(yes) {
			t.Errorf("%q should accept", yes)
		}
	}
	for _, no := range []string{"", "n", "no", "h", "hayır", "maybe", "yesterday"} {
		if acceptsTerms(no) {
			t.Errorf("%q should not accept", no)
		}
	}
}

// useFakeTerminalSave replaces the terminal-save seam and returns a flag that
// the restore function sets.
func useFakeTerminalSave(t *testing.T) *atomic.Bool {
	t.Helper()
	var restored atomic.Bool
	saved := saveTerminal
	saveTerminal = func() func() { return func() { restored.Store(true) } }
	t.Cleanup(func() { saveTerminal = saved })
	return &restored
}

func TestLoginInterruptedAtKeyPrompt(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	restored := useFakeTerminalSave(t)
	srv, rec := evrenServer(t, healthy())

	stdinTerminal = func() terminalKind { return consoleTerminal }
	called := make(chan struct{})
	release := make(chan struct{})
	savedRead := readSecret
	readSecret = func() ([]byte, error) {
		close(called)
		<-release
		return nil, errors.New("the abandoned read must not return a value")
	}
	t.Cleanup(func() { readSecret = savedRead; close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-called
		cancel()
	}()

	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stdout, stderr := execIn(t, ctx, nil, "", "login", "--no-browser", "--base-url", srv.URL)
		done <- result{code, stdout, stderr}
	}()
	select {
	case r := <-done:
		if r.code != exitInterrupted || !strings.Contains(r.stderr, "ulgen: cancelled") {
			t.Errorf("code %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
		}
		if !strings.HasSuffix(r.stdout, "Paste the API key (it stays hidden): \n") {
			t.Errorf("stdout = %q, want the prompt followed by exactly one newline", r.stdout)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not return after the context was cancelled")
	}
	if !restored.Load() {
		t.Error("the terminal was not restored after the interrupted read")
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
	if len(rec.all()) != 0 {
		t.Errorf("requests = %v", rec.all())
	}
}

// cancelOnSetStore is the file store plus a Set that cancels the command's
// context, standing in for a Ctrl+C that arrives while the key is written.
type cancelOnSetStore struct {
	credential.Store
	cancel context.CancelFunc
}

func (s *cancelOnSetStore) Set(account, secret string) error {
	if err := s.Store.Set(account, secret); err != nil {
		return err
	}
	s.cancel()
	return nil
}

// TestLoginCancelledWhileStoring covers a Ctrl+C that lands while the key is
// being written when nothing was stored before: the command ends 130 and the
// account is empty afterwards.
func TestLoginCancelledWhileStoring(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := openStore
	openStore = func(string, func(string) string) (credential.Store, error) {
		return &cancelOnSetStore{Store: store, cancel: cancel}, nil
	}
	t.Cleanup(func() { openStore = saved })

	code, _, stderr := execIn(t, ctx, nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
}

// TestLoginCancelledWhileStoringKeepsEarlierKey covers a cancelled re-login:
// a key from an earlier sign-in must still be there afterwards, not deleted.
func TestLoginCancelledWhileStoringKeepsEarlierKey(t *testing.T) {
	store := useFileStore(t)
	const earlier = "evren_llm_earlier_key"
	if err := store.Set("evren", earlier); err != nil {
		t.Fatal(err)
	}
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := openStore
	openStore = func(string, func(string) string) (credential.Store, error) {
		return &cancelOnSetStore{Store: store, cancel: cancel}, nil
	}
	t.Cleanup(func() { openStore = saved })

	code, _, stderr := execIn(t, ctx, nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if got, err := store.Get("evren"); err != nil || got != earlier {
		t.Errorf("after a cancelled re-login the earlier key = %q, %v; want %q", got, err, earlier)
	}
}

// recordingStore records the calls it receives, so a test can prove that Set
// was never reached.
type recordingStore struct {
	credential.Store
	mu   sync.Mutex
	sets int
}

func (s *recordingStore) Set(account, secret string) error {
	s.mu.Lock()
	s.sets++
	s.mu.Unlock()
	return s.Store.Set(account, secret)
}

func (s *recordingStore) setCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets
}

// TestLoginCancelledInsideOpenStore covers the check between opening the store
// and writing to it: openStore cancels the context and returns a store that
// records calls, so Set must never run.
func TestLoginCancelledInsideOpenStore(t *testing.T) {
	file := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, _ := evrenServer(t, healthy())
	rec := &recordingStore{Store: file}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := openStore
	openStore = func(string, func(string) string) (credential.Store, error) {
		cancel()
		return rec, nil
	}
	t.Cleanup(func() { openStore = saved })

	code, _, stderr := execIn(t, ctx, nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if rec.setCalls() != 0 {
		t.Errorf("Set was called %d times, want 0", rec.setCalls())
	}
	if _, err := file.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
}

// TestLoginCancelledBeforeStoring covers a Ctrl+C that arrives after the
// terms were accepted but before the key is written: nothing is stored.
func TestLoginCancelledBeforeStoring(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := http.NewServeMux()
	for path, a := range notAccepted() {
		a, path := a, path
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if a.status != 0 {
				w.WriteHeader(a.status)
			}
			io.WriteString(w, a.body)
			if path == "/terms/accept" {
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				cancel() // the answer is on the wire; the cancellation lands now
			}
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	code, _, stderr := execIn(t, ctx, nil, testKey+"\ny\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
}

// TestLoginTerminalPromptText covers the terminal path: stdin is a terminal,
// so the hidden read is used and the prompt says the key stays hidden. The
// plain pipe prompt must not appear.
func TestLoginTerminalPromptText(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	useFakeTerminalSave(t)
	srv, _ := evrenServer(t, healthy())

	stdinTerminal = func() terminalKind { return consoleTerminal }
	savedRead := readSecret
	readSecret = func() ([]byte, error) { return []byte(testKey), nil }
	t.Cleanup(func() { readSecret = savedRead })

	code, stdout, stderr := execIn(t, context.Background(), nil, "", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Paste the API key (it stays hidden): ") {
		t.Errorf("stdout lacks the hidden prompt:\n%s", stdout)
	}
	if strings.Contains(stdout, "Paste the API key: ") {
		t.Errorf("stdout used the plain pipe prompt:\n%s", stdout)
	}
	if got, _ := store.Get("evren"); got != testKey {
		t.Error("key not stored")
	}
}

// TestLoginReportFailureIsNotAFailedSignIn covers the closing status report
// failing after the key was saved: the key stays, the error is a note on
// stderr and the exit code is 0.
func TestLoginReportFailureIsNotAFailedSignIn(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	routes := healthy()
	routes["/quota"] = answer{status: 503, body: `{"error":{"message":"busy"}}`}
	srv, _ := evrenServer(t, routes)
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitOK {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "the key is saved, but the status report could not be fetched: EVREN is busy") {
		t.Errorf("stderr = %q", stderr)
	}
	if got, err := store.Get("evren"); err != nil || got != testKey {
		t.Errorf("the key was not kept: %q %v", got, err)
	}
	if strings.Contains(stdout, "EVREN: signed in") {
		t.Errorf("stdout printed a report that failed:\n%s", stdout)
	}
}

func TestLoginInterruptedDuringRequest(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	code, _, stderr := execIn(t, ctx, nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
}

func TestInterruptible(t *testing.T) {
	// The read finishes first.
	got, err := interruptible(context.Background(), func() (string, error) { return "value", nil }, nil)
	if got != "value" || err != nil {
		t.Errorf("got %q, %v; want value, nil", got, err)
	}

	// The context ends first: the read's value is dropped and onCancel runs.
	started := make(chan struct{})
	block := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var oncalled atomic.Bool
	go func() {
		<-started
		cancel()
	}()
	got, err = interruptible(ctx, func() (string, error) {
		close(started)
		<-block
		return "later", nil
	}, func() { oncalled.Store(true) })
	close(block)
	if got != "" || !errors.Is(err, context.Canceled) || !oncalled.Load() {
		t.Errorf("got %q, %v, onCancel %v; want empty, Canceled, true", got, err, oncalled.Load())
	}

	// The context is already done: the read must not start, onCancel still runs.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	var started2, oncalled2 atomic.Bool
	got, err = interruptible(ctx2, func() (string, error) { started2.Store(true); return "x", nil }, func() { oncalled2.Store(true) })
	if got != "" || !errors.Is(err, context.Canceled) || started2.Load() {
		t.Errorf("got %q, %v, started %v; want empty, Canceled, false", got, err, started2.Load())
	}
	if !oncalled2.Load() {
		t.Error("onCancel must run when the context is already done")
	}
}

// TestInterruptiblePrefersCancelToAReadError covers the race on a Windows
// console: a read that returns end-of-input at the same instant as Ctrl+C
// must still end as a cancellation, not as a plain read error.
func TestInterruptiblePrefersCancelToAReadError(t *testing.T) {
	saved := interruptGrace
	t.Cleanup(func() { interruptGrace = saved })

	// The read fails at once and the cancellation arrives just after: the
	// cancellation wins. A wide grace makes the race independent of the clock.
	interruptGrace = 2 * time.Second
	read := func() (string, error) { return "", io.EOF }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var oncalled atomic.Bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	got, err := interruptible(ctx, read, func() { oncalled.Store(true) })
	if got != "" || !errors.Is(err, context.Canceled) || !oncalled.Load() {
		t.Errorf("got %q, %v, onCancel %v; want empty, Canceled, true", got, err, oncalled.Load())
	}

	// A context that is never cancelled: the read's own error is returned
	// after the grace period. A short grace keeps the test quick.
	interruptGrace = 20 * time.Millisecond
	start := time.Now()
	got, err = interruptible(context.Background(), read, nil)
	if got != "" || !errors.Is(err, io.EOF) {
		t.Errorf("got %q, %v; want empty, EOF", got, err)
	}
	if elapsed := time.Since(start); elapsed < interruptGrace {
		t.Errorf("returned after %v, want at least the grace period %v", elapsed, interruptGrace)
	}
}

// TestLoginInterruptedReadReturnsEOF covers one Ctrl+C at the hidden prompt
// when the pending read returns end-of-input before the cancellation is seen
// (a Windows console): login must still end with the interrupt code.
func TestLoginInterruptedReadReturnsEOF(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, healthy())

	// A wide grace so a slow machine cannot miss the cancellation race.
	savedGrace := interruptGrace
	interruptGrace = 2 * time.Second
	t.Cleanup(func() { interruptGrace = savedGrace })

	stdinTerminal = func() terminalKind { return consoleTerminal }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	savedRead := readSecret
	readSecret = func() ([]byte, error) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		return nil, io.EOF
	}
	t.Cleanup(func() { readSecret = savedRead })

	code, _, stderr := execIn(t, ctx, nil, "", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
	if len(rec.all()) != 0 {
		t.Errorf("requests = %v", rec.all())
	}
}

// TestLoginEndOfInputFromTheHiddenReadIsNoKey covers the hidden read
// returning end-of-input: that is an empty key, exactly as on the pipe path.
// On Unix term.ReadPassword keeps waiting on Ctrl+D, so this reaches the
// library only on Windows consoles and in tests like this one.
func TestLoginEndOfInputFromTheHiddenReadIsNoKey(t *testing.T) {
	useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, healthy())

	stdinTerminal = func() terminalKind { return consoleTerminal }
	savedRead := readSecret
	readSecret = func() ([]byte, error) { return nil, io.EOF }
	t.Cleanup(func() { readSecret = savedRead })

	code, _, stderr := execIn(t, context.Background(), nil, "", "login", "--no-browser", "--base-url", srv.URL)
	if code != exitError || !strings.HasPrefix(stderr, "ulgen: no key entered\n") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if len(rec.all()) != 0 {
		t.Errorf("requests = %v", rec.all())
	}
}

// promptWatcher is a writer that signals once the text it watches for has
// been written. It lets a test cancel exactly when a prompt reaches stdout,
// not when some earlier request made it.
type promptWatcher struct {
	mu     sync.Mutex
	buf    strings.Builder
	needle string
	once   sync.Once
	signal chan struct{}
}

func newPromptWatcher(needle string) *promptWatcher {
	return &promptWatcher{needle: needle, signal: make(chan struct{})}
}

func (w *promptWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	if strings.Contains(w.buf.String(), w.needle) {
		w.once.Do(func() { close(w.signal) })
	}
	w.mu.Unlock()
	return len(p), nil
}

func (w *promptWatcher) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestLoginInterruptedAtTermsPrompt covers Ctrl+C at the terms prompt when
// stdin is a pipe: the key line arrives, then the pipe never ends, and the
// cancellation lands only once the accept prompt is on stdout.
func TestLoginInterruptedAtTermsPrompt(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, notAccepted())

	pr, pw := io.Pipe()
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readStarted := make(chan struct{})
	stdin := readerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(readStarted) })
		return pr.Read(p)
	})

	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		select {
		case <-readStarted:
		case <-stop: // the command never read stdin
			return
		}
		// The key line is waiting; the pipe has no buffer, so this write
		// blocks until the command reads the bytes, which it does before the
		// terms prompt. The pipe stays open afterwards, so the accept read
		// blocks until the cancellation ends it. Closing the write end in
		// cleanup makes a blocked write return with an error, and stop ends
		// the wait for a read that never came, so this goroutine ends on
		// every path.
		io.WriteString(pw, testKey+"\n")
	}()
	t.Cleanup(func() { close(stop); pw.Close(); pr.Close(); <-writerDone })

	// The cancellation waits for the accept prompt, so it cannot land while
	// the terms text is still being fetched.
	out := newPromptWatcher("Do you accept the terms?")
	var errBuf bytes.Buffer
	canceled := make(chan struct{})
	go func() {
		defer close(canceled)
		select {
		case <-out.signal:
			cancel()
		case <-ctx.Done():
		}
	}()

	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code := run(ctx, []string{"login", "--no-browser", "--base-url", srv.URL}, stdin, out, &errBuf, func(string) string { return "" })
		done <- result{code, out.String(), errBuf.String()}
	}()
	select {
	case r := <-done:
		if r.code != exitInterrupted || !strings.Contains(r.stderr, "ulgen: cancelled") {
			t.Errorf("code %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not return after the context was cancelled")
	}
	cancel()   // ends the goroutine below also when the prompt never came
	<-canceled // the cancellation goroutine has ended
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
	if paths := rec.paths(); strings.Contains(strings.Join(paths, " "), "/terms/accept") {
		t.Errorf("terms were accepted: requests = %v", paths)
	}
}

// TestLoginInterruptedAtKeyPromptPipe covers Ctrl+C at the key prompt when
// stdin is a pipe that never delivers a line: nothing is stored and no
// request is made.
func TestLoginInterruptedAtKeyPromptPipe(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	srv, rec := evrenServer(t, healthy())

	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close(); pr.Close() })
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readStarted, readRelease := make(chan struct{}), make(chan struct{})
	stdin := readerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(readStarted) })
		<-readRelease
		return pr.Read(p)
	})
	t.Cleanup(func() { close(readRelease) })

	go func() {
		<-readStarted
		cancel()
	}()

	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		var out, errOut bytes.Buffer
		code := run(ctx, []string{"login", "--no-browser", "--base-url", srv.URL}, stdin, &out, &errOut, func(string) string { return "" })
		done <- result{code, out.String(), errOut.String()}
	}()
	select {
	case r := <-done:
		if r.code != exitInterrupted || !strings.Contains(r.stderr, "ulgen: cancelled") {
			t.Errorf("code %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not return after the context was cancelled")
	}
	if _, err := store.Get("evren"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key was stored: %v", err)
	}
	if len(rec.all()) != 0 {
		t.Errorf("requests = %v", rec.all())
	}
}
