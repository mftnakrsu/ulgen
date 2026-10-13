package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/proxy"
)

// errTestListen is the listener failure a worker test reports.
var errTestListen = errors.New("address in use")

// workerJSON is the shape of the child's standard output the tests answer
// with: one JSON document with a result member.
const workerJSON = `{"type":"result","subtype":"success","is_error":false,"result":"all done"}`

// TestSkillNamesOnlyWhatExists checks the installed skill against the worker
// command: every --option the skill mentions on a line that names "ulgen
// worker" must be an option of that command, and every preset it names must
// be one of the presets. The skill instructs an orchestrating model, so it
// may name only options that are really there.
func TestSkillNamesOnlyWhatExists(t *testing.T) {
	text := claudeSkill()
	if len(text) == 0 {
		t.Fatal("this build carries no EVREN workers skill")
	}
	fs := workerFlagSet(&workerOptions{}, i18n.New("en"))
	options := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { options[f.Name] = true })

	named := map[string]bool{}
	for i, line := range strings.Split(string(text), "\n") {
		if !strings.Contains(line, "ulgen worker") {
			continue
		}
		for _, opt := range optionTokens(line) {
			name := strings.TrimLeft(opt, "-")
			if !options[name] {
				t.Errorf("line %d names %q, which is not an option of ulgen worker:\n%s", i+1, opt, line)
			}
			named[name] = true
		}
	}
	// The check must be looking at real options, not passing vacuously: the
	// skill's example command line names at least --dir and --allow.
	for _, want := range []string{"dir", "allow"} {
		if !named[want] {
			t.Errorf("the skill names no --%s on a line that mentions ulgen worker; is the test still looking at the skill?", want)
		}
	}
	for _, name := range workerPresetNames() {
		if !strings.Contains(string(text), name) {
			t.Errorf("the skill does not name the preset %q", name)
		}
	}
}

// optionTokens returns the --options of a line: the words that begin with
// "--" and are followed by a letter, with any "=<value>" tail removed.
func optionTokens(line string) []string {
	var out []string
	for _, word := range strings.Fields(line) {
		word = strings.Trim(word, "`.,;:()[]\"'")
		if !strings.HasPrefix(word, "--") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(word, "--"), "=")
		if name == "" || name[0] == '-' {
			continue
		}
		out = append(out, "--"+name)
	}
	return out
}

// startedWorker records how the worker would have started the child, and
// answers with the output, exit code and delay a test sets. It is the worker's
// version of startedChild: it also records the working directory and the
// standard input.
type startedWorker struct {
	binary string
	argv   []string
	env    []string
	dir    string
	stdin  string
	calls  int

	out          []byte
	code         int
	err          error
	delay        time.Duration
	ignoreStop   bool   // the child ends by itself after delay, not by the stop signal
	hook         func() // run at the start of the child, for cancel tests
	stop         <-chan struct{}
	endedByStop  bool
	returnedCode int
}

// useFakeWorkerChild records how the worker would start the child instead of
// starting it. environ is given a small parent environment that holds the key
// variable, so a test sees whether the child's environment drops it. The
// caller sets out, code, err and delay on the returned value before the run.
func useFakeWorkerChild(t *testing.T, code int, out []byte, err error) *startedWorker {
	t.Helper()
	s := &startedWorker{code: code, out: out, err: err}
	savedChild, savedEnv := startWorkerChild, environ
	startWorkerChild = func(ctx context.Context, binary string, argv, env []string, dir string, stdin io.Reader, stderr io.Writer, stop <-chan struct{}) (workerEnd, *endCause) {
		s.binary, s.argv, s.env, s.dir = binary, argv, env, dir
		if b, e := io.ReadAll(stdin); e == nil {
			s.stdin = string(b)
		}
		s.calls++
		s.stop = stop
		if s.hook != nil {
			s.hook()
		}
		cause := &endCause{}
		if s.delay > 0 {
			timer := time.NewTimer(s.delay)
			defer timer.Stop()
			if s.ignoreStop {
				// a child that ends by itself, whatever the deadline does
				<-timer.C
			} else {
				select {
				case <-timer.C:
				case <-stop:
					s.endedByStop = true
					cause.record(false)
				case <-ctx.Done():
					s.endedByStop = true
					cause.record(true)
				}
			}
		}
		s.returnedCode = s.code
		return workerEnd{code: s.code, out: s.out, err: s.err}, cause
	}
	environ = func() []string { return []string{"PATH=/usr/bin", "EVREN_API_KEY=" + testKey, "LANG=C"} }
	t.Cleanup(func() { startWorkerChild, environ = savedChild, savedEnv })
	return s
}

