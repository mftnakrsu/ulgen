package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// Seams the tests replace: how a tool is started, the caller's environment,
// and the directory the command runs in.
var (
	execTool   = platformExec
	environ    = os.Environ
	workingDir = os.Getwd
)

// runRun starts a tool with the EVREN key in its environment:
// "ulgen run [options] <tool> [arguments...]". Ülgen's own options stand
// before the tool's name; everything after the name belongs to the tool and
// is not looked at as an option.
func runRun(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	trust := fs.Bool("trust-project", false, tr.T("flag.run_trust_project"))
	var opts proxyOptions
	fs.StringVar(&opts.model, "model", "", tr.T("flag.setup_model", provider.EVREN.DefaultModel))
	fs.StringVar(&opts.baseURL, "base-url", "", tr.T("flag.evren_base_url"))
	usage := tr.T("run.usage", strings.Join(setup.IDs(), ", ")) + "\n" + optionRows(tr, fs)
	// Parse stops at the first word that is not an option: the tool's name.
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		io.WriteString(stdout, usage)
		return exitOK
	}
	if err != nil {
		return usageMistake(translateFlagError(err, tr)).report(stderr, usage)
	}
	rest := fs.Args()
	if len(rest) == 0 || rest[0] == "" {
		return usageMistake(tr.T("run.no_tool")).report(stderr, usage)
	}
	tool, ok := setup.Lookup(rest[0])
	if !ok {
		return usageMistake(tr.T("run.unknown_tool", safeArg(rest[0]), strings.Join(setup.IDs(), ", "))).report(stderr, usage)
	}
	proxied := false
	if p, ok := tool.(setup.Proxied); ok {
		proxied = p.Proxied()
	}
	// --model and --base-url point Claude Code at an EVREN model through the
	// translator; a tool that is started another way has no use for them.
	if !proxied && (flagWasSet(fs, "model") || flagWasSet(fs, "base-url")) {
		return usageMistake(tr.T("run.options_need_proxied", strings.Join(proxiedIDs(), ", "))).report(stderr, usage)
	}
	if err := checkBaseURL(opts.baseURL); err != nil {
		return usageMistake(tr.T("error.base_url_unsafe")).report(stderr, usage)
	}
	l := &launch{ctx: ctx, stderr: stderr, getenv: getenv, tr: tr, tool: tool, args: rest[1:], trustProject: *trust}
	var code int
	var f *failure
	if proxied {
		code, f = l.runProxied(opts)
	} else {
		code, f = l.run()
	}
	if f != nil {
		return f.report(stderr, usage)
	}
	return code
}

// proxiedIDs lists the tools that "ulgen run" starts through the
// translator: the adapters that answer the Proxied interface with true, in
// the order they are registered.
func proxiedIDs() []string {
	var ids []string
	for _, tool := range setup.Tools() {
		if p, ok := tool.(setup.Proxied); ok && p.Proxied() {
			ids = append(ids, tool.ID())
		}
	}
	return ids
}

// flagWasSet reports whether the named option was given on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// launch carries what the steps of one "ulgen run" share.
type launch struct {
	ctx          context.Context
	stderr       io.Writer
	getenv       func(string) string
	tr           *i18n.Translator
	tool         setup.Tool
	args         []string  // the tool's arguments, as the user gave them
	trustProject bool      // --trust-project: do not look at the project's files
	env          setup.Env // the machine, set by run
}

// run finds the tool, checks that it may be started, fetches the key and
// starts the tool. It returns the tool's exit code, or the failure that kept
// it from being started.
func (l *launch) run() (int, *failure) {
	env, err := checkedEnv(l.getenv)
	if err != nil {
		return 0, l.failed(describeSetupError(err, env.Home, l.tr))
	}
	// Only the tool's path is needed. Asking for its version would run the
	// tool once more before every start.
	env.Version = noVersion
	l.env = env
	found := l.tool.Detect(l.ctx, env)
	if !found.Installed {
		fmt.Fprintln(l.stderr, "ulgen: "+l.tr.T("run.not_installed", l.tool.Name()))
		fmt.Fprintln(l.stderr, renderNote(l.tr, env, l.tool.InstallHint(env)))
		return 0, &failure{code: exitError}
	}
	if f := l.refusal(found); f != nil {
		return 0, f
	}
	cred, code := resolveKey(l.stderr, l.getenv, l.tr)
	if code != exitOK {
		return 0, &failure{code: code}
	}
	return l.start(found, cred.Secret)
}

// failed is the failure for a run that does not start the tool.
func (l *launch) failed(msg string) *failure {
	return &failure{msg: msg, code: exitError}
}

// quietNoticeEnv leaves out the one-line notice "ulgen run claude" prints
// before it starts the tool: its value "1" means the user has seen it.
const quietNoticeEnv = "ULGEN_QUIET_NOTICE"

