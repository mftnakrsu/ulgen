package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/proxy"
)

// startedChild records how the proxied tool would have been started.
type startedChild struct {
	binary string
	argv   []string
	env    []string
	calls  int
}

// useFakeChild records how "ulgen run" would start the child instead of
// starting it. environ is given a small parent environment that holds the key
// variable, so a test sees whether the child's environment drops it.
func useFakeChild(t *testing.T, code int, err error) *startedChild {
	t.Helper()
	var s startedChild
	savedChild, savedEnv := startChild, environ
	startChild = func(binary string, argv, env []string) (int, error) {
		s.binary, s.argv, s.env = binary, argv, env
		s.calls++
		return code, err
	}
	environ = func() []string { return []string{"PATH=/usr/bin", "EVREN_API_KEY=" + testKey, "LANG=C"} }
	t.Cleanup(func() { startChild, environ = savedChild, savedEnv })
	return &s
}

// stubListener is a net.Listener that never accepts: the tests that only
// check what a command prints do not want the translator to serve.
type stubListener struct{ addr net.Addr }

func (l stubListener) Addr() net.Addr            { return l.addr }
func (l stubListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l stubListener) Close() error              { return nil }

// useFakeTranslator makes the translator listen on a fixed loopback address
// and serve nothing, so a test can check the address that is printed without
// opening a port or waiting for a request.
func useFakeTranslator(t *testing.T, port int) {
	t.Helper()
	savedListen, savedServe := listenTranslator, serveTranslator
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	listenTranslator = func(int) (net.Listener, error) { return stubListener{addr: addr}, nil }
	serveTranslator = func(*proxy.Server, net.Listener) error { return nil }
	t.Cleanup(func() { listenTranslator, serveTranslator = savedListen, savedServe })
}

// chatRoutes is the stub EVREN of a translated request: the model list of
// "ulgen setup" and one non-streaming chat completion.
func chatRoutes() map[string]answer {
	return map[string]answer{
		"/models":           {body: fxModels},
		"/chat/completions": {body: `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi from EVREN"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`},
	}
}

// The lines "ulgen proxy --print-env" prints are the ones a shell needs, in
// the shell's own form: POSIX assignment on Unix, PowerShell on Windows.
func TestProxyPrintEnv(t *testing.T) {
	for name, tc := range map[string]struct {
		style string
		want  []string
	}{
		"posix": {"posix", []string{
			"ANTHROPIC_BASE_URL='http://127.0.0.1:41234'\n",
			"ANTHROPIC_MODEL='deepseek-v4.1-flash'\n",
			"ANTHROPIC_DEFAULT_OPUS_MODEL='deepseek-v4.1-flash'\n",
			"ANTHROPIC_DEFAULT_SONNET_MODEL='deepseek-v4.1-flash'\n",
			"CLAUDE_CODE_SUBAGENT_MODEL='deepseek-v4.1-flash'\n",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL='deepseek-v4.1-flash'\n",
			"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS='1'\n",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC='1'\n",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS='16000'\n",
		}},
		"powershell": {"powershell", []string{
			"$env:ANTHROPIC_BASE_URL = 'http://127.0.0.1:41234'\n",
			"$env:ANTHROPIC_MODEL = 'deepseek-v4.1-flash'\n",
			"$env:CLAUDE_CODE_MAX_OUTPUT_TOKENS = '16000'\n",
		}},
	} {
		useSetupEnv(t, nil)
		vars := signedIn(t)
		srv, _ := evrenServer(t, healthy())
		useFakeTranslator(t, 41234)
		savedStyle := envStyle
		envStyle = func() string { return tc.style }
		t.Cleanup(func() { envStyle = savedStyle })

		code, stdout, stderr := exec(t, context.Background(), vars, "proxy", "--print-env", "--base-url", srv.URL)
		if code != 0 {
			t.Errorf("%s: code %d stderr %q", name, code, stderr)
		}
		if stderr != "ulgen: the translator is listening on http://127.0.0.1:41234\n" {
			t.Errorf("%s: stderr %q", name, stderr)
		}
		for _, w := range tc.want {
			if !strings.Contains(stdout, w) {
				t.Errorf("%s: stdout lacks %q:\n%s", name, w, stdout)
			}
		}
		// The token is printed in full by --print-env, and it is 64 hex
		// characters: a fresh random one.
		line := "ANTHROPIC_AUTH_TOKEN='"
		if tc.style == "powershell" {
			line = "$env:ANTHROPIC_AUTH_TOKEN = '"
		}
		i := strings.Index(stdout, line)
		if i < 0 {
			t.Fatalf("%s: no token line in:\n%s", name, stdout)
		}
		rest := stdout[i+len(line):]
		tok, _, _ := strings.Cut(rest, "'")
		if len(tok) != 64 {
			t.Errorf("%s: token %q has %d characters, want 64", name, tok, len(tok))
		}
	}
}