// writeTask writes a task file and returns its path.
func writeTask(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// workerServer starts the stub EVREN and the fake translator a worker test
// needs, and returns the stub's URL.
func workerServer(t *testing.T) string {
	t.Helper()
	srv, _ := evrenServer(t, chatRoutes())
	useFakeTranslator(t, 41234)
	return srv.URL
}

// The child's arguments are exactly the design's step 3, with the always
// -allowed tools and the file tools of step 3, and the sandbox on.
func TestWorkerChildArguments(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	n := len(s.argv)
	if n < 2 || s.argv[n-2] != "--settings" {
		t.Fatalf("argv = %q, want --settings last", s.argv)
	}
	settings := s.argv[n-1] // one argument, the JSON
	want := []string{testBin("claude"), "--bare", "-p", "--model", "deepseek-v4.1-flash",
		"--permission-mode", "dontAsk", "--allowedTools",
		"Edit(./**)",
		"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)",
		"Bash(ls:*)", "Bash(mkdir:*)",
		"--strict-mcp-config", "--output-format", "json", "--no-session-persistence",
		"--settings", settings}
	if strings.Join(s.argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q\nwant   %q", s.argv, want)
	}
	if !json.Valid([]byte(settings)) {
		t.Errorf("--settings is not JSON: %q", settings)
	}
	if s.binary != testBin("claude") {
		t.Errorf("binary = %q", s.binary)
	}
}

// --max-turns and --no-sandbox change the arguments as the design says.
func TestWorkerArgumentsMaxTurnsAndNoSandbox(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url,
		"--max-turns", "5", "--no-sandbox", task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	joined := strings.Join(s.argv, " ")
	if !strings.Contains(joined, "--max-turns 5") {
		t.Errorf("no --max-turns in %q", joined)
	}
	n := len(s.argv)
	if s.argv[n-2] != "--settings" || strings.Contains(s.argv[n-1], `"sandbox"`) || !strings.Contains(s.argv[n-1], `"deny":["Read(`) {
		t.Errorf("--no-sandbox: want settings with the read rules and no sandbox, got %q", s.argv)
	}
}

// On Windows the settings hold no sandbox, only the read rules.
func TestWorkerWindowsHasNoSandbox(t *testing.T) {
	env := useClaude(t, nil)
	env.GOOS = "windows"
	useEnv(t, env)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--no-sandbox", task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	n := len(s.argv)
	if s.argv[n-2] != "--settings" || strings.Contains(s.argv[n-1], `"sandbox"`) || !strings.Contains(s.argv[n-1], `"deny":["Read(`) {
		t.Errorf("Windows: want settings with the read rules and no sandbox, got %q", s.argv)
	}
}

// On Windows without --no-sandbox the worker refuses to start, in both
// languages, and says why, what accepts it, and where the sandbox works.
func TestWorkerWindowsRefusesWithoutNoSandbox(t *testing.T) {
	env := useClaude(t, nil)
	env.GOOS = "windows"
	useEnv(t, env)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != exitUsage || stdout != "" || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	for _, w := range []string{"cannot be confined", "--no-sandbox", "WSL2"} {
		if !strings.Contains(stderr, w) {
			t.Errorf("stderr lacks %q: %q", w, stderr)
		}
	}
	vars["LANG"] = "tr_TR.UTF-8"
	code, _, stderr = exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != exitUsage || s.calls != 0 || !strings.Contains(stderr, "--no-sandbox") || !strings.Contains(stderr, "WSL2") || !strings.Contains(stderr, "kısıtlanamaz") {
		t.Errorf("Turkish: code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// With --no-sandbox, on any system, one line on standard error says the
// worker's shell commands are not confined to its directory; without it on a
// system with a sandbox there is no such line.
func TestWorkerNoSandboxNotice(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		env := useClaude(t, nil)
		env.GOOS = goos
		useEnv(t, env)
		vars := signedIn(t)
		url := workerServer(t)
		s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
		task := writeTask(t, "do the thing")

		code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--no-sandbox", task)
		if code != 0 || s.calls != 1 || strings.Count(stderr, "not confined to its directory") != 1 {
			t.Errorf("%s: code %d stderr %q, %d starts", goos, code, stderr, s.calls)
		}
	}
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	_, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, writeTask(t, "x"))
	if strings.Contains(stderr, "not confined") {
		t.Errorf("a sandboxed worker printed the notice: %q", stderr)
	}
}

// --allow takes presets and rules; a preset expands into its rules in order.
func TestWorkerAllowPresetAndRule(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	useGoEnv(t, nil)
	task := writeTask(t, "go")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url,
		"--allow", "go", "--allow", "Bash(make:*)", task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	joined := strings.Join(s.argv, "\x00")
	for _, want := range []string{"Bash(go:*)", "Bash(gofmt:*)", "Bash(GOOS=windows go:*)", "Bash(GOOS=linux go:*)", "Bash(GOOS=darwin go:*)", "Bash(make:*)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the rules lack %q:\n%q", want, joined)
		}
	}
}

// Every --allow value of the review's finding 1, and the shells and command
// runners of point A, is refused with a usage error and nothing starts; the
// message names the value's position and the reason.
func TestWorkerRefusesBadAllowValues(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	useGoEnv(t, nil)
	task := writeTask(t, "go")
	const (
		shape  = "is not one permission rule"
		tool   = "names no tool the worker knows"
		edit   = "already edits files under its directory"
		spec   = "without a command specifier"
		any    = "run any shell command"
		runner = "shell or a command runner"
		op     = "joins or redirects shell commands"
	)
	for _, tc := range []struct{ value, want string }{
		// the three exact forms of the design
		{"Bash", spec}, {"Bash(*)", any}, {"Bash(:*)", any},
		// the review's list
		{"--dangerously-skip-permissions", shape}, {"-x", shape}, {"--add-dir", shape},
		{"--permission-mode", shape}, {"Read Bash", shape}, {"Read\nBash", shape},
		{"Bash ", shape}, {"Bash( * )", any}, {"Bash(*:*)", any}, {"Bash( :* )", any},
		{"bash", tool}, {"BASH(*)", tool}, {"mcp__*", shape}, {"Bash(sh:*)", runner},
		{"Bash(bash -c:*)", runner}, {"Bash(env:*)", runner}, {"Bash(xargs:*)", runner},
		{"Bash(**)", any}, {"Bash(* )", any}, {"Bash(\n*)", shape}, {"Bash(*", shape},
		{"", shape}, {"Foo bar baz", shape}, {"Bash()", any},
		// more shapes and tools
		{"Read(./a) x", shape}, {"Read,Write", shape}, {"Read\x1b[31m", shape},
		{"Glob", tool}, {"Foo(x)", tool}, {"Reads", tool},
		// the shells and runners, also with a path, variables and arguments
		{"Bash(/bin/sh:*)", runner}, {"Bash(zsh -c x)", runner}, {"Bash(FOO=1 sh -c x)", runner},
		{"Bash(A=1 B=2 /usr/bin/env x)", runner}, {"Bash(sudo ls)", runner}, {"Bash(time ls)", runner},
		{"Bash(powershell.exe:*)", runner}, {"Bash(\\Windows\\System32\\cmd.exe /c x)", runner},
		{"Bash(busybox sh)", runner}, {"Bash(nohup x)", runner}, {"Bash(eval x)", runner},
		{"Bash(exec x)", runner}, {"Bash(doas x)", runner}, {"Bash(osascript -e x)", runner},
		{"Bash(pwsh:*)", runner}, {"Bash(command -v x)", runner}, {"Bash(su x)", runner},
		{"Bash(dash:*)", runner}, {"Bash(ksh:*)", runner}, {"Bash(fish:*)", runner},
		{"Bash(csh:*)", runner}, {"Bash(tcsh:*)", runner}, {"Bash(cmd:*)", runner},
		// one value that Claude Code could read as two rules
		{"Read(./a),Bash(*)", shape}, {"Bash(git status) Bash(*)", shape}, {"Read(./a) Bash(sh:*)", shape},
		{"Read(./a(b))", shape}, {"Bash(git log --format=%h,%s)", shape}, {"Bash(echo $(id))", shape},
		{"Bash(ls\u2028*)", shape}, {"Bash(ls\t-l)", shape},
		// the editing tools cannot be widened
		{"Edit", edit}, {"Write", edit}, {"Edit(//**)", edit}, {"Write(~/x)", edit}, {"MultiEdit", edit},
		{"NotebookEdit(./**)", edit},
		// a runner hidden behind a variable with a colon, quotation marks, a drive or .exe
		{"Bash(PATH=/a:/b sh -c x)", runner}, {"Bash(\"sh\" -c x)", runner}, {"Bash('bash':*)", runner},
		{"Bash(C:\\Windows\\System32\\cmd.exe /c x)", runner}, {"Bash(C:/tools/bash.exe -c x)", runner},
		{"Bash(BASH.EXE:*)", runner}, {"Bash(timeout 5 x)", runner}, {"Bash(nice x)", runner},
		{"Bash(wsl x)", runner},
		// a pattern where the command's name belongs, or no name at all
		{"Bash(*sh:*)", any}, {"Bash(s*:*)", any}, {"Bash(* --version)", any}, {"Bash(FOO=1)", any},
		{"Bash(FOO=1 *)", any}, {"Bash(/bin/?sh x)", any}, {"Bash([bs]h x)", any},
		// shell operators
		{"Bash(git status; sh)", op}, {"Bash(git status && rm x)", op}, {"Bash(cat x | sh)", op},
		{"Bash(echo x > y)", op}, {"Bash(git log `id`)", op}, {"Bash(sort < x)", op},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url,
			"--allow", "go", "--allow", tc.value, task)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: the second --allow value ") ||
			!strings.Contains(stderr, tc.want) {
			t.Errorf("--allow %q: code %d stdout %q stderr %q, want %q", tc.value, code, stdout, stderr, tc.want)
		}
		if strings.ContainsAny(stderr, "\x1b\r") {
			t.Errorf("--allow %q: the message holds a control character: %q", tc.value, stderr)
		}
	}
	if s.calls != 0 {
		t.Errorf("a refused worker started the child")
	}
}

