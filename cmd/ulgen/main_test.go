package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup"
	"github.com/mftnakrsu/ulgen/internal/version"
)

// TestMain guards the real machine: before any test runs, every seam that
// reaches it is replaced by a function that fails loudly. The helpers
// (useFileStore, useFakeBrowser, useSetupEnv, useFakeExec, useTerminalStdout)
// replace the seams per test, so every test that needs them still does; a
// test that forgets one now fails instead of touching the owner's keychain,
// home directory, PATH or terminal, opening a real browser or starting a
// real tool.
//
// The test binary also plays the tool in the tests that start a real
// process: started with ULGEN_TEST_HELPER set, it does what that variable
// says and exits (see helper_test.go).
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperVar); mode != "" {
		os.Exit(runHelper(mode))
	}
	openStore = func(string, func(string) string) (credential.Store, error) {
		panic("a test reached the real credential store: use useFileStore")
	}
	defaultConfigDir = func(func(string) string) (string, error) {
		panic("a test asked for the real configuration directory: use useFileStore")
	}
	openBrowser = func(string) error {
		panic("a test tried to open the real browser: use useFakeBrowser")
	}
	setupEnv = func(func(string) string) setup.Env {
		panic("a test looked at the real machine (home directory, PATH): use useSetupEnv")
	}
	execTool = func(string, []string, []string) (int, error) {
		panic("a test tried to start a real tool: use useFakeExec")
	}
	environ = func() []string {
		panic("a test read the real environment: use useFakeExec")
	}
	workingDir = func() (string, error) {
		panic("a test asked for the real working directory: use useSetupEnv")
	}
	startWorkerChild = func(context.Context, string, []string, []string, string, io.Reader, io.Writer, <-chan struct{}) (workerEnd, *endCause) {
		panic("a test tried to start a real worker child: use useFakeWorkerChild")
	}
	workerGoEnv = func(string) (string, error) {
		panic("a test asked the real go command: use useGoEnv")
	}
	// No test makes a directory outside its own through the worker's presets;
	// the one test of that behaviour puts os.MkdirAll back for itself.
	workerMakeDir = func(string, os.FileMode) error { return nil }
	stdoutIsTerminal = func() bool {
		panic("a test asked whether the real standard output is a terminal: use useTerminalStdout")
	}
	stdinTerminal = func() terminalKind {
		panic("a test asked whether the real standard input is a terminal: use useFakeBrowser")
	}
	os.Exit(m.Run())
}

// exec runs the command with the given arguments and no standard input.
func exec(t *testing.T, ctx context.Context, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return execIn(t, ctx, env, "", args...)
}

// execIn runs the command with the given standard input. It checks every
// run for the key: no command prints it, except "ulgen token" on its
// standard output.
func execIn(t *testing.T, ctx context.Context, env map[string]string, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	getenv := func(k string) string { return env[k] }
	code = run(ctx, args, strings.NewReader(stdin), &out, &errOut, getenv)
	secrets := []string{testKey}
	if v := strings.TrimSpace(env["EVREN_API_KEY"]); v != "" {
		secrets = append(secrets, v)
	}
	assertNoKey(t, "standard error of ulgen "+strings.Join(args, " "), errOut.String(), secrets...)
	if len(args) == 0 || args[0] != "token" {
		assertNoKey(t, "standard output of ulgen "+strings.Join(args, " "), out.String(), secrets...)
	}
	return code, out.String(), errOut.String()
}

// assertNoKey is the one check that the key did not leak: text, which is
// described by where, must not contain any of the secrets. Every command
// run of a test goes through it (execIn), and so does every file a setup
// test leaves in the home directory (useSetupEnv).
func assertNoKey(t *testing.T, where, text string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("the key is in the %s", where)
		}
	}
}

func TestVersion(t *testing.T) {
	want := "ulgen " + version.Current().String() + "\n"
	for _, args := range [][]string{{"--version"}, {"-version"}, {"version"}} {
		code, stdout, stderr := exec(t, context.Background(), nil, args...)
		if code != 0 || stdout != want || stderr != "" {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestHelp(t *testing.T) {
	useGuide(t, "", false)
	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"help"}} {
		code, stdout, stderr := exec(t, context.Background(), nil, args...)
		if code != 0 || !strings.HasPrefix(stdout, "Usage:") || !strings.Contains(stdout, "ulgen login") || !strings.Contains(stdout, "ulgen status") || stderr != "" {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
	code, stdout, _ := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "--help")
	if code != 0 || !strings.HasPrefix(stdout, "Kullanım:") || !strings.Contains(stdout, "ulgen login") {
		t.Errorf("Turkish help: code %d stdout %q", code, stdout)
	}
}

func TestTopLevelUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"bogus"}, "unknown command: bogus"},
		{"unknown option", []string{"-p", "hi"}, "unknown option: -p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := exec(t, context.Background(), nil, tt.args...)
			if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: "+tt.want+"\n") || !strings.Contains(stderr, "Usage:") {
				t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
			}
		})
	}
	code, _, stderr := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "bogus")
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: bilinmeyen komut: bogus\n") || !strings.Contains(stderr, "Kullanım:") {
		t.Errorf("Turkish: code %d stderr %q", code, stderr)
	}
}

func TestTranslateFlagError(t *testing.T) {
	tr := i18n.New("en")
	for in, want := range map[string]string{
		"flag provided but not defined: -bogus":                 "unknown option: -bogus",
		"flag needs an argument: -model":                        "option -model needs a value",
		`invalid value "x" for flag -port: parse error`:         `invalid value "x" for option -port`,
		`invalid boolean value "maybe" for -dry-run: parse err`: `invalid value "maybe" for option -dry-run`,
	} {
		if got := translateFlagError(errString(in), tr); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := translateFlagError(errString("flag provided but not defined: -bogus"), i18n.New("tr")); got != "bilinmeyen seçenek: -bogus" {
		t.Errorf("Turkish: got %q", got)
	}
	if got := translateFlagError(errString("something else"), tr); got != "something else" {
		t.Errorf("got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
