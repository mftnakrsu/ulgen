package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useGuide makes the guide's sign-in and setup talk to the stub EVREN and
// says whether "ulgen" alone sees a terminal on both sides.
func useGuide(t *testing.T, baseURL string, terminal bool) {
	t.Helper()
	savedURL, savedIn := guideBaseURL, stdinTerminal
	guideBaseURL = baseURL
	stdinTerminal = func() terminalKind {
		if terminal {
			return ptyPipeTerminal // a terminal whose input is read as lines
		}
		return notTerminal
	}
	useTerminalStdout(t, terminal)
	t.Cleanup(func() { guideBaseURL, stdinTerminal = savedURL, savedIn })
}

func TestBareUlgenWithoutATerminalPrintsTheUsage(t *testing.T) {
	useGuide(t, "", false)
	code, stdout, stderr := exec(t, context.Background(), nil)
	if code != 0 || !strings.HasPrefix(stdout, "Usage:") || stderr != "" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestGuideStartsTheToolThatIsSetUp(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 7, nil)
	useGuide(t, "", true)
	code, stdout, stderr := exec(t, context.Background(), vars)
	if code != 7 || s.calls != 1 || s.binary != testBin("qwen") {
		t.Fatalf("code %d, %d starts of %q; stdout %q stderr %q", code, s.calls, s.binary, stdout, stderr)
	}
	if got := strings.Join(s.env, "|"); !strings.Contains(got, "EVREN_API_KEY="+testKey) {
		t.Errorf("the tool did not get the key: %q", got)
	}
	if strings.Contains(stdout, "Which tool") {
		t.Errorf("one installed tool, but the guide asked: %q", stdout)
	}
}

func TestGuideAsksWhichToolAndRemembersTheAnswer(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	setUp(t, vars, "opencode")
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", true)

	// A word that is no number is asked again; "2" is the second tool.
	code, stdout, stderr := execIn(t, context.Background(), vars, "x\n2\n")
	if code != 0 || s.calls != 1 || s.binary != testBin("opencode") {
		t.Fatalf("code %d, %d starts of %q; stdout %q stderr %q", code, s.calls, s.binary, stdout, stderr)
	}
	if !strings.Contains(stdout, "1) Qwen Code") || !strings.Contains(stdout, "2) opencode") || !strings.Contains(stdout, "Enter a number from 1 to 2") {
		t.Errorf("stdout = %q", stdout)
	}

	// The next run asks nothing.
	code, stdout, _ = exec(t, context.Background(), vars)
	if code != 0 || s.calls != 2 || s.binary != testBin("opencode") || strings.Contains(stdout, "Which tool") {
		t.Errorf("second run: code %d, %d starts of %q; stdout %q", code, s.calls, s.binary, stdout)
	}
}

func TestGuideTakesTheFirstToolOnEnter(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", true)
	if code, _, stderr := execIn(t, context.Background(), vars, "\n"); code != 0 || s.binary != testBin("qwen") {
		t.Errorf("code %d started %q stderr %q", code, s.binary, stderr)
	}
}

func TestGuideWithoutAnAnswerStartsNothing(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", true)
	code, _, stderr := execIn(t, context.Background(), vars, "") // the input ends at the question
	if code != exitError || s.calls != 0 || !strings.Contains(stderr, "no tool was chosen") {
		t.Errorf("code %d, %d starts, stderr %q", code, s.calls, stderr)
	}
}