// A refused value is repeated for at most 40 characters, and the Turkish
// message names the position too.
func TestWorkerAllowMessageEchoesLittle(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	useGoEnv(t, nil)
	task := writeTask(t, "go")
	long := "-" + strings.Repeat("abcdefghij", 10)
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--allow", long, task)
	if code != exitUsage || !strings.Contains(stderr, long[:40]) || strings.Contains(stderr, long[:41]) {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	vars["LANG"] = "tr_TR.UTF-8"
	code, _, stderr = exec(t, context.Background(), vars, "worker", "--base-url", url, "--allow", "go", "--allow", "-x", task)
	if code != exitUsage || !strings.Contains(stderr, "ikinci --allow değeri") || s.calls != 0 {
		t.Errorf("Turkish: code %d stderr %q", code, stderr)
	}
}

// The values that are accepted: one rule each, passed on as one argument.
func TestWorkerAcceptsGoodAllowValues(t *testing.T) {
	good := []string{"Bash(make test:*)", "Bash(GOOS=windows go:*)", "Read(./docs/**)", "WebSearch",
		"Bash(npm run build)", "Read", "Read(~/refs/**)", "WebFetch(domain:example.com)",
		"Bash(git push:*)", "Bash(./scripts/ci.sh)", "Bash(git * main)", "Bash(CGO_ENABLED=0 GOOS=linux go build:*)",
		"Bash(C:/tools/make.exe test)", "Bash(shellcheck:*)", "Bash(envsubst:*)"}
	for _, v := range good {
		if key := workerAllowProblem(v); key != "" {
			t.Errorf("%q refused: %s", v, key)
		}
	}
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "go")
	args := []string{"worker", "--base-url", url}
	for _, v := range good {
		args = append(args, "--allow", v)
	}
	code, _, stderr := exec(t, context.Background(), vars, append(args, task)...)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	for _, v := range good {
		found := false
		for _, a := range s.argv {
			found = found || a == v
		}
		if !found {
			t.Errorf("%q is not an argument of its own: %q", v, s.argv)
		}
	}
}

// The child's environment carries the token as ANTHROPIC_API_KEY, not
// ANTHROPIC_AUTH_TOKEN, never the EVREN key, and the attribution switch.
func TestWorkerChildEnvironment(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	joined := strings.Join(s.env, "\n")
	if !strings.Contains(joined, "ANTHROPIC_API_KEY=") {
		t.Errorf("no ANTHROPIC_API_KEY:\n%s", joined)
	}
	if strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=") {
		t.Errorf("the worker set ANTHROPIC_AUTH_TOKEN:\n%s", joined)
	}
	if !strings.Contains(joined, "CLAUDE_CODE_ATTRIBUTION_HEADER=0") {
		t.Errorf("no attribution switch:\n%s", joined)
	}
	if strings.Contains(joined, "EVREN_API_KEY=") || strings.Contains(joined, testKey) {
		t.Errorf("the EVREN key reached the child:\n%s", joined)
	}
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=http://127.0.0.1:") {
		t.Errorf("no local base URL:\n%s", joined)
	}
}

// The task comes from a file, and is handed to the child on its standard
// input unchanged.
func TestWorkerTaskFromFile(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "line one\nline two\n")

	if code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task); code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if s.stdin != "line one\nline two\n" {
		t.Errorf("child stdin = %q", s.stdin)
	}
}

// The task comes from standard input for "-".
func TestWorkerTaskFromStdin(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)

	code, _, stderr := execIn(t, context.Background(), vars, "from stdin\n", "worker", "--base-url", url, "-")
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	if s.stdin != "from stdin\n" {
		t.Errorf("child stdin = %q", s.stdin)
	}
}