// ULGEN_PROXY_TOKEN decides the token when it is long enough; a short one is
// refused by name, and it is never printed.
func TestProxyToken(t *testing.T) {
	short := "too-short"
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
		code int
	}{
		{"a variable long enough", map[string]string{tokenEnv: "a-token-of-16-or-more"}, "a-token-of-16-or-more", 0},
		{"a short variable", map[string]string{tokenEnv: short}, "", exitError},
	} {
		useSetupEnv(t, nil)
		vars := signedIn(t)
		for k, v := range tc.env {
			vars[k] = v
		}
		srv, _ := evrenServer(t, healthy())
		savedToken, savedStyle := randomToken, envStyle
		randomToken = func() (string, error) { return "generated-token-64", nil }
		envStyle = func() string { return "posix" }
		t.Cleanup(func() { randomToken, envStyle = savedToken, savedStyle })
		useFakeTranslator(t, 41234)

		code, stdout, stderr := exec(t, context.Background(), vars, "proxy", "--print-env", "--base-url", srv.URL)
		if code != tc.code {
			t.Errorf("%s: code %d stderr %q", tc.name, code, stderr)
			continue
		}
		if tc.code != 0 {
			// The value of a short var must not be printed; the name is.
			if strings.Contains(stderr, short) || !strings.Contains(stderr, tokenEnv) {
				t.Errorf("%s: stderr %q", tc.name, stderr)
			}
			continue
		}
		if !strings.Contains(stdout, "ANTHROPIC_AUTH_TOKEN='"+tc.want+"'") {
			t.Errorf("%s: stdout lacks the token %q:\n%s", tc.name, tc.want, stdout)
		}
	}
}

// A provider failure before any byte is sent is the plain EVREN error: the
// translator reports what EVREN said.
func TestProxyReportsProviderFailure(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	if err := useFileStore(t).Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	srv, _ := evrenServer(t, map[string]answer{"/models": {status: 401, body: fxUnauthorized}})
	code, _, stderr := exec(t, context.Background(), vars, "proxy", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stderr, "the stored key is no longer valid") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// A port outside 0..65535 is a usage mistake; nothing is fetched.
func TestProxyBadPort(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	for _, port := range []string{"-1", "65536", "99999"} {
		code, stdout, stderr := exec(t, context.Background(), vars, "proxy", "--port", port)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: port ") {
			t.Errorf("--port %s: code %d stdout %q stderr %q", port, code, stdout, stderr)
		}
	}
}

// A token generator that fails is reported and nothing listens.
func TestProxyTokenFailure(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	savedToken := randomToken
	randomToken = func() (string, error) { return "", errors.New("no entropy") }
	t.Cleanup(func() { randomToken = savedToken })
	code, _, stderr := exec(t, context.Background(), vars, "proxy", "--base-url", srv.URL)
	if code != exitError || stderr != "ulgen: a token could not be generated: no entropy\n" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// A listener that cannot be opened is reported with the address it needed.
func TestProxyListenFailure(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	savedListen := listenTranslator
	listenTranslator = func(int) (net.Listener, error) { return nil, errors.New("address in use") }
	t.Cleanup(func() { listenTranslator = savedListen })
	code, _, stderr := exec(t, context.Background(), vars, "proxy", "--port", "41234", "--base-url", srv.URL)
	if code != exitError || stderr != "ulgen: the translator could not listen: address in use\n" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// The proxy help names the options and the token variable's rule.
func TestProxyHelp(t *testing.T) {
	code, stdout, stderr := exec(t, context.Background(), nil, "proxy", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	for _, w := range []string{"ulgen proxy [options]", "-port number", "-model text", "-print-env"} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
}
