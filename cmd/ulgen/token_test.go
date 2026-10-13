package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/credential"
)

func useTerminalStdout(t *testing.T, terminal bool) {
	t.Helper()
	saved := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { stdoutIsTerminal = saved })
}

func TestTokenPrintsTheKeyForPrograms(t *testing.T) {
	useFileStore(t).Set("evren", testKey)
	useTerminalStdout(t, false)
	code, stdout, stderr := exec(t, context.Background(), nil, "token")
	if code != 0 || stdout != testKey+"\n" || stderr != "" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	// The environment variable wins, as everywhere.
	code, stdout, _ = exec(t, context.Background(), map[string]string{"EVREN_API_KEY": "evren_llm_fromenv"}, "token")
	if code != 0 || stdout != "evren_llm_fromenv\n" {
		t.Errorf("environment: code %d stdout %q", code, stdout)
	}
}

func TestTokenDoesNotPrintOnAScreen(t *testing.T) {
	useFileStore(t).Set("evren", testKey)
	useTerminalStdout(t, true)
	code, stdout, stderr := exec(t, context.Background(), nil, "token")
	if code != 2 || stdout != "" || stderr != "ulgen: this would print your API key on the screen; add --show if you mean it\n" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "token")
	if code != 2 || stdout != "" || stderr != "ulgen: bu komut API anahtarınızı ekrana yazdırır; gerçekten istiyorsanız --show ekleyin\n" {
		t.Errorf("Turkish: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = exec(t, context.Background(), nil, "token", "--show")
	if code != 0 || stdout != testKey+"\n" || stderr != "" {
		t.Errorf("--show: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

// refusingStore fails the test when the key is asked for.
type refusingStore struct {
	credential.Store
	t *testing.T
}

func (s refusingStore) Get(string) (string, error) {
	s.t.Error("the key was read although it was not going to be printed")
	return "", credential.ErrNotFound
}

// The screen is checked before the key is read: reading it can open a
// keychain dialog, and there is no point in that when the answer is no.
func TestTokenRefusesBeforeItReadsTheKey(t *testing.T) {
	useStore(t, refusingStore{Store: useFileStore(t), t: t})
	useTerminalStdout(t, true)
	code, stdout, stderr := exec(t, context.Background(), nil, "token")
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "ulgen: this would print your API key on the screen") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

// failingWriter is a standard output that cannot be written to: a closed
// descriptor, a pipe whose reader is gone, a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write /dev/stdout: broken\x1b[2J pipe")
}

// A program that asked for the key and did not get it must see a failure.
func TestTokenFailsWhenTheKeyCannotBeWritten(t *testing.T) {
	useFileStore(t).Set("evren", testKey)
	useTerminalStdout(t, false)
	for lang, want := range map[string]string{
		"en_US.UTF-8": "ulgen: the key could not be written to the output: write /dev/stdout: broken pipe\n",
		"tr_TR.UTF-8": "ulgen: anahtar çıktıya yazılamadı: write /dev/stdout: broken pipe\n",
	} {
		var stderr strings.Builder
		getenv := func(k string) string { return map[string]string{"LANG": lang}[k] }
		code := run(context.Background(), []string{"token"}, strings.NewReader(""), failingWriter{}, &stderr, getenv)
		if code != 1 || stderr.String() != want {
			t.Errorf("%s: code %d stderr %q", lang, code, stderr.String())
		}
		assertNoKey(t, "standard error of ulgen token", stderr.String(), testKey)
	}
}

func TestTokenErrors(t *testing.T) {
	useFileStore(t)
	useTerminalStdout(t, false)
	code, stdout, stderr := exec(t, context.Background(), nil, "token")
	if code != 1 || stdout != "" || stderr != "ulgen: not signed in; run ulgen login\n" {
		t.Errorf("signed out: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = exec(t, context.Background(), nil, "token", "extra")
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "ulgen: unknown arguments: extra\n") {
		t.Errorf("extra argument: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	// A key pasted after the command is not printed back.
	code, stdout, stderr = exec(t, context.Background(), nil, "token", "evren_llm_PASTED", "more")
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "ulgen: unknown arguments: [key] more\n") || strings.Contains(stderr, "PASTED") {
		t.Errorf("a pasted key: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = exec(t, context.Background(), nil, "token", "-h")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "ulgen token [options]") || !strings.Contains(stdout, "-show") {
		t.Errorf("help: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}