// An empty task is a usage mistake; nothing is fetched.
func TestWorkerEmptyTask(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, rec := evrenServer(t, chatRoutes())
	useFakeTranslator(t, 41234)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "   \n")
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", srv.URL, task)
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: the task is empty") || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	if len(rec.paths()) != 0 {
		t.Errorf("something was fetched: %v", rec.paths())
	}
}

// A task over 1 MB is a usage mistake.
func TestWorkerTaskTooLarge(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	useFakeTranslator(t, 41234)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, strings.Repeat("x", workerMaxTask+10))
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", srv.URL, task)
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: the task is larger than 1 MB") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// --dir must exist, be a directory, and be a project: a missing path, a file,
// the home directory and a file system root are each refused.
func TestWorkerDirChecks(t *testing.T) {
	env := useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)

	links := t.TempDir()
	link := func(name, target string) string {
		path := filepath.Join(links, name)
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("cannot make a symbolic link here: %v", err)
		}
		return path
	}

	for _, tc := range []struct {
		name string
		dir  string
		want string
	}{
		{"missing", filepath.Join(t.TempDir(), "nope"), "does not exist"},
		{"a file", file, "is not a directory"},
		{"the home directory", env.Home, "is your home directory"},
		{"the home directory with a trailing dot", filepath.Join(env.Home, "."), "is your home directory"},
		{"a root", root, "is a file system root"},
		{"the parent of home", filepath.Dir(env.Home), "holds your home directory"},
		{"the grandparent of home", filepath.Dir(filepath.Dir(env.Home)), "holds your home directory"},
		{"a link to the root", link("root", root), "is a file system root"},
		{"a link to home", link("home", env.Home), "is your home directory"},
		{"a link to the parent of home", link("parent", filepath.Dir(env.Home)), "holds your home directory"},
		{"a link to nothing", link("dangling", filepath.Join(links, "gone")), "does not exist"},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--dir", tc.dir, task)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: ") || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: code %d stdout %q stderr %q", tc.name, code, stdout, stderr)
		}
	}
	if s.calls != 0 {
		t.Errorf("a wrongly directed worker started the child")
	}
}

// The child runs in --dir, which is passed on.
func TestWorkerRunsInDir(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	dir := t.TempDir()
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--dir", dir, task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	if want, err := filepath.EvalSymlinks(dir); err != nil || s.dir != want {
		t.Errorf("child dir = %q, want %q (%v)", s.dir, want, err)
	}
}

// A project directory given through a symbolic link is accepted, and the
// child gets the resolved path, not the link.
func TestWorkerDirThroughLinkIsResolved(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	project := t.TempDir()
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(project, link); err != nil {
		t.Skipf("cannot make a symbolic link here: %v", err)
	}
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--dir", link, task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if s.dir != want || s.dir == link {
		t.Errorf("child dir = %q, want the resolved %q", s.dir, want)
	}
}

// A timeout ends the child, gives the exit code 124, and the summary says so.
func TestWorkerTimeout(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	s.delay = 5 * time.Second
	task := writeTask(t, "do the thing")

	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--timeout", "200ms", task)
	if code != exitTimeout {
		t.Errorf("code %d, want %d (stderr %q)", code, exitTimeout, stderr)
	}
	if !s.endedByStop {
		t.Errorf("the timeout did not end the child")
	}
	if !strings.Contains(stdout, "worker timed out in") || !strings.Contains(stdout, "all done") {
		t.Errorf("summary %q", stdout)
	}
}

// An interrupt ends the child the same way and gives 130.
func TestWorkerInterrupted(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	s.delay = 5 * time.Second
	task := writeTask(t, "do the thing")

	ctx, cancel := context.WithCancel(context.Background())
	s.hook = cancel
	code, stdout, stderr := exec(t, ctx, vars, "worker", "--base-url", url, task)
	if code != exitInterrupted {
		t.Errorf("code %d, want %d (stderr %q)", code, exitInterrupted, stderr)
	}
	if !s.endedByStop {
		t.Errorf("the interrupt did not end the child")
	}
	if !strings.Contains(stdout, "worker interrupted in") {
		t.Errorf("summary %q", stdout)
	}
}

// --out holds the child's JSON byte for byte.
func TestWorkerOutFile(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	out := []byte(workerJSON)
	s := useFakeWorkerChild(t, 0, out, nil)
	task := writeTask(t, "do the thing")
	outFile := filepath.Join(t.TempDir(), "result.json")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--out", outFile, task)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(out) {
		t.Errorf("out file = %q, want %q", got, out)
	}
}

// The summary carries the result member when there is one, and none when
// there is not; output that is not JSON still ends with a summary.
func TestWorkerSummary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     int
		out      []byte
		status   string
		wantText string
	}{
		{"a result member", 0, []byte(workerJSON), "finished", "all done"},
		{"no result member", 0, []byte(`{"type":"result","is_error":false}`), "finished", "the worker printed no result"},
		{"not JSON", 0, []byte("plain text output"), "finished", "the worker printed no result"},
		{"empty output", 0, nil, "finished", "the worker printed no result"},
		{"a failure code", 3, []byte(workerJSON), "failed", "all done"},
	} {
		useClaude(t, nil)
		vars := signedIn(t)
		url := workerServer(t)
		s := useFakeWorkerChild(t, tc.code, tc.out, nil)
		task := writeTask(t, "do the thing")

		code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
		if code != tc.code {
			t.Errorf("%s: code %d stderr %q", tc.name, code, stderr)
		}
		wantStatus := "ulgen: worker " + tc.status + " in "
		if !strings.HasPrefix(stdout, wantStatus) {
			t.Errorf("%s: summary %q, want it to start %q", tc.name, stdout, wantStatus)
		}
		if !strings.Contains(stdout, tc.wantText) {
			t.Errorf("%s: summary %q lacks %q", tc.name, stdout, tc.wantText)
		}
		if s.calls != 1 {
			t.Errorf("%s: %d starts", tc.name, s.calls)
		}
	}
}

// runWorkerWith runs a worker whose fake child answers code and out, with the
// extra arguments before the task, and returns the exit code and both streams.
func runWorkerWith(t *testing.T, code int, out []byte, extra ...string) (int, string, string, *startedWorker) {
	t.Helper()
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, code, out, nil)
	args := append([]string{"worker", "--base-url", url}, extra...)
	c, stdout, stderr := exec(t, context.Background(), vars, append(args, writeTask(t, "do the thing"))...)
	return c, stdout, stderr, s
}

