package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/proxy"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// useClaude makes the test machine one where Claude Code is installed, with
// the given working directory as the project.
func useClaude(t *testing.T, bins map[string]string) setup.Env {
	t.Helper()
	if bins == nil {
		bins = map[string]string{"claude": "1.0.3 (Claude Code)\n"}
	}
	return useSetupEnv(t, bins)
}

// claudeRunArgs runs "ulgen run --base-url <stub> claude <toolArgs...>"
// against a stub EVREN that offers the models and answers a chat request,
// with a fake child. --base-url stands before the tool's name, as Ülgen's own
// options must.
func claudeRunArgs(t *testing.T, vars map[string]string, toolArgs ...string) (int, string, string, *startedChild) {
	t.Helper()
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	args := append([]string{"run", "--base-url", srv.URL, "claude"}, toolArgs...)
	code, stdout, stderr := exec(t, context.Background(), vars, args...)
	return code, stdout, stderr, s
}

// "ulgen run claude" starts the child with the translator's address, a token
// of its own, the model variables, and not the EVREN key. The arguments reach
// the child unchanged.
func TestRunClaudeSetsTheChildEnvironment(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	code, stdout, stderr, s := claudeRunArgs(t, vars, "-p", "say hi")
	if code != 0 || stdout != "" || s.calls != 1 {
		t.Fatalf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	if s.binary != testBin("claude") || strings.Join(s.argv, "|") != testBin("claude")+"|-p|say hi" {
		t.Errorf("started %q with %q", s.binary, s.argv)
	}
	joined := strings.Join(s.env, "\n")
	// The notice is on standard error, once.
	if stderr != "ulgen: Claude Code runs on an EVREN model through Ülgen's translator; Anthropic does not support this use.\n" {
		t.Errorf("stderr %q", stderr)
	}
	for _, want := range []string{
		"ANTHROPIC_AUTH_TOKEN=",
		"ANTHROPIC_MODEL=deepseek-v4.1-flash",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-v4.1-flash",
		"ANTHROPIC_DEFAULT_SONNET_MODEL=deepseek-v4.1-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL=deepseek-v4.1-flash",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-v4.1-flash",
		"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"CLAUDE_CODE_ATTRIBUTION_HEADER=0",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=16000",
		// The model's own context window, so Claude Code does not assume
		// 200k for a model it does not know and warn about it at every start.
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS=1048576",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the child's environment lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=http://127.0.0.1:") {
		t.Errorf("no local base URL:\n%s", joined)
	}
	// The EVREN key is stripped; the token is not the key.
	if strings.Contains(joined, "EVREN_API_KEY=") || strings.Contains(joined, testKey) {
		t.Errorf("the EVREN key reached the child:\n%s", joined)
	}
	// The environment is the parent's, with the entries above: a variable the
	// caller had is kept.
	if !strings.Contains(joined, "LANG=C") {
		t.Errorf("the caller's environment was dropped:\n%s", joined)
	}
}

// The variables that would send Claude Code to another provider are removed
// from the child's environment, CLAUDE_CODE_USE_FOUNDRY included.
func TestRunClaudeRemovesProviderVariables(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	saved := environ
	environ = func() []string {
		return []string{"PATH=/usr/bin", "EVREN_API_KEY=" + testKey, "ANTHROPIC_API_KEY=old",
			"CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1", "CLAUDE_CODE_USE_FOUNDRY=1", "LANG=C"}
	}
	t.Cleanup(func() { environ = saved })
	if code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude"); code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	joined := strings.Join(s.env, "\n")
	for _, gone := range []string{"EVREN_API_KEY=", "ANTHROPIC_API_KEY=", "CLAUDE_CODE_USE_BEDROCK=", "CLAUDE_CODE_USE_VERTEX=", "CLAUDE_CODE_USE_FOUNDRY="} {
		if strings.Contains(joined, gone) {
			t.Errorf("the child kept %q:\n%s", gone, joined)
		}
	}
	if !strings.Contains(joined, "LANG=C") {
		t.Errorf("the caller's environment was dropped:\n%s", joined)
	}
}

// ULGEN_QUIET_NOTICE=1 leaves out the notice.
func TestRunClaudeQuietNotice(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	vars[quietNoticeEnv] = "1"
	code, _, stderr, s := claudeRunArgs(t, vars)
	if code != 0 || stderr != "" || s.calls != 1 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// The caller's own CLAUDE_CODE_MAX_OUTPUT_TOKENS is kept.
func TestRunClaudeKeepsTheCallersMaxOutput(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	saved := environ
	environ = func() []string {
		return []string{"PATH=/usr/bin", claudeEnvMaxOutput + "=777", claudeEnvMaxContext + "=5000", "LANG=C"}
	}
	t.Cleanup(func() { environ = saved })
	if code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude"); code != 0 || stderr == "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	joined := strings.Join(s.env, "\n")
	if !strings.Contains(joined, claudeEnvMaxContext+"=5000") || strings.Contains(joined, claudeEnvMaxContext+"=1048576") {
		t.Errorf("the caller's context limit was replaced:\n%s", joined)
	}
	if !strings.Contains(joined, claudeEnvMaxOutput+"=777") || strings.Contains(joined, claudeEnvMaxOutput+"=65536") {
		t.Errorf("max output:\n%s", joined)
	}
}

// A tool that is not installed gets the message and the install note, exit 1,
// and nothing is fetched.
func TestRunClaudeNotInstalled(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, rec := evrenServer(t, healthy())
	s := useFakeChild(t, 0, nil)
	code, stdout, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude")
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
func TestRunClaudeNotSignedIn(t *testing.T) {
	useClaude(t, nil)
	useFileStore(t)
	srv, rec := evrenServer(t, healthy())
	s := useFakeChild(t, 0, nil)
	code, _, stderr := exec(t, context.Background(), map[string]string{}, "run", "--base-url", srv.URL, "claude")
	if code != 1 || stderr != "ulgen: not signed in; run ulgen login\n" || s.calls != 0 || len(rec.paths()) != 0 {
		t.Errorf("code %d stderr %q, %d starts, sent %v", code, stderr, s.calls, rec.paths())
	}
}

// The child's exit code is passed on.
func TestRunClaudeExitCode(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 42, nil)
	code, _, _ := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude")
	if code != 42 || s.calls != 1 {
		t.Errorf("code %d, %d starts", code, s.calls)
	}
}

// --model on a tool that is not started through the translator is a usage
// error: the option would be dropped without a word.
func TestRunModelNeedsProxied(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 0, nil)
	for _, args := range [][]string{
		{"run", "--model", "glm-5.3", "qwen"},
		{"run", "--base-url", "https://x.example/v1", "qwen"},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, args...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: --model and --base-url apply only to claude, which Ülgen starts through the translator") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
	if s.calls != 0 {
		t.Errorf("a refused run started %q", s.binary)
	}
}

// The end-to-end test: the child makes one request to the translator it was
// given, the translator asks the stub EVREN, and the answer comes back. The
// stub sees the EVREN key only in the Authorization header of its own
// request, and the child never holds it.
func TestRunClaudeTranslatesARequest(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, rec := evrenServer(t, chatRoutes())

	// The child is the test binary in the mode "proxy-request"; it reads the
	// translator's address and token from its environment. The real child
	// seam and the real translator are used.
	answerFile := filepath.Join(t.TempDir(), "answer")
	savedChild, savedEnv := startChild, environ
	startChild = func(binary string, argv, env []string) (int, error) {
		if binary != testBin("claude") {
			t.Errorf("started %q", binary)
		}
		self, err := os.Executable()
		if err != nil {
			return 0, err
		}
		childEnv := append(append([]string{}, env...),
			helperVar+"=proxy-request", helperReqBodyVar+"="+helperReqForClaude, helperOutVar+"="+answerFile)
		return runChild(self, []string{self}, childEnv)
	}
	environ = func() []string { return []string{"PATH=/usr/bin", "EVREN_API_KEY=" + testKey, "LANG=C"} }
	t.Cleanup(func() { startChild, environ = savedChild, savedEnv })

	var out, errOut strings.Builder
	getenv := func(k string) string { return vars[k] }
	code := run(context.Background(), []string{"run", "--base-url", srv.URL, "claude"}, strings.NewReader(""), &out, &errOut, getenv)
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	answer, err := os.ReadFile(answerFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(answer), "hi from EVREN") {
		t.Errorf("the answer did not come back: %q", answer)
	}
	// The stub saw the key only in the Authorization header of the chat call,
	// and the model list was fetched with it too.
	for _, r := range rec.all() {
		if r.Path == "/chat/completions" || r.Path == "/models" {
			if r.Auth != "Bearer "+testKey {
				t.Errorf("%s: Authorization %q", r.Path, r.Auth)
			}
		}
	}
	// The key is nowhere in the transcript.
	assertNoKey(t, "standard output", out.String(), testKey)
	assertNoKey(t, "standard error", errOut.String(), testKey)
	assertNoKey(t, "the request body", rec.all()[len(rec.all())-1].Body, testKey)
}

// A failed child start is reported like the other runs, and names the file.
func TestRunClaudeStartFailure(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, errors.New("exec format\x1b[2J error\nno"))
	code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude")
	if code != exitError || !strings.Contains(stderr, "ulgen: could not start Claude Code ("+testBin("claude")+"): exec format error no\n") || s.calls != 1 {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// The translator's listener that fails to open is reported, and nothing else
// starts.
func TestRunClaudeListenFailure(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	savedListen := listenTranslator
	listenTranslator = func(int) (net.Listener, error) { return nil, errors.New("address in use") }
	t.Cleanup(func() { listenTranslator = savedListen })
	code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude")
	if code != exitError || !strings.Contains(stderr, "ulgen: the translator could not listen: address in use\n") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A project file that names the EVREN key does not stop Claude Code: the
// child holds only the local token, so the project check is skipped.
func TestRunClaudeIgnoresProjectFiles(t *testing.T) {
	env := useClaude(t, nil)
	vars := signedIn(t)
	seedFile(t, filepath.Join(env.Home, "work", ".qwen", "settings.json"), `{"envKey": "EVREN_API_KEY"}`)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude")
	if code != 0 || s.calls != 1 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A --base-url that would send the key in clear text is refused before
// anything is fetched.
func TestRunClaudeRefusesUnsafeBaseURL(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	s := useFakeChild(t, 0, nil)
	code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", "http://example.com/v1", "claude")
	if code != exitUsage || !strings.HasPrefix(stderr, "ulgen: --base-url must use https") || s.calls != 0 {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// A --model that EVREN does not offer is a usage mistake; nothing starts.
func TestRunClaudeUnknownModel(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	code, _, stderr := exec(t, context.Background(), vars, "run", "--model", "no-such", "--base-url", srv.URL, "claude")
	if code != exitUsage || !strings.Contains(stderr, "EVREN has no tool-capable chat model named no-such") || s.calls != 0 {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// claudeCmdEnv makes the test machine one where Claude Code is installed as a
// batch file at path, which Windows starts through cmd.exe.
func claudeCmdEnv(t *testing.T, path string) setup.Env {
	t.Helper()
	env := useSetupEnv(t, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	env.GOOS = "windows"
	env.LookPath = func(name string) (string, error) {
		if name == "claude" {
			return path, nil
		}
		return "", errors.New("not found")
	}
	useEnv(t, env)
	return env
}

// A .cmd Claude install with an argument cmd.exe would interpret is refused
// before the model list is fetched and before the translator starts, with the
// same message and exit code 2 as the other tools.
func TestRunClaudeRefusesWhatCmdWouldInterpret(t *testing.T) {
	shim := testBin("claude.cmd")
	claudeCmdEnv(t, shim)
	vars := signedIn(t)
	srv, rec := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	const tail = ", which cannot be passed safely to a tool that Windows starts through cmd.exe; Claude Code can be started without that argument\n"
	code, stdout, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude", "what is %EVREN_API_KEY%")
	if code != exitUsage || stdout != "" || stderr != "ulgen: argument 1 contains %"+tail {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if s.calls != 0 {
		t.Errorf("a refused run started the child")
	}
	if len(rec.paths()) != 0 {
		t.Errorf("a refused run fetched something: %v", rec.paths())
	}
}

// A failure of the child start is reported through the same mapping the other
// tools use, so a *shimArgError gives the usage refusal and exit code 2.
func TestRunClaudeStartFailureIsMapped(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, &shimArgError{Index: 3, Char: '%'})
	const tail = ", which cannot be passed safely to a tool that Windows starts through cmd.exe; Claude Code can be started without that argument\n"
	code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude", "ok")
	if code != exitUsage || !strings.Contains(stderr, "ulgen: argument 3 contains %"+tail) || s.calls != 1 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A cancelled run starts nothing and exits 130: the context is checked right
// before the child is started.
func TestRunClaudeCancelledStartsNothing(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	s := useFakeChild(t, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	savedListen := listenTranslator
	listenTranslator = func(port int) (net.Listener, error) {
		cancel() // the interrupt lands between the translator and the child
		addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41235}
		return stubListener{addr: addr}, nil
	}
	t.Cleanup(func() { listenTranslator = savedListen })
	code, _, stderr := exec(t, ctx, vars, "run", "--base-url", srv.URL, "claude")
	if code != exitInterrupted || !strings.Contains(stderr, "ulgen: cancelled") || s.calls != 0 {
		t.Errorf("code %d stderr %q, %d starts", code, stderr, s.calls)
	}
}

// A translator that stops serving while the tool still runs is not lost: one
// line on standard error says so and the tool is left to end by itself.
func TestRunClaudeServeFailureIsReported(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, chatRoutes())
	useFakeChild(t, 0, nil) // replaces environ with a small parent environment
	savedServe := serveTranslator
	serveTranslator = func(*proxy.Server, net.Listener) error { return errors.New("listener broke") }
	release := make(chan struct{})
	started := make(chan struct{})
	startChild = func(string, []string, []string) (int, error) {
		close(started)
		<-release
		return 0, nil
	}
	t.Cleanup(func() { serveTranslator = savedServe })

	var out strings.Builder
	errOut := &lockedBuffer{}
	getenv := func(k string) string { return vars[k] }
	done := make(chan int, 1)
	go func() {
		done <- run(context.Background(), []string{"run", "--base-url", srv.URL, "claude"}, strings.NewReader(""), &out, errOut, getenv)
	}()
	<-started
	// The serve error must reach standard error while the child still runs.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(errOut.String(), "stopped serving while the tool is still running") {
		time.Sleep(2 * time.Millisecond)
	}
	if !strings.Contains(errOut.String(), "ulgen: the translator stopped serving while the tool is still running: listener broke\n") {
		t.Errorf("stderr %q", errOut.String())
	}
	close(release)
	if code := <-done; code != 0 {
		t.Errorf("code %d", code)
	}
}

// lockedBuffer is an io.Writer whose content can be read while a goroutine
// writes it, which strings.Builder alone does not allow.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write appends to the buffer.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what has been written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The small model is deepseek-v4-flash when EVREN offers it, else the chosen
// model.
func TestRunClaudeSmallModel(t *testing.T) {
	useClaude(t, nil)
	vars := signedIn(t)
	routes := map[string]answer{
		"/models": {body: `{"object":"list","data":[` +
			`{"id":"deepseek-v4.1-flash","object":"model","task":"chat","context_length":1048576,"default_max_output_tokens":65536,"capabilities":{"tools":true,"thinking":true,"vision":true}},` +
			`{"id":"deepseek-v4-flash","object":"model","task":"chat","context_length":1048576,"default_max_output_tokens":32000,"capabilities":{"tools":true,"thinking":false,"vision":false}}]}`},
	}
	srv, _ := evrenServer(t, routes)
	s := useFakeChild(t, 0, nil)
	if code, _, stderr := exec(t, context.Background(), vars, "run", "--base-url", srv.URL, "claude"); code != 0 || stderr == "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if !strings.Contains(strings.Join(s.env, "\n"), "ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-v4-flash") {
		t.Errorf("small model not picked:\n%s", strings.Join(s.env, "\n"))
	}
}
