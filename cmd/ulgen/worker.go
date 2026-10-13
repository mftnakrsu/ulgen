package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
	"github.com/mftnakrsu/ulgen/internal/termsafe"
)

// This file is "ulgen worker": one headless Claude Code process on an EVREN
// model, run to its end unattended. It shares with "ulgen run claude" the
// translator set-up (setupTranslator, startTranslator, stopTranslator) and the
// building of the child's environment (claudeChildEnv), differing only in the
// token variable and the attribution switch, both of them parameters.

// The allow-list data: the always-allowed tools, the presets and the refused
// rules. docs/design.md ("Workers") says why each is what it is.
const (
	// workerDefaultTimeout is the default --timeout. 0 means no timeout.
	workerDefaultTimeout = 30 * time.Minute

	// workerMaxTask is the largest task that is read. A task may be up to
	// 1 MB (design).
	workerMaxTask = 1 << 20

	// workerSizeLimit names the 1 MB task bound in messages.
	workerSizeLimit = "1 MB"

	// workerOutputLimit names the 64 MB child-output bound in messages; the
	// bound itself is maxChildOutput.
	workerOutputLimit = "64 MB"

	// workerValueLimit is how much of a refused --allow value a message
	// repeats: never more than the first 40 characters of the value, so a
	// value cannot push the reason out of sight or forge a line.
	workerValueLimit = 40

	// exitTimeout is the exit code the design gives a worker that reached
	// --timeout.
	exitTimeout = 124
)

// workerAlwaysAllowed are the permission rules a worker always has: the rule
// for editing under its directory and the read-only shell commands of the
// design, in the design's order. They stand before the --allow rules in the
// child's --allowedTools. No bare file tool is here: a bare Read, Edit, Write,
// Glob or Grep would allow the tool everywhere, while Edit(./**) confines it
// to the worker's directory (design, "What a worker may do").
var workerAlwaysAllowed = []string{
	"Edit(./**)",
	"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)",
	"Bash(ls:*)", "Bash(mkdir:*)",
}

// workerPresets are the four presets --allow may name, in the design's order.
var workerPresets = map[string][]string{
	"go":     {"Bash(go:*)", "Bash(gofmt:*)", "Bash(GOOS=windows go:*)", "Bash(GOOS=linux go:*)", "Bash(GOOS=darwin go:*)"},
	"node":   {"Bash(node:*)", "Bash(npm test:*)", "Bash(npm run:*)"},
	"python": {"Bash(python3:*)", "Bash(python:*)", "Bash(pytest:*)"},
	"read":   {"Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(grep:*)", "Bash(rg:*)", "Bash(wc:*)", "Bash(sed -n:*)"},
}

// workerPresetNames lists the presets in a stable order, for messages and the
// usage text: the design's order, go, node, python, read.
func workerPresetNames() []string { return []string{"go", "node", "python", "read"} }

// workerRulePattern is what one accepted --allow value looks like as a whole:
// a tool name of letters, digits and underscores that begins with a letter,
// optionally followed by one parenthesised specifier and nothing else. It rules
// out a leading dash (which Claude Code would read as an option), white space
// outside the parentheses, an opening parenthesis left unclosed, and a comma or
// a second parenthesis anywhere: Claude Code documents its list of rules as
// separated by commas or spaces, so "Read(./a),Bash(*)" given as one value
// could be read there as two rules.
var workerRulePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\([^(),]*\))?$`)

// workerToolNames are the tools a rule may name, compared exactly. Another
// spelling, such as "bash" or "BASH", is refused rather than corrected. Edit
// and Write are not here: a worker already edits under its directory, and a
// rule for them could only widen that (see workerEditTools).
var workerToolNames = []string{"Bash", "Read", "WebFetch", "WebSearch"}

// workerEditTools are the file-editing tools. A rule for one of them is
// refused with its own reason.
var workerEditTools = []string{"Edit", "Write", "MultiEdit", "NotebookEdit"}

// workerRunners are the command names that make a Bash rule as good as
// Bash(*): a shell or a command runner would run anything. The comparison is
// against the command's base name, in lower case and without ".exe", so
// /bin/sh and PowerShell.exe also count. The list holds the usual ones and
// cannot be complete; the sandbox is the fence behind it.
var workerRunners = []string{
	"sh", "bash", "zsh", "dash", "ksh", "fish", "csh", "tcsh",
	"env", "xargs", "eval", "exec", "sudo", "su", "doas", "nohup",
	"command", "time", "cmd", "powershell", "pwsh", "osascript", "busybox",
	"nice", "timeout", "watch", "stdbuf", "setsid", "script", "wsl",
}

// workerShellOperators are the characters that join or redirect shell
// commands. A rule names one command, so a specifier that holds one is
// refused. (Command substitution with "$(" needs a parenthesis, which the
// shape already rules out.)
const workerShellOperators = ";&|<>`"