// The worker's final text is sanitised before it is printed: no escape
// sequence reaches the terminal.
func TestWorkerSummarySanitisesResult(t *testing.T) {
	out := []byte(`{"result":"fine \u001b[31mred\u001b[0m \u001b]0;title\u0007done\r\nnext\u0000"}`)
	code, stdout, stderr, _ := runWorkerWith(t, 0, out)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if strings.ContainsAny(stdout, "\x1b\x07\r\x00") || !strings.Contains(stdout, "fine red done\nnext") {
		t.Errorf("stdout = %q", stdout)
	}
}

// Refused tool calls are counted and up to five are listed, each sanitised and
// cut to 80 characters; a missing or oddly shaped member prints nothing.
func TestWorkerSummaryDenials(t *testing.T) {
	long := strings.Repeat("x", 120)
	denials := `[` +
		`{"tool_name":"Bash","tool_input":{"command":"git push origin main"}},` +
		`{"tool_name":"Bash","tool_input":{"command":"curl \u001b[31mevil\u001b[0m\nsecond line"}},` +
		`{"tool_name":"Bash","tool_input":{"command":"` + long + `"}},` +
		`{"tool_name":"Read","tool_input":{"file_path":"/inv/home/.ssh/key\u001b[0m"}},` +
		`{"tool_name":"Write","tool_input":"not an object"},` +
		`{"tool_name":"Bash","tool_input":{"command":"sixth"}},` +
		`{"tool_name":"Bash","tool_input":{"command":"seventh"}}]`
	out := []byte(`{"result":"all done","permission_denials":` + denials + `}`)
	code, stdout, stderr, _ := runWorkerWith(t, 0, out)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	// status, count, five denials, result
	if len(lines) != 8 || lines[1] != "ulgen: 7 tool calls were refused by the worker's permissions" {
		t.Fatalf("stdout = %q", stdout)
	}
	if lines[2] != "  Bash: git push origin main" {
		t.Errorf("first denial = %q", lines[2])
	}
	if strings.ContainsAny(stdout, "\x1b") || lines[3] != "  Bash: curl evil second line" {
		t.Errorf("second denial = %q", lines[3])
	}
	if lines[4] != "  Bash: "+long[:80] {
		t.Errorf("third denial = %q", lines[4])
	}
	if lines[5] != "  Read: /inv/home/.ssh/key" || lines[6] != "  Write" || lines[7] != "all done" {
		t.Errorf("the rest = %q", lines[5:])
	}

	for _, raw := range []string{
		`{"result":"ok"}`,
		`{"result":"ok","permission_denials":[]}`,
		`{"result":"ok","permission_denials":"none"}`,
		`{"result":"ok","permission_denials":{"a":1}}`,
		`{"result":"ok","permission_denials":null}`,
	} {
		_, stdout, _, _ := runWorkerWith(t, 0, []byte(raw))
		if strings.Contains(stdout, "refused") {
			t.Errorf("%s: stdout = %q", raw, stdout)
		}
	}

	_, stdout, _, _ = runWorkerWith(t, 0, []byte(`{"result":"ok","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"ls"}}]}`), "--no-sandbox")
	if !strings.Contains(stdout, "ulgen: 1 tool calls were refused") {
		t.Errorf("stdout = %q", stdout)
	}
}

// The Turkish summary names the refused calls too.
func TestWorkerSummaryDenialsTurkish(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	vars["LANG"] = "tr_TR.UTF-8"
	url := workerServer(t)
	useFakeWorkerChild(t, 0, []byte(`{"result":"x","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"ls"}}]}`), nil)
	_, stdout, _ := exec(t, context.Background(), vars, "worker", "--base-url", url, writeTask(t, "x"))
	if !strings.Contains(stdout, "1 araç çağrısı işçinin izinleri nedeniyle reddedildi") {
		t.Errorf("stdout = %q", stdout)
	}
}

// --out is created before the child starts, with mode 0600 (an existing file
// is truncated and set to 0600), and holds the child's output byte for byte,
// also when it is empty or not JSON.
func TestWorkerOutIsPreparedAndExact(t *testing.T) {
	for _, out := range [][]byte{[]byte(workerJSON), nil, []byte("not json \x00\xff\n")} {
		dir := t.TempDir()
		outFile := filepath.Join(dir, "result.json")
		if err := os.WriteFile(outFile, []byte("old content that is longer"), 0o644); err != nil {
			t.Fatal(err)
		}
		var modeAtStart os.FileMode
		var sizeAtStart int64 = -1
		useClaude(t, nil)
		vars := signedIn(t)
		url := workerServer(t)
		s := useFakeWorkerChild(t, 0, out, nil)
		s.hook = func() {
			if fi, err := os.Stat(outFile); err == nil {
				modeAtStart, sizeAtStart = fi.Mode().Perm(), fi.Size()
			}
		}
		code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--out", outFile, writeTask(t, "x"))
		if code != 0 {
			t.Fatalf("code %d stderr %q", code, stderr)
		}
		got, err := os.ReadFile(outFile)
		if err != nil || string(got) != string(out) {
			t.Errorf("out file = %q (%v), want %q", got, err, out)
		}
		if runtime.GOOS != "windows" {
			if modeAtStart != 0o600 || sizeAtStart != 0 {
				t.Errorf("at the child's start: mode %v size %d, want 0600 and 0", modeAtStart, sizeAtStart)
			}
			if fi, err := os.Stat(outFile); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("final mode = %v (%v)", fi.Mode().Perm(), err)
			}
		}
	}
}

// A new --out file is created with mode 0600 as well.
func TestWorkerOutNewFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes here")
	}
	outFile := filepath.Join(t.TempDir(), "new.json")
	if code, _, stderr, _ := runWorkerWith(t, 0, []byte(workerJSON), "--out", outFile); code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if fi, err := os.Stat(outFile); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v (%v)", fi.Mode().Perm(), err)
	}
}