// runProxied starts a tool through Ülgen's translator, for a tool that speaks
// the Anthropic API and cannot be pointed at EVREN by a file. It is the steps
// of the design for "ulgen run claude": the tool is found, the EVREN models
// are fetched and the model is chosen, a translator listens on loopback and
// holds the key, and the tool is started beside it with the translator's
// address and a token of its own in its environment. The tool never sees the
// EVREN key. The function returns the tool's exit code; the translator is
// shut down once the tool has ended.
func (l *launch) runProxied(opts proxyOptions) (int, *failure) {
	env, err := checkedEnv(l.getenv)
	if err != nil {
		return 0, l.failed(describeSetupError(err, env.Home, l.tr))
	}
	env.Version = noVersion // only the tool's path is needed; see run
	l.env = env
	found := l.tool.Detect(l.ctx, env)
	if !found.Installed {
		fmt.Fprintln(l.stderr, "ulgen: "+l.tr.T("run.not_installed", l.tool.Name()))
		fmt.Fprintln(l.stderr, renderNote(l.tr, env, l.tool.InstallHint(env)))
		return 0, &failure{code: exitError}
	}
	// The refusals run before the model list is fetched and before the
	// translator starts: a .cmd start that cmd.exe would misread, or the other
	// tools' refusals, must not cost a model fetch or a listening port.
	if f := l.refusal(found); f != nil {
		return 0, f
	}
	setupTr, key, f := setupTranslator(l.ctx, opts, l.getenv, l.stderr, l.tr)
	if f != nil {
		return 0, f
	}
	if l.ctx.Err() != nil {
		return 0, cancelled(l.tr)
	}
	token, err := randomToken()
	if err != nil {
		return 0, l.failed(l.tr.T("proxy.token_failed", oneLine(err.Error())))
	}
	srv, ln, err := startTranslator(setupTr, key, token, 0)
	if err != nil {
		return 0, l.failed(l.tr.T("proxy.listen_failed", oneLine(err.Error())))
	}
	base := "http://" + ln.Addr().String()
	// The translator serves while the tool runs. It listens before the tool
	// starts, so a request the tool makes at once finds it ready.
	served := make(chan error, 1)
	// The serve function is read here, not inside the goroutine: the variable
	// is a seam the tests replace, and reading it in the same goroutine that
	// runs the command keeps that write ordered with the start of the work.
	serve := serveTranslator
	go func() { served <- serve(srv, ln) }()
	// The notice says what the user is about to run; it comes once per run,
	// right before the tool starts.
	if l.getenv(quietNoticeEnv) != "1" {
		fmt.Fprintln(l.stderr, "ulgen: "+l.tr.T("run.claude_notice"))
	}
	// The context is checked right before the tool is started: after a Ctrl+C
	// nothing is started any more.
	if l.ctx.Err() != nil {
		if sf := stopTranslator(srv, l.tr); sf != nil {
			fmt.Fprintln(l.stderr, "ulgen: "+sf.msg)
		}
		return 0, cancelled(l.tr)
	}
	childEnv := claudeChildEnv(environ(), base, token, claudeEnvToken, setupTr, env.GOOS == "windows")
	argv := append([]string{found.Binary}, l.tool.Launch(env, l.args)...)
	type childResult struct {
		code int
		err  error
	}
	childDone := make(chan childResult, 1)
	go func() {
		code, err := startChild(found.Binary, argv, childEnv)
		childDone <- childResult{code: code, err: err}
	}()
	// While the tool runs, a translator that stops serving is not lost: one
	// line says so and the tool is left to end by itself.
	var res childResult
	select {
	case res = <-childDone:
	case serr := <-served:
		if serr != nil && l.ctx.Err() == nil {
			fmt.Fprintln(l.stderr, "ulgen: "+l.tr.T("proxy.serve_stopped", oneLine(serr.Error())))
		}
		res = <-childDone
	}
	// The tool has ended; the translator has no one left to serve. A failure
	// to wind it down is reported but does not replace the tool's own code.
	if sf := stopTranslator(srv, l.tr); sf != nil {
		if res.code == 0 && res.err == nil {
			return 0, sf
		}
		fmt.Fprintln(l.stderr, "ulgen: "+sf.msg)
	}
	if res.err != nil {
		if f := l.shimFailure(res.err, found.Binary); f != nil {
			return 0, f
		}
		return 0, l.failed(l.tr.T("run.failed", l.tool.Name(), showPath(env.Home, found.Binary), oneLine(res.err.Error())))
	}
	return res.code, nil
}

