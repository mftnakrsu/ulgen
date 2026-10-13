package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/proxy"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// Seams the tests replace: making the translator listen, running it, and the
// token generator. A test never binds a port, serves, or makes a real random
// token.
var (
	listenTranslator = func(port int) (net.Listener, error) {
		return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	serveTranslator = func(s *proxy.Server, ln net.Listener) error { return s.Serve(ln) }
	randomToken     = func() (string, error) { return randomHex(32) }
)

// tokenEnv is the environment variable that holds the token "ulgen proxy"
// uses, instead of a generated one.
const tokenEnv = "ULGEN_PROXY_TOKEN"

// minTokenLen is the shortest token "ulgen proxy" accepts from tokenEnv. It
// is read as text, never printed.
const minTokenLen = 16

// shutdownGrace bounds how long the translator is given to finish the
// requests that are running when the command is interrupted.
const shutdownGrace = 5 * time.Second

// randomHex returns n random bytes as a lower-case hex string.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// proxyOptions are the options the two translated commands share: the model
// to point at and the API root.
type proxyOptions struct {
	model   string // --model, "" for EVREN's default
	baseURL string // --base-url, "" for EVREN's own root
}

// translatorSetup is what a started (or to-be-started) translator needs: the
// EVREN models, the chosen model and the small model, and the max output of
// the chosen model.
type translatorSetup struct {
	models     []setup.Model
	def        string
	small      string
	baseURL    string
	maxOutputs int
}

// setupTranslator resolves the key, asks EVREN for its models and chooses the
// model and the small model. It is the shared first half of "ulgen run
// claude" and "ulgen proxy": the same rules "ulgen setup" uses apply. It
// returns the key as well, which the translator holds and no one else sees.
func setupTranslator(ctx context.Context, opts proxyOptions, getenv func(string) string, stderr io.Writer, tr *i18n.Translator) (translatorSetup, string, *failure) {
	cred, code := resolveKey(stderr, getenv, tr)
	if code != exitOK {
		return translatorSetup{}, "", &failure{code: code}
	}
	if notice := baseURLNotice(opts.baseURL); notice != "" {
		fmt.Fprintln(stderr, tr.T("notice.base_url", notice))
	}
	models, skipped, err := fetchModels(ctx, evren.New(cred.Secret, opts.baseURL, userAgent()))
	if err != nil {
		f := &failure{msg: describeEvrenError(err, tr, tr.T("status.key_invalid"), tr.T("status.terms_missing")), code: exitError}
		if errors.Is(err, context.Canceled) {
			f.code = exitInterrupted
		}
		return translatorSetup{}, "", f
	}
	if skipped > 0 {
		fmt.Fprintln(stderr, "ulgen: "+plural(tr, "setup.models_skipped", skipped))
	}
	if len(models) == 0 {
		return translatorSetup{}, "", &failure{msg: tr.T("setup.no_models"), code: exitError}
	}
	def, _, f := chooseDefault(models, opts.model, tr)
	if f != nil {
		return translatorSetup{}, "", f
	}
	base := provider.EVREN.BaseURL
	if opts.baseURL != "" {
		base = opts.baseURL
	}
	return translatorSetup{
		models:     models,
		def:        def,
		small:      smallModel(models, def),
		baseURL:    base,
		maxOutputs: outputLimit(models, def),
	}, cred.Secret, nil
}

// smallModel is the model Claude Code uses for small jobs (a name that
// contains "haiku"): deepseek-v4-flash when EVREN offers it, else the chosen
// model.
func smallModel(models []setup.Model, def string) string {
	const preferred = "deepseek-v4-flash"
	for _, m := range models {
		if m.ID == preferred {
			return preferred
		}
	}
	return def
}

// outputLimit returns the chosen model's output limit, or 0 when EVREN gives
// none.
func outputLimit(models []setup.Model, def string) int {
	for _, m := range models {
		if m.ID == def {
			return m.MaxOutput
		}
	}
	return 0
}

// modelIDs returns the EVREN model ids, for the translator's model list.
func modelIDs(models []setup.Model) []string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

// startTranslator makes the translator listen on loopback on the given port
// (0: a port the system picks) and returns it, ready to serve. It holds the
// EVREN key; the token is what a client must present. It never listens on
// anything but loopback.
func startTranslator(s translatorSetup, key, token string, port int) (*proxy.Server, net.Listener, error) {
	srv, err := proxy.New(proxy.Config{
		BaseURL: s.baseURL,
		Key:     key,
		Token:   token,
		Models:  modelIDs(s.models),
		Default: s.def,
		Small:   s.small,
	})
	if err != nil {
		return nil, nil, err
	}
	ln, err := listenTranslator(port)
	if err != nil {
		return nil, nil, err
	}
	return srv, ln, nil
}

// runTranslator serves until ctx ends, then shuts down cleanly. It returns
// the failure of a listener that stopped by itself, or nil after the
// shutdown the interrupt asked for.
func runTranslator(ctx context.Context, srv *proxy.Server, ln net.Listener, tr *i18n.Translator, stderr io.Writer) *failure {
	done := make(chan error, 1)
	go func() { done <- serveTranslator(srv, ln) }()

	select {
	case err := <-done:
		// The listener stopped without an interrupt: a real error, unless
		// shutdown closed it.
		if err != nil {
			return &failure{msg: tr.T("proxy.serve_failed", oneLine(err.Error())), code: exitError}
		}
		return nil
	case <-ctx.Done():
	}

	return stopTranslator(srv, tr)
}

// stopTranslator shuts a translator down cleanly, giving the requests that
// are running the same grace period "ulgen proxy" gives them at an interrupt.
// It returns the failure of a shutdown that did not finish, or nil.
func stopTranslator(srv *proxy.Server, tr *i18n.Translator) *failure {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return &failure{msg: tr.T("proxy.shutdown_failed", oneLine(err.Error())), code: exitError}
	}
	return nil
}

