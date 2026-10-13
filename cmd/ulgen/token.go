package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mftnakrsu/ulgen/internal/i18n"
)

// stdoutIsTerminal is a seam: whether standard output is a screen, be it a
// console or the window of Git Bash (see terminalOf).
var stdoutIsTerminal = func() bool { return terminalOf(os.Stdout) != notTerminal }

// runToken prints the stored key for another program: "ulgen token [--show]".
// It is the one place where the key leaves Ülgen as text, so it refuses to
// put it on a screen unless asked to.
func runToken(args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	show := fs.Bool("show", false, tr.T("flag.token_show"))
	if code, done := parseSub(fs, "token", args, stdout, stderr, tr); done {
		return code
	}
	// The screen is checked before the key is read: reading can open a
	// keychain dialog, which would be for nothing when the answer is no.
	if !*show && stdoutIsTerminal() {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("token.terminal"))
		return exitUsage
	}
	cred, code := resolveKey(stderr, getenv, tr)
	if code != exitOK {
		return code
	}
	// A program that asked for the key and did not get it must see a failure.
	if _, err := fmt.Fprintln(stdout, cred.Secret); err != nil {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("token.write_failed", oneLine(err.Error())))
		return exitError
	}
	return exitOK
}