// workerResolveAllow turns the --allow values into the extra rules for the
// child's --allowedTools, in the order they were given. A value that names a
// preset expands to the preset's rules; any other value must be one permission
// rule that passes workerAllowProblem, and is then passed on as it stands. A
// value that is neither ends the command with a usage error that names the
// value's position and the reason (design, "The values of --allow").
func workerResolveAllow(values []string, tr *i18n.Translator) (rules []string, f *failure) {
	for i, v := range values {
		if preset, ok := workerPresets[v]; ok {
			rules = append(rules, preset...)
			continue
		}
		if key := workerAllowProblem(v); key != "" {
			return nil, usageMistake(tr.T(key, workerPosition(tr, i+1), workerShowValue(v)))
		}
		rules = append(rules, v)
	}
	return rules, nil
}

// workerAllowProblem returns the message key for why value is not an accepted
// --allow rule, or "" when it is one. The checks are the design's, in order:
// the shape; the tool name, compared exactly; and, for Bash, the specifier.
func workerAllowProblem(value string) string {
	if !workerRulePattern.MatchString(value) || strings.IndexFunc(value, workerOddRune) >= 0 {
		return "worker.allow_shape"
	}
	name, spec, hasSpec := strings.Cut(value, "(")
	if namesAny(name, workerEditTools, false) {
		return "worker.allow_edit"
	}
	if !namesAny(name, workerToolNames, false) {
		return "worker.allow_tool"
	}
	if name != "Bash" {
		return ""
	}
	if !hasSpec {
		return "worker.allow_bash_spec"
	}
	inside := strings.TrimSuffix(spec, ")")
	if workerWildcardOnly(inside) {
		return "worker.allow_any_bash"
	}
	if strings.ContainsAny(inside, workerShellOperators) {
		return "worker.allow_operator"
	}
	word := workerCommandWord(inside)
	if word == "" || strings.ContainsAny(word, "*?[") {
		// No command name, or a pattern in its place: the rule would match
		// commands by something other than their name.
		return "worker.allow_any_bash"
	}
	if workerIsRunner(word) {
		return "worker.allow_runner"
	}
	return ""
}

// workerOddRune reports a character no rule may hold: a control character
// (which covers tab and the line ends) or any white space other than the
// plain space.
func workerOddRune(r rune) bool {
	return unicode.IsControl(r) || unicode.IsSpace(r) && r != ' '
}

// workerWildcardOnly reports whether spec, a Bash rule's specifier with all
// white space removed, is empty or is only the characters "*" and ":". Each of
// those allows every shell command.
func workerWildcardOnly(spec string) bool {
	spec = strings.Join(strings.Fields(spec), "")
	if spec == "" {
		return true
	}
	return strings.Trim(spec, "*:") == ""
}

// workerCommandWord returns the command name a Bash specifier begins with:
// its first word that is not a leading VAR=value word, without quotation
// marks, and without the ":…" that ends a prefix rule such as "go:*". The
// colon of a drive prefix ("C:\…") belongs to the word. It returns "" when
// the specifier names no command.
func workerCommandWord(spec string) string {
	for _, word := range strings.Fields(spec) {
		word = strings.NewReplacer(`"`, "", "'", "").Replace(word)
		if workerEnvAssignment(word) {
			// Checked on the whole word, so that a value with a colon in it
			// (PATH=/a:/b) is skipped as one word.
			continue
		}
		start := 0
		if len(word) > 2 && word[1] == ':' && (word[2] == '\\' || word[2] == '/') {
			start = 2
		}
		if i := strings.IndexByte(word[start:], ':'); i >= 0 {
			word = word[:start+i]
		}
		return word
	}
	return ""
}

