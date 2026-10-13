// Command ulgen connects EVREN, Türkiye's model API, to the coding tools
// people already use.
//
//	ulgen           the guide: sign in, pick a tool, set it up, start it
//	ulgen <tool>    the same for the named tool (qwen, opencode, codex, claude)
//	ulgen login     sign in: API key, terms, quota
//	ulgen logout    remove the saved key
//	ulgen status    remaining quota and models
//	ulgen doctor    check the key, EVREN and the tools; say what to do about what is wrong
//	ulgen setup     write the EVREN provider into Qwen Code, opencode and Codex CLI
//	ulgen run       start a tool with the key in its environment; Claude Code through the translator
//	ulgen worker    run one headless Claude Code on an EVREN model to do a task unattended
//	ulgen proxy     run the translator on its own, for a program that speaks the Anthropic API
//	ulgen token     print the saved key for another program
//
// The key is never printed, logged, written into a tool's file or put into
// an error message. The one exception is the standard output of "ulgen
// token".
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup"
	"github.com/mftnakrsu/ulgen/internal/version"
)

// Exit codes.
const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitInterrupted = 130
)

// main runs the command line with the real streams and environment and
// exits with the code run returns.
func main() {
	ctx, stop := signalContext()
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

// signalContext returns the context of a run: it ends at the first
// termination signal. After that signal the default handling returns, so a
// second Ctrl+C (or TERM) ends a program that is slow to wind down. (While
// "ulgen run" waits for a tool on Windows it keeps a registration of its
// own; see platformExec.)
func signalContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals...)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

// run executes the command line and returns the process exit code. The
// first word picks the subcommand; each subcommand parses its own options.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	// The configuration file, and with it the language override, is still to come.
	tr := i18n.New(i18n.Detect("", getenv))
	if len(args) == 0 {
		// On a terminal "ulgen" alone is the guide; a script gets the usage.
		if stdinTerminal() != notTerminal && stdoutIsTerminal() {
			return runGuide(ctx, "", nil, stdin, stdout, stderr, getenv, tr)
		}
		io.WriteString(stdout, mainUsage(tr))
		return exitOK
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		io.WriteString(stdout, mainUsage(tr))
		return exitOK
	case "version", "-version", "--version":
		fmt.Fprintf(stdout, "ulgen %s\n", version.Current())
		return exitOK
	case "status":
		return runStatus(ctx, args[1:], stdout, stderr, getenv, tr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr, getenv, tr)
	case "setup":
		return runSetup(ctx, args[1:], stdout, stderr, getenv, tr)
	case "run":
		return runRun(ctx, args[1:], stdout, stderr, getenv, tr)
	case "worker":
		return runWorker(ctx, args[1:], stdin, stdout, stderr, getenv, tr)
	case "proxy":
		return runProxy(ctx, args[1:], stdout, stderr, getenv, tr)
	case "token":
		return runToken(args[1:], stdout, stderr, getenv, tr)
	case "login":
		return runLogin(ctx, args[1:], stdin, stdout, stderr, getenv, tr)
	case "logout":
		return runLogout(args[1:], stdout, stderr, getenv, tr)
	}
	// A tool's name is a shortcut: "ulgen qwen" signs in and sets the tool up
	// when that is still to do, then starts it.
	if _, ok := setup.Lookup(args[0]); ok {
		return runGuide(ctx, args[0], args[1:], stdin, stdout, stderr, getenv, tr)
	}
	// A word that is not a command may be anything, a pasted key included.
	msg := tr.T("error.unknown_command", safeArg(args[0]))
	if strings.HasPrefix(args[0], "-") {
		msg = tr.T("error.unknown_option", safeArg(args[0]))
	}
	fmt.Fprintln(stderr, "ulgen: "+msg)
	io.WriteString(stderr, mainUsage(tr))
	return exitUsage
}

// mainUsage is the top-level usage: the subcommands.
func mainUsage(tr *i18n.Translator) string {
	return tr.T("usage.main") + "\n"
}