// An --out that cannot be created ends the command with exit 1 and a message,
// and nothing is started: a directory that is not there, and a path that is a
// directory.
func TestWorkerOutNotCreatable(t *testing.T) {
	for _, outFile := range []string{
		filepath.Join(t.TempDir(), "no-such-dir", "result.json"),
		t.TempDir(),
	} {
		code, stdout, stderr, s := runWorkerWith(t, 0, []byte(workerJSON), "--out", outFile)
		if code != exitError || stdout != "" || s.calls != 0 || !strings.HasPrefix(stderr, "ulgen: the --out file ") {
			t.Errorf("%s: code %d stdout %q stderr %q, %d starts", outFile, code, stdout, stderr, s.calls)
		}
	}
}

// When the output cannot be written after the run, the message goes to
// standard error and a run that would have given 0 gives 1; a failing child's
// own code is kept.
func TestWorkerOutWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a file cannot be made read-only the same way here")
	}
	for _, tc := range []struct{ childCode, want int }{{0, exitError}, {3, 3}} {
		outFile := filepath.Join(t.TempDir(), "result.json")
		useClaude(t, nil)
		vars := signedIn(t)
		url := workerServer(t)
		s := useFakeWorkerChild(t, tc.childCode, []byte(workerJSON), nil)
		s.hook = func() { // the file becomes unwritable while the child runs
			_ = os.Remove(outFile)
			_ = os.Mkdir(outFile, 0o700)
		}
		code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--out", outFile, writeTask(t, "x"))
		if code != tc.want || !strings.Contains(stderr, "could not be written") || !strings.Contains(stdout, "all done") {
			t.Errorf("child %d: code %d stdout %q stderr %q", tc.childCode, code, stdout, stderr)
		}
	}
}

// A child that ended by itself keeps its own code even when the deadline
// passes a moment later.
func TestWorkerDeadlineAfterNormalEndKeepsTheCode(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 3, []byte(workerJSON), nil)
	s.delay = 400 * time.Millisecond
	s.ignoreStop = true
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--timeout", "100ms", writeTask(t, "x"))
	if code != 3 || !strings.Contains(stdout, "worker failed in") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}

	// and the same for a child that ended with 0 while the context ended
	s = useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	ctx, cancel := context.WithCancel(context.Background())
	s.hook = cancel
	s.delay = 50 * time.Millisecond
	s.ignoreStop = true
	code, stdout, _ = exec(t, ctx, vars, "worker", "--base-url", url, writeTask(t, "x"))
	if code != 0 || !strings.Contains(stdout, "worker finished in") {
		t.Errorf("interrupt after a normal end: code %d stdout %q", code, stdout)
	}
}

// The caller's ANTHROPIC_AUTH_TOKEN does not reach the worker's child, and the
// real-looking value appears nowhere in it.
func TestWorkerDropsCallersAuthToken(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	environ = func() []string {
		return []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=invented-caller-token", "ANTHROPIC_API_KEY=invented-old-key"}
	}
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, writeTask(t, "x"))
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	joined := strings.Join(s.env, "\n")
	if strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN") || strings.Contains(joined, "invented-") {
		t.Errorf("the caller's credentials reached the child:\n%s", joined)
	}
	if strings.Count(joined, "ANTHROPIC_API_KEY=") != 1 {
		t.Errorf("want exactly one ANTHROPIC_API_KEY:\n%s", joined)
	}
}