// refusal returns why the tool must not be started, or nil: EVREN is not
// set up in it, an argument cannot be passed safely (Windows, a tool
// installed as a batch file), or a file of the project could send the key
// elsewhere. All of it is checked before the key is fetched.
func (l *launch) refusal(found setup.Detection) *failure {
	// A tool started through the translator reaches EVREN because "ulgen run"
	// gives it the translator's address, not because a file of its own points
	// there, so there is nothing that could be "not set up" in it. For every
	// other tool the provider must be in its files.
	if p, ok := l.tool.(setup.Proxied); !ok || !p.Proxied() {
		configured, err := l.tool.Configured(l.env)
		if err != nil {
			return l.failed(describeSetupError(err, l.env.Home, l.tr))
		}
		if !configured {
			return l.failed(l.tr.T("run.not_set_up", l.tool.Name(), l.tool.ID()))
		}
	}
	if l.env.GOOS == "windows" && isBatchFile(found.Binary) {
		if _, err := shimCommandLine(found.Binary, l.args); err != nil {
			if f := l.shimFailure(err, found.Binary); f != nil {
				return f
			}
		}
	}
	if !l.trustProject {
		return l.projectRefusal()
	}
	return nil
}

// shimFailure words the reason a batch file cannot be started through cmd.exe
// as asked: a *shimArgError for a character cmd.exe would interpret, or a
// *shimLineError for a command line that is too long. It returns nil for
// anything else, which the start itself then reports. A refusal is a mistake
// of the command line, so the exit code is the usage one, but the usage text
// would not help and is not printed.
func (l *launch) shimFailure(err error, binary string) *failure {
	var bad *shimArgError
	if errors.As(err, &bad) {
		var msg string
		lineBreak := bad.Char == '\r' || bad.Char == '\n'
		switch {
		case bad.Index == 0 && lineBreak:
			msg = l.tr.T("run.shim_path", l.tool.Name(), showPath(l.env.Home, binary), fmt.Sprintf("U+%04X", bad.Char))
		case bad.Index == 0:
			msg = l.tr.T("run.shim_path", l.tool.Name(), showPath(l.env.Home, binary), string(bad.Char))
		case lineBreak:
			msg = l.tr.T("run.shim_arg_newline", bad.Index, l.tool.Name())
		default:
			msg = l.tr.T("run.shim_arg", bad.Index, string(bad.Char), l.tool.Name())
		}
		return &failure{msg: msg, code: exitUsage}
	}
	var long *shimLineError
	if errors.As(err, &long) {
		return &failure{msg: l.tr.T("run.shim_line_too_long", maxShimLine, l.tool.Name()), code: exitUsage}
	}
	return nil
}

// projectRefusal returns the failure for a project file that could send the
// key elsewhere, or nil when the project has none. When the project cannot
// be looked at, that is a failure as well.
func (l *launch) projectRefusal() *failure {
	if len(projectConfig[l.tool.ID()]) == 0 {
		return nil // this tool reads no project file that could matter
	}
	cwd, err := workingDir()
	var finding *projectFinding
	if err == nil {
		finding, err = findProjectConfig(l.tool, l.env, cwd)
	}
	switch {
	case err != nil:
		if msg, ok := describeEnvError(err, l.tr); ok {
			return l.failed(msg)
		}
		return l.failed(l.tr.T("run.project_unchecked", oneLine(err.Error()), l.tool.ID()))
	case finding == nil:
		return nil
	case finding.Unreadable:
		return l.failed(l.tr.T("run.project_unreadable", showPath(l.env.Home, finding.Path), l.tool.Name(), l.tool.ID()))
	}
	return l.failed(l.tr.T("run.project_file", showPath(l.env.Home, finding.Path), l.tool.Name(), l.tool.ID()))
}

// start hands over to the tool. The context is checked right before: after a
// Ctrl+C nothing is started any more.
func (l *launch) start(found setup.Detection, key string) (int, *failure) {
	if l.ctx.Err() != nil {
		return 0, cancelled(l.tr)
	}
	argv := append([]string{found.Binary}, l.tool.Launch(l.env, l.args)...)
	code, err := execTool(found.Binary, argv, withKey(environ(), key, l.env.GOOS == "windows"))
	if err != nil {
		if f := l.shimFailure(err, found.Binary); f != nil {
			return 0, f
		}
		return 0, l.failed(l.tr.T("run.failed", l.tool.Name(), showPath(l.env.Home, found.Binary), oneLine(err.Error())))
	}
	return code, nil
}

// withKey returns env with the EVREN key variable set to key, exactly once:
// an entry the caller's environment already has is dropped. foldCase is for
// Windows, where the names of variables differ in case only on paper.
func withKey(env []string, key string, foldCase bool) []string {
	prefix := provider.EVREN.KeyEnv + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		same := strings.HasPrefix(kv, prefix)
		if foldCase && len(kv) >= len(prefix) {
			same = strings.EqualFold(kv[:len(prefix)], prefix)
		}
		if !same {
			out = append(out, kv)
		}
	}
	return append(out, prefix+key)
}