func TestGuideSetsTheToolUpBeforeItStartsIt(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	s := useFakeExec(t, 0, nil)
	useGuide(t, srv.URL, true)
	code, stdout, stderr := exec(t, context.Background(), vars)
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d, %d starts; stdout %q stderr %q", code, s.calls, stdout, stderr)
	}
	settings := readFile(t, filepath.Join(env.Home, ".qwen", "settings.json"))
	if !strings.Contains(settings, "EVREN_API_KEY") {
		t.Errorf("the settings were not written: %q", settings)
	}
	if !strings.Contains(stdout, "Qwen Code is not set up for EVREN yet") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestGuideSignsInFirst(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	store := useFileStore(t)
	useFakeBrowser(t, nil)
	vars := map[string]string{"ULGEN_CONFIG_DIR": t.TempDir()}
	srv, _ := evrenServer(t, healthy())
	s := useFakeExec(t, 0, nil)
	useGuide(t, srv.URL, true)
	code, stdout, stderr := execIn(t, context.Background(), vars, testKey+"\n")
	if code != 0 || s.calls != 1 {
		t.Fatalf("code %d, %d starts; stdout %q stderr %q", code, s.calls, stdout, stderr)
	}
	if got, err := store.Get("evren"); err != nil || got != testKey {
		t.Errorf("the key was not saved: %v", err)
	}
	if !strings.Contains(stdout, "not signed in to EVREN yet") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestGuideStartsNothingWhenSignInFails(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	useFileStore(t)
	useFakeBrowser(t, nil)
	routes := healthy()
	routes["/terms/status"] = answer{status: 401, body: `{"error":{"message":"bad key","code":"unauthorized"}}`}
	srv, _ := evrenServer(t, routes)
	s := useFakeExec(t, 0, nil)
	useGuide(t, srv.URL, true)
	code, _, _ := execIn(t, context.Background(), map[string]string{"ULGEN_CONFIG_DIR": t.TempDir()}, testKey+"\n")
	if code == 0 || s.calls != 0 {
		t.Errorf("code %d, %d starts", code, s.calls)
	}
}

func TestGuideStartsNothingWhenSetupFails(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	routes := healthy()
	routes["/models"] = answer{status: 401, body: `{"error":{"message":"bad key","code":"unauthorized"}}`}
	srv, _ := evrenServer(t, routes)
	s := useFakeExec(t, 0, nil)
	useGuide(t, srv.URL, true)
	code, _, _ := exec(t, context.Background(), vars)
	if code == 0 || s.calls != 0 {
		t.Errorf("code %d, %d starts", code, s.calls)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qwen", "settings.json")); err == nil {
		t.Error("settings were written although setup failed")
	}
}

func TestGuideWithNoToolInstalledSaysHowToInstallOne(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", true)
	code, stdout, stderr := exec(t, context.Background(), vars)
	if code != exitError || s.calls != 0 {
		t.Fatalf("code %d, %d starts", code, s.calls)
	}
	if !strings.Contains(stderr, "No supported coding tool is installed") || !strings.Contains(stderr, "npm") {
		t.Errorf("stdout %q stderr %q", stdout, stderr)
	}
}

func TestToolNameIsAShortcut(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	setUp(t, vars, "opencode")
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", false) // the shortcut needs no terminal
	code, _, stderr := exec(t, context.Background(), vars, "opencode", "run", "say hi")
	if code != 0 || s.binary != testBin("opencode") || !strings.HasSuffix(strings.Join(s.argv, "|"), "|run|say hi") {
		t.Fatalf("code %d started %q with %q; stderr %q", code, s.binary, s.argv, stderr)
	}
	// The tool that was named is the one "ulgen" alone starts from now on.
	useGuide(t, "", true)
	if code, _, _ := exec(t, context.Background(), vars); code != 0 || s.calls != 2 || s.binary != testBin("opencode") {
		t.Errorf("after the shortcut: code %d, %d starts of %q", code, s.calls, s.binary)
	}
}

func TestShortcutToAToolThatIsNotInstalled(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", false)
	code, _, stderr := exec(t, context.Background(), vars, "qwen")
	if code != exitError || s.calls != 0 || !strings.Contains(stderr, "Qwen Code is not installed") {
		t.Errorf("code %d, %d starts, stderr %q", code, s.calls, stderr)
	}
}

func TestGuideInterruptedAtTheQuestion(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	s := useFakeExec(t, 0, nil)
	useGuide(t, "", true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code, _, _ := execIn(t, ctx, vars, ""); code != exitInterrupted || s.calls != 0 {
		t.Errorf("code %d, %d starts", code, s.calls)
	}
}