// inventedPath returns an absolute path that exists nowhere, made of the
// given parts, in the form of the system the test runs on (on Windows a path
// is absolute only with a drive).
func inventedPath(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{string(filepath.Separator)}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// settingsOf decodes a --settings value into the sandbox's deny list, its
// write list and the sandbox object itself.
func settingsOf(t *testing.T, raw string) (deny, allow []string, sb map[string]any) {
	t.Helper()
	var doc struct {
		Sandbox map[string]any `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("settings are not JSON: %v\n%s", err, raw)
	}
	fs, _ := doc.Sandbox["filesystem"].(map[string]any)
	list := func(key string) []string {
		var out []string
		items, _ := fs[key].([]any)
		for _, v := range items {
			out = append(out, v.(string))
		}
		return out
	}
	return list("denyRead"), list("allowWrite"), doc.Sandbox
}

// The sandbox settings: the three switches, the deny list under home and
// Ülgen's own directory, and the writable extras of the presets.
func TestWorkerSandboxSettings(t *testing.T) {
	home := inventedPath(t, "home", "invented")
	invCache, invMod := inventedPath(t, "inv", "cache"), inventedPath(t, "inv", "mod")
	cfg := filepath.Join(home, ".config", "ulgen")
	contains := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}

	t.Run("no preset", func(t *testing.T) {
		useGoEnv(t, map[string]string{"GOCACHE": inventedPath(t, "never")})
		raw, err := workerSettings(home, cfg, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		deny, allow, sb := settingsOf(t, raw)
		if sb["enabled"] != true || sb["allowUnsandboxedCommands"] != false || sb["failIfUnavailable"] != true {
			t.Errorf("switches = %v", sb)
		}
		if len(allow) != 0 || strings.Contains(raw, "allowWrite") {
			t.Errorf("a worker with no preset may write elsewhere: %s", raw)
		}
		for _, rel := range []string{".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".netrc", ".npmrc",
			".pypirc", ".git-credentials", filepath.Join(".config", "gh"), filepath.Join(".config", "gcloud"),
			".claude", ".codex", ".qwen", filepath.Join(".config", "opencode"), filepath.Join("Library", "Keychains")} {
			if !contains(deny, filepath.Join(home, rel)) {
				t.Errorf("the deny list lacks %s: %v", rel, deny)
			}
		}
		if !contains(deny, cfg) {
			t.Errorf("the deny list lacks Ülgen's configuration directory %s: %v", cfg, deny)
		}
		for _, p := range deny {
			if !filepath.IsAbs(p) {
				t.Errorf("relative path in the deny list: %s", p)
			}
		}
	})

	t.Run("go with two answers", func(t *testing.T) {
		asked := useGoEnv(t, map[string]string{"GOCACHE": invCache, "GOMODCACHE": invMod})
		raw, _ := workerSettings(home, cfg, workerWritablePaths(home, []string{"go"}), true)
		_, allow, _ := settingsOf(t, raw)
		if strings.Join(allow, ",") != invCache+","+invMod || strings.Join(*asked, ",") != "GOCACHE,GOMODCACHE" {
			t.Errorf("allow = %v, asked %v", allow, *asked)
		}
	})

	t.Run("go with one answer", func(t *testing.T) {
		useGoEnv(t, map[string]string{"GOMODCACHE": invMod})
		raw, _ := workerSettings(home, cfg, workerWritablePaths(home, []string{"go"}), true)
		_, allow, _ := settingsOf(t, raw)
		if strings.Join(allow, ",") != invMod {
			t.Errorf("allow = %v", allow)
		}
	})

	t.Run("go with errors, empty and relative answers", func(t *testing.T) {
		useGoEnv(t, nil)
		raw, _ := workerSettings(home, cfg, workerWritablePaths(home, []string{"go"}), true)
		_, allow, _ := settingsOf(t, raw)
		if len(allow) != 0 {
			t.Errorf("allow = %v", allow)
		}
		useGoEnv(t, map[string]string{"GOCACHE": "", "GOMODCACHE": "relative/dir"})
		raw, _ = workerSettings(home, cfg, workerWritablePaths(home, []string{"go"}), true)
		_, allow, _ = settingsOf(t, raw)
		if len(allow) != 0 {
			t.Errorf("allow = %v", allow)
		}
	})

	t.Run("node", func(t *testing.T) {
		useGoEnv(t, nil)
		raw, _ := workerSettings(home, cfg, workerWritablePaths(home, []string{"node"}), true)
		_, allow, _ := settingsOf(t, raw)
		if strings.Join(allow, ",") != filepath.Join(home, ".npm") {
			t.Errorf("allow = %v", allow)
		}
	})

	t.Run("python and read add nothing", func(t *testing.T) {
		asked := useGoEnv(t, nil)
		raw, _ := workerSettings(home, cfg, workerWritablePaths(home, []string{"python", "read"}), true)
		_, allow, _ := settingsOf(t, raw)
		if len(allow) != 0 || len(*asked) != 0 {
			t.Errorf("allow = %v, asked %v", allow, *asked)
		}
	})
}

// The settings always refuse the file-reading tools the places where keys
// live, by rules written as Claude Code writes paths, with or without the
// sandbox; without it there is no sandbox object at all.
func TestWorkerSettingsReadRules(t *testing.T) {
	useGoEnv(t, nil)
	home := filepath.Join(string(filepath.Separator), "home", "invented")
	inside := filepath.Join(home, ".config", "ulgen")
	outside := filepath.Join(string(filepath.Separator), "srv", "ulgen cfg")
	for _, sandbox := range []bool{true, false} {
		raw, err := workerSettings(home, inside, nil, sandbox)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Permissions struct {
				Deny []string `json:"deny"`
			} `json:"permissions"`
			Sandbox map[string]any `json:"sandbox"`
		}
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("settings are not JSON: %v\n%s", err, raw)
		}
		if (doc.Sandbox != nil) != sandbox {
			t.Errorf("sandbox %v: sandbox object = %v", sandbox, doc.Sandbox)
		}
		rules := "\n" + strings.Join(doc.Permissions.Deny, "\n") + "\n"
		for _, want := range []string{"Read(~/.ssh)", "Read(~/.ssh/**)", "Read(~/.netrc)", "Read(~/.config/gh/**)",
			"Read(~/Library/Keychains/**)", "Read(~/.claude/**)", "Read(~/.config/ulgen)", "Read(~/.config/ulgen/**)"} {
			if !strings.Contains(rules, "\n"+want+"\n") {
				t.Errorf("sandbox %v: no rule %s in %v", sandbox, want, doc.Permissions.Deny)
			}
		}
		if len(doc.Permissions.Deny) != 2*(len(workerHiddenFromHome)+1) {
			t.Errorf("sandbox %v: %d rules, want two for each hidden path", sandbox, len(doc.Permissions.Deny))
		}
	}
	// A configuration directory outside home is named as an absolute path.
	raw, _ := workerSettings(home, outside, nil, false)
	if !strings.Contains(raw, `"Read(//srv/ulgen cfg)","Read(//srv/ulgen cfg/**)"`) {
		t.Errorf("no absolute rule for %s in %s", outside, raw)
	}
	// A path that only begins like home is not under it.
	if got := workerReadRulePath(home, home+"-other"); got != "//home/invented-other" {
		t.Errorf("workerReadRulePath = %q", got)
	}
}

// The directories a preset lets commands write are made before the child is
// started: the sandbox would not let a command make the directories above a
// cache that was never used. Without the sandbox nothing is made.
func TestWorkerMakesThePresetDirectories(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	base := t.TempDir()
	cache := filepath.Join(base, "never", "used", "go-build")
	mod := filepath.Join(base, "mod")
	useGoEnv(t, map[string]string{"GOCACHE": cache, "GOMODCACHE": mod})
	saved := workerMakeDir
	workerMakeDir = os.MkdirAll
	t.Cleanup(func() { workerMakeDir = saved })

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--no-sandbox", "--allow", "go", writeTask(t, "x"))
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Errorf("--no-sandbox made %s", cache)
	}

	code, _, stderr = exec(t, context.Background(), vars, "worker", "--base-url", url, "--allow", "go", writeTask(t, "x"))
	if code != 0 || s.calls != 2 {
		t.Fatalf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	for _, dir := range []string{cache, mod} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s was not made: %v", dir, err)
		}
	}
}

// Through the command: the --settings argument holds Ülgen's configuration
// directory in the deny list, and the preset's go answers in the write list.
func TestWorkerSettingsThroughTheCommand(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	invCache, invMod := inventedPath(t, "inv", "cache"), inventedPath(t, "inv", "mod")
	useGoEnv(t, map[string]string{"GOCACHE": invCache, "GOMODCACHE": invMod})
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--allow", "go", "--allow", "go", writeTask(t, "x"))
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	n := len(s.argv)
	deny, allow, _ := settingsOf(t, s.argv[n-1])
	found := false
	for _, p := range deny {
		found = found || p == vars["ULGEN_CONFIG_DIR"]
	}
	if s.argv[n-2] != "--settings" || !found {
		t.Errorf("Ülgen's configuration directory %q is not denied: %v", vars["ULGEN_CONFIG_DIR"], deny)
	}
	if strings.Join(allow, ",") != invCache+","+invMod {
		t.Errorf("allow = %v", allow)
	}
}

// With no model option the child is given the default EVREN model, and the
// summary names it.
func TestWorkerDefaultModel(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != 0 || !strings.Contains(stdout, "(model deepseek-v4.1-flash)") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

// A tool that is not installed gets the message and the install note, exit 1,
// and nothing is fetched.
func TestWorkerNotInstalled(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, rec := evrenServer(t, chatRoutes())
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", srv.URL, task)
	want := "ulgen: Claude Code is not installed\nInstall it with:  npm install -g @anthropic-ai/claude-code\n"
	if code != 1 || stdout != "" || stderr != want || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	if len(rec.paths()) != 0 {
		t.Errorf("something was sent: %v", rec.paths())
	}
}

// Not signed in: the key is not found, exit 1, nothing is sent and nothing
// starts.
func TestWorkerNotSignedIn(t *testing.T) {
	useClaude(t, nil)
	useFileStore(t)
	srv, rec := evrenServer(t, chatRoutes())
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")
	code, _, stderr := exec(t, context.Background(), map[string]string{}, "worker", "--base-url", srv.URL, task)
	if code != 1 || stderr != "ulgen: not signed in; run ulgen login\n" || s.calls != 0 || len(rec.paths()) != 0 {
		t.Errorf("code %d stderr %q, %d starts, sent %v", code, stderr, s.calls, rec.paths())
	}
}

// A --base-url that would send the key in clear text is refused before
// anything is fetched.
func TestWorkerRefusesUnsafeBaseURL(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", "http://example.com/v1", task)
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: --base-url must use https") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A task and a tool with no task file is a usage mistake: the task file must
// be named.
func TestWorkerNeedsTask(t *testing.T) {
	code, stdout, stderr := exec(t, context.Background(), nil, "worker")
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: name the task file") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

// The worker's usage names the presets.
func TestWorkerHelpNamesPresets(t *testing.T) {
	code, stdout, stderr := exec(t, context.Background(), nil, "worker", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	for _, w := range []string{"ulgen worker [options]", "-allow", "-timeout", "-dir text", "go, node, python, read"} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	code, stdout, _ = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "worker", "--help")
	if code != 0 || !strings.Contains(stdout, "go, node, python, read") {
		t.Errorf("Turkish help: code %d stdout %q", code, stdout)
	}
}

// An --allow value with a comma is refused: one rule at a time.
func TestWorkerAllowWithCommasRefused(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "go")
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, "--allow", "Read,Write", task)
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: the first --allow value Read,Write ") || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
}

// useGoEnv makes "go env" answer from a table instead of running go; a name
// that is not in the table is an error. It returns the names asked.
func useGoEnv(t *testing.T, answers map[string]string) *[]string {
	t.Helper()
	asked := &[]string{}
	saved := workerGoEnv
	workerGoEnv = func(name string) (string, error) {
		*asked = append(*asked, name)
		if v, ok := answers[name]; ok {
			return v, nil
		}
		return "", errors.New("go env failed")
	}
	t.Cleanup(func() { workerGoEnv = saved })
	return asked
}

// A listener that fails to open is reported, and nothing starts.
func TestWorkerListenFailure(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	savedListen := listenTranslator
	listenTranslator = func(int) (net.Listener, error) { return nil, errTestListen }
	t.Cleanup(func() { listenTranslator = savedListen })
	task := writeTask(t, "do the thing")
	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", srv.URL, task)
	if code != exitError || !strings.Contains(stderr, "the translator could not listen: address in use") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A cancel that lands between the translator and the child starts nothing,
// shuts the translator down, and exits 130.
func TestWorkerCancelledStartsNothing(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	ln := useShuttingTranslator(t)
	s := useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	ctx, cancel := context.WithCancel(context.Background())
	savedListen := listenTranslator
	listenTranslator = func(int) (net.Listener, error) {
		cancel() // the interrupt lands between the translator and the child
		return ln, nil
	}
	t.Cleanup(func() { listenTranslator = savedListen })
	task := writeTask(t, "do the thing")
	code, _, stderr := exec(t, ctx, vars, "worker", "--base-url", srv.URL, task)
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	select {
	case <-ln.closed:
	case <-time.After(2 * time.Second):
		t.Errorf("the translator was not shut down")
	}
}

// A child whose output passed 64 MB is an error, not a truncation: it is
// reported and the run ends with 1, even though the child's own code was 0.
func TestWorkerOutputTooLarge(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	url := workerServer(t)
	s := useFakeWorkerChild(t, 0, nil, errChildOutputTooLarge)
	task := writeTask(t, "do the thing")
	code, stdout, stderr := exec(t, context.Background(), vars, "worker", "--base-url", url, task)
	if code != exitError || stdout != "" || !strings.Contains(stderr, "the worker wrote more than 64 MB") || s.calls != 1 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
}

// shuttingListener is a net.Listener whose Accept blocks until Close, so the
// translator's http.Server keeps it tracked and Shutdown closes it. That is
// how a test sees the translator being shut down.
type shuttingListener struct {
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func (l *shuttingListener) Addr() net.Addr { return l.addr }
func (l *shuttingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}
func (l *shuttingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

// useShuttingTranslator makes the translator listen on a listener a test can
// watch, and serve for real on it (so the real Shutdown closes it).
func useShuttingTranslator(t *testing.T) *shuttingListener {
	t.Helper()
	ln := &shuttingListener{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41234}, closed: make(chan struct{})}
	savedListen, savedServe := listenTranslator, serveTranslator
	listenTranslator = func(int) (net.Listener, error) { return ln, nil }
	serveTranslator = func(s *proxy.Server, l net.Listener) error { return s.Serve(l) }
	t.Cleanup(func() { listenTranslator, serveTranslator = savedListen, savedServe })
	return ln
}

// After a finished run the translator is shut down.
func TestWorkerShutsTranslatorDown(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	ln := useShuttingTranslator(t)
	useFakeWorkerChild(t, 0, []byte(workerJSON), nil)
	task := writeTask(t, "do the thing")

	code, _, stderr := exec(t, context.Background(), vars, "worker", "--base-url", srv.URL, task)
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	select {
	case <-ln.closed:
	case <-time.After(2 * time.Second):
		t.Errorf("the translator was not shut down")
	}
}