// workerIsRunner reports whether a command word names a shell or a command
// runner. A word written with a path in front counts by its base name.
func workerIsRunner(word string) bool {
	return namesAny(strings.TrimSuffix(workerBaseName(word), ".exe"), workerRunners, false)
}

// workerEnvAssignment reports whether word is a leading VAR=value word of a
// command line: a name of letters, digits and underscores that does not begin
// with a digit, then "=".
func workerEnvAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		letter := r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		if !letter && !digit {
			return false
		}
		if i == 0 && digit {
			return false
		}
	}
	return true
}

// workerBaseName drops any directory part from a word, for Unix and Windows
// separators alike, and lower-cases it.
func workerBaseName(word string) string {
	if i := strings.LastIndexAny(word, `/\`); i >= 0 {
		word = word[i+1:]
	}
	return strings.ToLower(word)
}

// workerShowValue makes a refused --allow value safe to print back: on one
// line, without escape sequences, with anything that looks like an EVREN key
// masked, and cut to workerValueLimit characters so the message stays a
// message.
func workerShowValue(value string) string {
	shown := []rune(safeArg(value))
	if len(shown) > workerValueLimit {
		shown = shown[:workerValueLimit]
	}
	return string(shown)
}

// workerPosition names the place of a --allow value in the run, for a message:
// "first", "second", and so on.
func workerPosition(tr *i18n.Translator, n int) string {
	key := fmt.Sprintf("worker.pos.%d", n)
	if msg := tr.T(key); msg != key {
		return msg
	}
	return fmt.Sprintf("%d.", n)
}

// workerOptions is what the command line of "ulgen worker" asks for.
type workerOptions struct {
	model     string   // --model, "" for EVREN's default
	dir       string   // --dir, "" for the working directory
	allow     []string // --allow, in order
	timeout   time.Duration
	maxTurns  int    // --max-turns, 0 when unset
	out       string // --out, "" to print no file
	noSandbox bool   // --no-sandbox
	baseURL   string // --base-url
	task      string // the task file, or "-" for standard input
}

// runWorker runs one headless worker:
// "ulgen worker [options] <task-file | ->". It reads the task, starts the
// translator, starts Claude Code as a child in --dir with the task on its
// standard input, waits (with --timeout), writes the child's JSON to --out
// when asked, prints a short summary, and returns the exit code.
func runWorker(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	var opts workerOptions
	fs := workerFlagSet(&opts, tr)
	usage := tr.T("worker.usage", strings.Join(workerPresetNames(), ", ")) + "\n" + optionRows(tr, fs)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			io.WriteString(stdout, usage)
			return exitOK
		}
		return usageMistake(translateFlagError(err, tr)).report(stderr, usage)
	}
	rest := fs.Args()
	if len(rest) != 1 || rest[0] == "" {
		return usageMistake(tr.T("worker.no_task")).report(stderr, usage)
	}
	opts.task = rest[0]
	if opts.timeout < 0 {
		return usageMistake(tr.T("worker.bad_timeout")).report(stderr, usage)
	}
	if opts.maxTurns < 0 {
		return usageMistake(tr.T("worker.bad_max_turns")).report(stderr, usage)
	}
	if err := checkBaseURL(opts.baseURL); err != nil {
		return usageMistake(tr.T("error.base_url_unsafe")).report(stderr, usage)
	}
	extraRules, f := workerResolveAllow(opts.allow, tr)
	if f != nil {
		return f.report(stderr, usage)
	}

	l := &workerLaunch{ctx: ctx, stdin: stdin, stdout: stdout, stderr: stderr,
		getenv: getenv, tr: tr, opts: opts, extraRules: extraRules, started: now()}
	if f := l.run(); f != nil {
		return f.report(stderr, usage)
	}
	return l.code
}

// workerFlagSet declares the options of "ulgen worker" on a flag set that
// fills opts. It is a function so that the options can be inspected without
// running the command: the skill's text is checked against them (see
// TestSkillNamesOnlyWhatExists).
func workerFlagSet(opts *workerOptions, tr *i18n.Translator) *flag.FlagSet {
	fs := flag.NewFlagSet("ulgen worker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.model, "model", "", tr.T("flag.setup_model", provider.EVREN.DefaultModel))
	fs.StringVar(&opts.dir, "dir", "", tr.T("flag.worker_dir"))
	fs.Var((*allowList)(&opts.allow), "allow", tr.T("flag.worker_allow", strings.Join(workerPresetNames(), ", ")))
	fs.DurationVar(&opts.timeout, "timeout", workerDefaultTimeout, tr.T("flag.worker_timeout"))
	fs.IntVar(&opts.maxTurns, "max-turns", 0, tr.T("flag.worker_max_turns"))
	fs.StringVar(&opts.out, "out", "", tr.T("flag.worker_out"))
	fs.BoolVar(&opts.noSandbox, "no-sandbox", false, tr.T("flag.worker_no_sandbox"))
	fs.StringVar(&opts.baseURL, "base-url", "", tr.T("flag.evren_base_url"))
	return fs
}

// allowList is a flag.Value that collects the repeated --allow values in
// order.
type allowList []string

// String returns the values, for the flag package's own error text.
func (a *allowList) String() string { return strings.Join(*a, ",") }

// Set appends one --allow value.
func (a *allowList) Set(v string) error {
	*a = append(*a, v)
	return nil
}

// workerLaunch carries what the steps of one "ulgen worker" share.
type workerLaunch struct {
	ctx        context.Context
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	getenv     func(string) string
	tr         *i18n.Translator
	opts       workerOptions
	extraRules []string
	started    time.Time

	code int // the exit code the run ended with
}

// run is the whole of the worker after option parsing: it reads the task,
// checks --dir, prepares the child's environment, and runs the child under the
// translator. It returns a failure only for something that kept the child from
// being started; the child's own ending is reported through l.code.
func (l *workerLaunch) run() *failure {
	tr := l.tr
	env, err := checkedEnv(l.getenv)
	if err != nil {
		return &failure{msg: describeSetupError(err, env.Home, tr), code: exitError}
	}

	// The task is read first, so an empty or missing one costs no model fetch.
	task, f := l.readTask()
	if f != nil {
		return f
	}

	// --dir must be a real project directory, not everything.
	dir, f := l.resolveDir(env)
	if f != nil {
		return f
	}

	// --out is prepared before anything is fetched or started: a directory that
	// is not there, or a file that cannot be created, ends the command with a
	// message and exit 1, and the worker is not started.
	if f := l.prepareOut(); f != nil {
		return f
	}

	// Claude Code must be installed.
	tool, _ := setup.Lookup("claude")
	env.Version = noVersion // only the tool's path is needed; see launch.run
	found := tool.Detect(l.ctx, env)
	if !found.Installed {
		fmt.Fprintln(l.stderr, "ulgen: "+tr.T("run.not_installed", tool.Name()))
		fmt.Fprintln(l.stderr, renderNote(tr, env, tool.InstallHint(env)))
		return &failure{code: exitError}
	}

	// On Windows Claude Code has no sandbox: a worker's shell commands cannot be
	// confined there, so the worker refuses to start unless --no-sandbox accepts
	// it. With --no-sandbox, on any system, one line says the commands are not
	// confined to --dir.
	if env.GOOS == "windows" && !l.opts.noSandbox {
		return usageMistake(tr.T("worker.no_sandbox_windows"))
	}
	if l.opts.noSandbox {
		fmt.Fprintln(l.stderr, "ulgen: "+tr.T("worker.no_sandbox"))
	}

	// The settings are built from Ülgen's own configuration directory and the
	// presets asked for, before anything is started.
	settings, f := l.sandboxSettings(env)
	if f != nil {
		return f
	}

	// The models are fetched and the translator is started, as in "ulgen run
	// claude".
	setupTr, key, f := setupTranslator(l.ctx, proxyOptions{model: l.opts.model, baseURL: l.opts.baseURL}, l.getenv, l.stderr, tr)
	if f != nil {
		return f
	}
	if l.ctx.Err() != nil {
		return cancelled(tr)
	}
	token, err := randomToken()
	if err != nil {
		return &failure{msg: tr.T("proxy.token_failed", oneLine(err.Error())), code: exitError}
	}
	srv, ln, err := startTranslator(setupTr, key, token, 0)
	if err != nil {
		return &failure{msg: tr.T("proxy.listen_failed", oneLine(err.Error())), code: exitError}
	}
	base := "http://" + ln.Addr().String()
	serve := serveTranslator
	go func() { _ = serve(srv, ln) }()

	if l.ctx.Err() != nil {
		if sf := stopTranslator(srv, tr); sf != nil {
			fmt.Fprintln(l.stderr, "ulgen: "+sf.msg)
		}
		return cancelled(tr)
	}

	// The child's environment: the worker passes ANTHROPIC_API_KEY (bare mode
	// reads the key variable) rather than ANTHROPIC_AUTH_TOKEN, and
	// CLAUDE_CODE_ATTRIBUTION_HEADER=0 rides along in claudeChildEnv.
	childEnv := claudeChildEnv(environ(), base, token, claudeEnvAPIKey, setupTr, env.GOOS == "windows")

	// The timeout: 0 means none. Closing stop ends the child the same way an
	// interrupt does; the child's own end cause tells which actually happened.
	stop := make(chan struct{})
	if l.opts.timeout > 0 {
		timer := time.AfterFunc(l.opts.timeout, func() { close(stop) })
		defer timer.Stop()
	}

	argv := append([]string{found.Binary}, workerArgs(l.opts, l.extraRules, setupTr.def, settings)...)
	done := make(chan workerEnd, 1)
	var cause *endCause
	go func() {
		end, c := startWorkerChild(l.ctx, found.Binary, argv, childEnv, dir, strings.NewReader(task), l.stderr, stop)
		cause = c
		done <- end
	}()
	res := <-done

	// The translator has no one left to serve; a wind-down failure is reported
	// but never replaces the child's own ending.
	if sf := stopTranslator(srv, tr); sf != nil {
		fmt.Fprintln(l.stderr, "ulgen: "+sf.msg)
	}

	// How it ended decides the exit code and the summary. The child's own end
	// cause is the source of truth: a child that finished by itself keeps its
	// code even when the deadline or an interrupt passed a moment later.
	signalled, interrupted := cause.result()
	switch {
	case res.err != nil && errors.Is(res.err, errChildOutputTooLarge):
		return &failure{msg: tr.T("worker.output_too_large", workerOutputLimit), code: exitError}
	case res.err != nil:
		return &failure{msg: tr.T("run.failed", tool.Name(), showPath(env.Home, found.Binary), oneLine(res.err.Error())), code: exitError}
	case signalled && interrupted:
		l.code = exitInterrupted
		l.summary("interrupted", res.out, setupTr.def)
	case signalled:
		l.code = exitTimeout
		l.summary("timed_out", res.out, setupTr.def)
	default:
		l.code = res.code
		if res.code == 0 {
			l.summary("finished", res.out, setupTr.def)
		} else {
			l.summary("failed", res.out, setupTr.def)
		}
	}
	return nil
}

// prepareOut checks --out before the child is started: the directory must
// exist and the file must be creatable. It is created (or truncated) with mode
// 0600, and an existing file's mode is set to 0600. A failure returns a
// message and exit 1, with nothing started.
func (l *workerLaunch) prepareOut() *failure {
	if l.opts.out == "" {
		return nil
	}
	if err := createWorkerOut(l.opts.out); err != nil {
		return &failure{msg: l.tr.T("worker.out_not_creatable", safeArg(l.opts.out), oneLine(err.Error())), code: exitError}
	}
	return nil
}

// readTask reads the task: standard input for "-", else the file. An empty
// task and one larger than workerMaxTask are refused, before anything is
// started.
func (l *workerLaunch) readTask() (string, *failure) {
	tr := l.tr
	var data []byte
	var err error
	if l.opts.task == "-" {
		data, err = io.ReadAll(io.LimitReader(l.stdin, workerMaxTask+1))
	} else {
		data, err = readTaskFile(l.opts.task)
	}
	if err != nil {
		return "", &failure{msg: tr.T("worker.task_unreadable", safeArg(l.opts.task), oneLine(err.Error())), code: exitError}
	}
	if len(data) > workerMaxTask {
		return "", usageMistake(tr.T("worker.task_too_large", workerSizeLimit))
	}
	task := string(data)
	if strings.TrimSpace(task) == "" {
		return "", usageMistake(tr.T("worker.task_empty"))
	}
	return task, nil
}

// readTaskFile reads a task file, applying the size bound as it reads so a
// huge file is not held in memory.
func readTaskFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, workerMaxTask+1))
}

// resolveDir returns the directory the worker runs in: --dir when given, else
// the working directory. The path is made absolute and its symbolic links are
// resolved; every check runs on the resolved path, and the resolved path is
// what the child gets as its working directory. It must exist, be a directory,
// and be neither a file system root, nor the user's home directory, nor an
// ancestor of it.
func (l *workerLaunch) resolveDir(env setup.Env) (string, *failure) {
	tr := l.tr
	dir := l.opts.dir
	if dir == "" {
		cwd, err := workingDir()
		if err != nil {
			return "", &failure{msg: tr.T("worker.dir_cwd", oneLine(err.Error())), code: exitError}
		}
		dir = cwd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", usageMistake(tr.T("worker.dir_no_dir", safeArg(dir)))
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", usageMistake(tr.T("worker.dir_missing", showPath(env.Home, abs)))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", usageMistake(tr.T("worker.dir_missing", showPath(env.Home, resolved)))
	}
	if !info.IsDir() {
		return "", usageMistake(tr.T("worker.dir_not_dir", showPath(env.Home, resolved)))
	}
	if key := workerDirTooBroad(env, resolved, info); key != "" {
		return "", usageMistake(tr.T(key, showPath(env.Home, resolved)))
	}
	return resolved, nil
}

// workerDirTooBroad returns the message key for a directory that is too much
// for a worker (a file system root, the home directory, or a directory that
// holds the home directory), or "" when the directory is a project. dir is
// already resolved and info is its os.Stat. The home directory is compared with
// os.SameFile, and its ancestors by walking up from the resolved home
// directory and comparing each step the same way.
func workerDirTooBroad(env setup.Env, dir string, info os.FileInfo) string {
	if isFilesystemRoot(dir) {
		return "worker.dir_root"
	}
	if env.Home == "" {
		return ""
	}
	home := env.Home
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	homeInfo, err := os.Stat(home)
	if err != nil {
		return ""
	}
	if os.SameFile(homeInfo, info) {
		return "worker.dir_home"
	}
	for p := home; ; {
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
		if pi, err := os.Stat(p); err == nil && os.SameFile(pi, info) {
			return "worker.dir_home_parent"
		}
	}
	return ""
}

// isFilesystemRoot reports whether dir is a file system root: "/" on Unix; on
// Windows a drive root such as "C:\" or a share root such as "\\host\share\".
func isFilesystemRoot(dir string) bool {
	if filepath.Dir(dir) == dir {
		return true
	}
	if vol := filepath.VolumeName(dir); vol != "" {
		return dir == vol || dir == vol+string(filepath.Separator)
	}
	return false
}

// summary prints the short report on Ülgen's standard output: how it ended,
// the seconds it took, the model, and the worker's final text. The child's
// standard output is written to --out, byte for byte, before the summary; it
// is never printed here.
func (l *workerLaunch) summary(status string, out []byte, model string) {
	tr := l.tr
	fmt.Fprintln(l.stdout, "ulgen: "+tr.T("worker.summary.status",
		tr.T("worker.status."+status), model, l.seconds()))

	if l.opts.out != "" {
		if err := writeWorkerOut(l.opts.out, out); err != nil {
			fmt.Fprintln(l.stderr, "ulgen: "+tr.T("worker.out_failed", safeArg(l.opts.out), oneLine(err.Error())))
			if l.code == 0 {
				l.code = exitError
			}
		}
	}

	report := parseWorkerOutput(out)
	if n := len(report.denials); n > 0 {
		fmt.Fprintln(l.stdout, "ulgen: "+tr.T("worker.denials", n))
		for i, d := range report.denials {
			if i == workerMaxDenialLines {
				break
			}
			fmt.Fprintln(l.stdout, "  "+d.line())
		}
	}
	if strings.TrimSpace(report.result) == "" {
		fmt.Fprintln(l.stdout, "ulgen: "+tr.T("worker.out_empty"))
		return
	}
	fmt.Fprintln(l.stdout, termsafe.Sanitize(report.result))
}

// seconds is how long the child ran, as a whole number of seconds, for the
// summary.
func (l *workerLaunch) seconds() int {
	if l.started.IsZero() {
		return 0
	}
	if d := now().Sub(l.started); d > 0 {
		return int(d.Seconds())
	}
	return 0
}

// writeWorkerOut writes the child's standard output to path unchanged, with
// owner-only permissions: it is the worker's whole answer.
func writeWorkerOut(path string, out []byte) error {
	return os.WriteFile(path, out, 0o600)
}

// workerMaxDenialLines is how many refused tool calls the summary lists.
const workerMaxDenialLines = 5

// workerDenialLimit is how many characters of a refused call's command the
// summary shows.
const workerDenialLimit = 80

// workerDenial is one entry of the child's "permission_denials": a tool call
// the allow-list refused.
type workerDenial struct {
	tool   string
	detail string // the shell tool's command or a file tool's path; else ""
}

// line is the denial as one sanitised summary line: the tool's name and, for
// the shell tool, the command (for a file tool, the path), cut to
// workerDenialLimit characters.
func (d workerDenial) line() string {
	name := cutRunes(safeArg(d.tool), workerValueLimit)
	if d.detail == "" {
		return name
	}
	return name + ": " + cutRunes(safeArg(d.detail), workerDenialLimit)
}

// cutRunes returns at most n characters of s.
func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// workerOutput is what the summary takes from the child's JSON document.
type workerOutput struct {
	result  string // the "result" member when it is a string, else ""
	denials []workerDenial
}

// parseWorkerOutput reads the child's JSON document leniently: output that is
// not JSON, a missing member, or a member of another shape gives the empty
// value for that part and never an error.
func parseWorkerOutput(out []byte) workerOutput {
	var res workerOutput
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		return res
	}
	var text string
	if err := json.Unmarshal(doc["result"], &text); err == nil {
		res.result = text
	}
	var items []json.RawMessage
	if err := json.Unmarshal(doc["permission_denials"], &items); err != nil {
		return res
	}
	for _, raw := range items {
		var item struct {
			ToolName  string          `json:"tool_name"`
			ToolInput json.RawMessage `json:"tool_input"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		d := workerDenial{tool: item.ToolName}
		var input struct {
			Command  string `json:"command"`
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(item.ToolInput, &input) == nil {
			if item.ToolName == "Bash" {
				d.detail = input.Command
			} else {
				d.detail = input.FilePath
			}
		}
		res.denials = append(res.denials, d)
	}
	return res
}

// workerArgs builds the child's arguments, exactly as the design's step 3 has
// them. chosen is the model the translator resolved (the same value the model
// variables carry); settings is the JSON for --settings (see workerSettings),
// passed as one argument.
func workerArgs(opts workerOptions, extraRules []string, chosen, settings string) []string {
	argv := []string{"--bare", "-p", "--model", chosen, "--permission-mode", "dontAsk"}
	argv = append(argv, "--allowedTools")
	argv = append(argv, workerAlwaysAllowed...)
	argv = append(argv, extraRules...)
	argv = append(argv, "--strict-mcp-config", "--output-format", "json", "--no-session-persistence")
	if opts.maxTurns > 0 {
		argv = append(argv, "--max-turns", fmt.Sprintf("%d", opts.maxTurns))
	}
	if settings != "" {
		argv = append(argv, "--settings", settings)
	}
	return argv
}