// runProxy runs the translator for another program:
// "ulgen proxy [--port N] [--model ID] [--base-url URL] [--print-env]". It
// listens on loopback, prints the address, and ends cleanly at an interrupt.
func runProxy(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen proxy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, tr.T("flag.proxy_port"))
	var opts proxyOptions
	fs.StringVar(&opts.model, "model", "", tr.T("flag.setup_model", provider.EVREN.DefaultModel))
	fs.StringVar(&opts.baseURL, "base-url", "", tr.T("flag.evren_base_url"))
	printEnv := fs.Bool("print-env", false, tr.T("flag.proxy_print_env"))
	if code, done := parseSub(fs, "proxy", args, stdout, stderr, tr); done {
		return code
	}
	if *port < 0 || *port > 65535 {
		return usageError(fs, "proxy", tr.T("proxy.bad_port", *port), stderr, tr)
	}
	if err := checkBaseURL(opts.baseURL); err != nil {
		return usageError(fs, "proxy", tr.T("error.base_url_unsafe"), stderr, tr)
	}

	token, f := proxyToken(getenv, tr)
	if f != nil {
		return f.report(stderr, "")
	}
	setupTr, key, f := setupTranslator(ctx, opts, getenv, stderr, tr)
	if f != nil {
		return f.report(stderr, "")
	}
	if ctx.Err() != nil {
		return cancelled(tr).report(stderr, "")
	}
	srv, ln, err := startTranslator(setupTr, key, token, *port)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+tr.T("proxy.listen_failed", oneLine(err.Error())))
		return exitError
	}
	base := "http://" + ln.Addr().String()
	fmt.Fprintln(stderr, "ulgen: "+tr.T("proxy.listening", base))
	if *printEnv {
		io.WriteString(stdout, claudeEnvLines(base, token, setupTr, envStyle()))
	}

	if f := runTranslator(ctx, srv, ln, tr, stderr); f != nil {
		return f.report(stderr, "")
	}
	return exitOK
}

// proxyToken is the token "ulgen proxy" accepts: ULGEN_PROXY_TOKEN when it
// is set and long enough, else a fresh random one. The value is never
// printed, so a short one is reported by name only.
func proxyToken(getenv func(string) string, tr *i18n.Translator) (string, *failure) {
	if v := getenv(tokenEnv); v != "" {
		if len(v) < minTokenLen {
			return "", &failure{msg: tr.T("proxy.token_short", tokenEnv, minTokenLen), code: exitError}
		}
		return v, nil
	}
	token, err := randomToken()
	if err != nil {
		return "", &failure{msg: tr.T("proxy.token_failed", oneLine(err.Error())), code: exitError}
	}
	return token, nil
}

// envStyle says which shell's form of the variables is written: "powershell"
// on Windows, "posix" elsewhere. It reads the operating system Ülgen runs
// on. It is a seam so a test can ask for the other form without changing the
// machine's system.
var envStyle = func() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "posix"
}

// claudeEnvLines writes the lines that put a shell into the state "ulgen run
// claude" gives its child: the local address, the token, and the model
// variables. It is what the user pastes into the shell that starts Claude
// Code; the token is meant to be there, so it is printed in full.
func claudeEnvLines(baseURL, token string, s translatorSetup, style string) string {
	vars := claudeVars(baseURL, token, claudeEnvToken, s)
	vars = append(vars, fmt.Sprintf("%s=%d", claudeEnvMaxOutput, maxOutput(s)))
	var b strings.Builder
	for _, kv := range vars {
		name, value, _ := strings.Cut(kv, "=")
		b.WriteString(shellLine(style, name, value))
	}
	return b.String()
}

// shellLine writes one variable assignment in the shell's form, quoting the
// value so a token with a special character in it is safe to paste.
func shellLine(style, name, value string) string {
	if style == "powershell" {
		return "$env:" + name + " = '" + strings.ReplaceAll(value, "'", "''") + "'\n"
	}
	return name + "='" + strings.ReplaceAll(value, "'", `'\''`) + "'\n"
}
