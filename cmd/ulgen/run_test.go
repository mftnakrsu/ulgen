package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/setup"
)

// started records how a tool would have been started.
type started struct {
	binary string
	argv   []string
	env    []string
	calls  int
}

// useFakeExec records how a tool would be started instead of starting it,
// and gives the command a small environment without the key variable, which
// is what a shell has after "ulgen login".
func useFakeExec(t *testing.T, code int, err error) *started {
	t.Helper()
	var s started
	savedExec, savedEnviron := execTool, environ
	execTool = func(binary string, argv, env []string) (int, error) {
		s.binary, s.argv, s.env = binary, argv, env
		s.calls++
		return code, err
	}
	environ = func() []string { return []string{"PATH=/usr/bin", "LANG=C"} }
	t.Cleanup(func() { execTool, environ = savedExec, savedEnviron })
	return &s
}

// setUp runs "ulgen setup <tool>" against a stub EVREN so the tool counts
// as set up.
func setUp(t *testing.T, vars map[string]string, tool string) {
	t.Helper()
	srv, _ := evrenServer(t, healthy())
	if code, _, stderr := exec(t, context.Background(), vars, "setup", tool, "--base-url", srv.URL); code != 0 {
		t.Fatalf("setup %s: code %d stderr %q", tool, code, stderr)
	}
}

func TestRunStartsTheToolWithTheKey(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 7, nil)
	// "ulgen run" needs the tool's path, not its version: asking for it
	// would run the tool once more before every start.
	asked := probes(t, env, nil)
	code, stdout, stderr := exec(t, context.Background(), vars, "run", "qwen", "-p", "say hi", "--model", "x")
	if code != 7 || stdout != "" || stderr != "" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if s.binary != testBin("qwen") || strings.Join(s.argv, "|") != testBin("qwen")+"|-p|say hi|--model|x" {
		t.Errorf("started %q with %q", s.binary, s.argv)
	}
	if got := strings.Join(s.env, "|"); got != "PATH=/usr/bin|LANG=C|EVREN_API_KEY="+testKey {
		t.Errorf("environment = %q", got)
	}
	if *asked != 0 || s.calls != 1 {
		t.Errorf("%d version probes, %d starts", *asked, s.calls)
	}
}

// The variable in the environment wins over the saved key, as in "ulgen
// status" and "ulgen token": the tool gets the key the caller's shell has.
// A real shell has it in both places Ülgen looks, so the test has too.
func TestRunUsesTheKeyOfTheEnvironment(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 0, nil)
	const fromShell = "evren_llm_exported_in_the_shell"
	vars["EVREN_API_KEY"] = fromShell
	environ = func() []string { return []string{"PATH=/usr/bin", "EVREN_API_KEY=" + fromShell, "LANG=C"} }
	if code, _, stderr := exec(t, context.Background(), vars, "run", "qwen"); code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if got := strings.Join(s.env, "|"); got != "PATH=/usr/bin|LANG=C|EVREN_API_KEY="+fromShell {
		t.Errorf("environment = %q", got)
	}

	// A variable that is set but blank is not a key: the saved one is used,
	// and the blank entry does not stay next to it.
	vars["EVREN_API_KEY"] = "  "
	environ = func() []string { return []string{"EVREN_API_KEY=  ", "PATH=/usr/bin"} }
	exec(t, context.Background(), vars, "run", "qwen")
	if got := strings.Join(s.env, "|"); got != "PATH=/usr/bin|EVREN_API_KEY="+testKey {
		t.Errorf("blank variable: environment = %q", got)
	}

	// On Windows the name of a variable is the same in any case.
	delete(vars, "EVREN_API_KEY")
	env.GOOS = "windows"
	useEnv(t, env)
	environ = func() []string { return []string{"Path=C:\\Windows", "Evren_Api_Key=old", "EVREN_API_KEYS=other"} }
	exec(t, context.Background(), vars, "run", "qwen")
	if got := strings.Join(s.env, "|"); got != "Path=C:\\Windows|EVREN_API_KEYS=other|EVREN_API_KEY="+testKey {
		t.Errorf("Windows: environment = %q", got)
	}
}

func TestWithKey(t *testing.T) {
	in := []string{"A=1", "EVREN_API_KEY=old", "evren_api_key=lower", "EVREN_API_KEY_OLD=x", "EVREN_API_KEY=again", "B=2", "EVREN"}
	if got := strings.Join(withKey(in, "new", false), "|"); got != "A=1|evren_api_key=lower|EVREN_API_KEY_OLD=x|B=2|EVREN|EVREN_API_KEY=new" {
		t.Errorf("exact names: %q", got)
	}
	if got := strings.Join(withKey(in, "new", true), "|"); got != "A=1|EVREN_API_KEY_OLD=x|B=2|EVREN|EVREN_API_KEY=new" {
		t.Errorf("names in any case: %q", got)
	}
	if got := withKey(nil, "new", false); len(got) != 1 || got[0] != "EVREN_API_KEY=new" {
		t.Errorf("empty environment: %q", got)
	}
	if len(in) != 7 || in[1] != "EVREN_API_KEY=old" {
		t.Errorf("the caller's slice was changed: %q", in)
	}
}

// Everything after the tool's name belongs to the tool, whatever it looks
// like.
func TestRunPassesTheArgumentsUnchanged(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 0, nil)
	for _, tc := range []struct {
		name string
		args []string // after "run"
		want []string // what the tool gets
	}{
		{"the tool's own help", []string{"qwen", "-h"}, []string{"-h"}},
		{"--help", []string{"qwen", "--help"}, []string{"--help"}},
		{"a double hyphen", []string{"qwen", "--", "-p", "x"}, []string{"--", "-p", "x"}},
		{"an empty argument", []string{"qwen", "", "-x", ""}, []string{"", "-x", ""}},
		{"a leading hyphen", []string{"qwen", "-", "-1", "--flag=a b"}, []string{"-", "-1", "--flag=a b"}},
		{"ulgen's own option after the name", []string{"qwen", "--trust-project", "-trust-project"}, []string{"--trust-project", "-trust-project"}},
		{"a double hyphen before the name", []string{"--", "qwen", "-p", "x"}, []string{"-p", "x"}},
		{"shell characters", []string{"qwen", "a&b|c", "$HOME", "%PATH%", `q"uo` + "`te", "a b", "çğıöşü"}, []string{"a&b|c", "$HOME", "%PATH%", `q"uo` + "`te", "a b", "çğıöşü"}},
		{"no arguments", []string{"qwen"}, nil},
	} {
		s.calls = 0
		code, stdout, stderr := exec(t, context.Background(), vars, append([]string{"run"}, tc.args...)...)
		if code != 0 || stdout != "" || stderr != "" || s.calls != 1 {
			t.Errorf("%s: code %d stdout %q stderr %q, %d starts", tc.name, code, stdout, stderr, s.calls)
			continue
		}
		if got := s.argv[1:]; len(got) != len(tc.want) || strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("%s: the tool got %q, want %q", tc.name, got, tc.want)
		}
		if s.argv[0] != testBin("qwen") {
			t.Errorf("%s: argv[0] = %q", tc.name, s.argv[0])
		}
	}
}

func TestRunCodexUsesTheProfile(t *testing.T) {
	useSetupEnv(t, map[string]string{"codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	setUp(t, vars, "codex")
	s := useFakeExec(t, 0, nil)
	if code, _, stderr := exec(t, context.Background(), vars, "run", "codex", "exec", "fix it"); code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if got := strings.Join(s.argv, "|"); got != testBin("codex")+"|--profile|evren|exec|fix it" {
		t.Errorf("argv = %q", got)
	}
	exec(t, context.Background(), vars, "run", "codex", "--profile", "work")
	if got := strings.Join(s.argv, "|"); got != testBin("codex")+"|--profile|work" {
		t.Errorf("argv with the user's profile = %q", got)
	}
}

func TestRunRefusals(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	s := useFakeExec(t, 0, nil)
	usage := "Usage:\n  ulgen run [options] <tool> [arguments...]\n"
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"run"}, 2, "ulgen: name the tool to start\n" + usage},
		{[]string{"run", ""}, 2, "ulgen: name the tool to start\n" + usage},
		{[]string{"run", "--trust-project"}, 2, "ulgen: name the tool to start\n" + usage},
		{[]string{"run", "cursor"}, 2, "ulgen: cannot start cursor; supported: qwen, opencode, codex, claude\n" + usage},
		{[]string{"run", "--nope", "qwen"}, 2, "ulgen: unknown option: -nope\n" + usage},
		// The name of a tool, never a path to a program.
		{[]string{"run", "./qwen"}, 2, "ulgen: cannot start ./qwen; supported: qwen, opencode, codex, claude\n"},
		{[]string{"run", testBin("qwen")}, 2, "ulgen: cannot start " + testBin("qwen") + "; supported: qwen, opencode, codex, claude\n"},
		// A key pasted in the wrong place is not printed back.
		{[]string{"run", "evren_llm_PASTED"}, 2, "ulgen: cannot start [key]; supported: qwen, opencode, codex, claude\n"},
		{[]string{"run", "--evren_llm_PASTED", "qwen"}, 2, "ulgen: unknown option: -[key]\n"},
		{[]string{"run", "opencode"}, 1, "ulgen: opencode is not installed\nInstall it with:  npm install -g opencode-ai\n"},
		{[]string{"run", "qwen"}, 1, "ulgen: EVREN is not set up in Qwen Code; run ulgen setup qwen\n"},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, tc.args...)
		if code != tc.code || !strings.HasPrefix(stderr, tc.want) || stdout != "" {
			t.Errorf("%v: code %d stdout %q stderr %q", tc.args, code, stdout, stderr)
		}
		if tc.code == 1 && stderr != tc.want {
			t.Errorf("%v: a failure that is not a usage mistake printed more: %q", tc.args, stderr)
		}
	}
	if s.calls != 0 {
		t.Errorf("a refused run started %q", s.binary)
	}

	setUp(t, vars, "qwen")
	// An unreadable file is reported, not started over.
	settings := filepath.Join(env.Home, ".qwen", "settings.json")
	good, _ := os.ReadFile(settings)
	os.WriteFile(settings, []byte("{"), 0o600)
	shown := "~" + string(filepath.Separator) + filepath.Join(".qwen", "settings.json")
	if code, _, stderr := exec(t, context.Background(), vars, "run", "qwen"); code != 1 || !strings.HasPrefix(stderr, "ulgen: "+shown+" is not valid JSON (") || s.calls != 0 {
		t.Errorf("unreadable: code %d stderr %q", code, stderr)
	}
	os.WriteFile(settings, good, 0o600)

	// A start that fails names the file that was tried, and puts what the
	// system says on one line.
	useFakeExec(t, 0, errors.New("exec format\x1b[2J error\nulgen: fine"))
	if code, _, stderr := exec(t, context.Background(), vars, "run", "qwen"); code != 1 || stderr != "ulgen: could not start Qwen Code ("+testBin("qwen")+"): exec format error ulgen: fine\n" {
		t.Errorf("start failure: code %d stderr %q", code, stderr)
	}
	vars["LANG"] = "tr_TR.UTF-8"
	if _, _, stderr := exec(t, context.Background(), vars, "run", "qwen"); stderr != "ulgen: Qwen Code başlatılamadı ("+testBin("qwen")+"): exec format error ulgen: fine\n" {
		t.Errorf("Turkish start failure: %q", stderr)
	}
	if _, _, stderr := exec(t, context.Background(), vars, "run", "cursor"); !strings.HasPrefix(stderr, "ulgen: cursor aracı başlatılamıyor; desteklenenler: qwen, opencode, codex, claude\n") {
		t.Errorf("Turkish unknown tool: %q", stderr)
	}
	delete(vars, "LANG")

	// Signed out: nothing starts.
	useFileStore(t)
	s = useFakeExec(t, 0, nil)
	if code, _, stderr := exec(t, context.Background(), vars, "run", "qwen"); code != 1 || stderr != "ulgen: not signed in; run ulgen login\n" || s.calls != 0 {
		t.Errorf("signed out: code %d stderr %q started %q", code, stderr, s.binary)
	}
}

// One Ctrl+C ends the command: after it, no tool is started.
func TestRunStartsNothingAfterCancel(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 7, nil)
	// Cancelled before the command begins.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, stdout, stderr := exec(t, ctx, vars, "run", "qwen", "-p", "x")
	if code != 130 || stdout != "" || stderr != "ulgen: cancelled\n" || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	// Cancelled while the command works, here while it looks at the project.
	ctx, cancel = context.WithCancel(context.Background())
	dir := workingDir
	workingDir = func() (string, error) {
		cancel()
		return dir()
	}
	t.Cleanup(func() { workingDir = dir })
	vars["LANG"] = "tr_TR.UTF-8"
	code, _, stderr = exec(t, ctx, vars, "run", "qwen")
	if code != 130 || stderr != "ulgen: iptal edildi\n" || s.calls != 0 {
		t.Errorf("mid-way: code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	// Cancelled while the key is read: the last step before the start.
	ctx, cancel = context.WithCancel(context.Background())
	workingDir = dir
	store := &cancellingStore{countingStore: countingStore{Store: useFileStore(t)}, cancel: cancel}
	store.Set("evren", testKey)
	useStore(t, store)
	code, _, _ = exec(t, ctx, vars, "run", "qwen")
	if code != 130 || s.calls != 0 || store.gets != 1 {
		t.Errorf("while reading the key: code %d, %d starts, %d reads", code, s.calls, store.gets)
	}
}

// cancellingStore cancels a context when the key is read.
type cancellingStore struct {
	countingStore
	cancel context.CancelFunc
}

func (s *cancellingStore) Get(account string) (string, error) {
	s.cancel()
	return s.countingStore.Get(account)
}

// cmdTool makes the test machine a Windows one where qwen is what npm
// installs there: a batch file at the given path.
func cmdTool(t *testing.T, path string) setup.Env {
	t.Helper()
	env := useSetupEnv(t, nil)
	env.GOOS = "windows"
	env.LookPath = func(name string) (string, error) {
		if name == "qwen" {
			return path, nil
		}
		return "", errors.New("not found")
	}
	useEnv(t, env)
	return env
}

// On Windows a tool installed by npm is a batch file, which only cmd.exe
// can run, and cmd.exe gives a meaning to characters that are ordinary in a
// prompt. Such an argument is refused before anything is read or started.
func TestRunRefusesWhatCmdWouldInterpret(t *testing.T) {
	shim := testBin("qwen.cmd")
	cmdTool(t, shim)
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	store := &countingStore{Store: useFileStore(t)}
	store.Set("evren", testKey)
	useStore(t, store)
	s := useFakeExec(t, 0, nil)
	const tail = ", which cannot be passed safely to a tool that Windows starts through cmd.exe; Qwen Code can be started without that argument\n"
	for _, tc := range []struct {
		args []string // the tool's arguments
		want string
	}{
		{[]string{"-p", `it"s broken & whoami`}, `ulgen: argument 2 contains "` + tail},
		{[]string{"what is %EVREN_API_KEY%"}, "ulgen: argument 1 contains %" + tail},
		{[]string{"ok", "fine", "a^b"}, "ulgen: argument 3 contains ^" + tail},
		{[]string{"wow!"}, "ulgen: argument 1 contains !" + tail},
		{[]string{"-p", "two\nlines"}, "ulgen: argument 2 contains a line break" + tail},
		{[]string{"-p", "two\rlines"}, "ulgen: argument 2 contains a line break" + tail},
		{[]string{"a\x01b"}, "ulgen: argument 1 contains \x01" + tail},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, append([]string{"run", "qwen"}, tc.args...)...)
		if code != 2 || stdout != "" || stderr != tc.want {
			t.Errorf("%q: code %d stdout %q stderr %q", tc.args, code, stdout, stderr)
		}
	}
	// Arguments that cmd.exe reads literally inside the quotes are passed on.
	for _, args := range [][]string{
		{"a&b"}, {"x|y"}, {"(see #12)"}, {"a<b>c"}, {"a\tb"},
	} {
		s.calls, store.gets = 0, 0
		if code, _, stderr := exec(t, context.Background(), vars, append([]string{"run", "qwen"}, args...)...); code != 0 || stderr != "" || s.calls != 1 {
			t.Errorf("%q should have been passed on: code %d stderr %q, %d starts", args, code, stderr, s.calls)
		}
	}
	// A command line longer than Windows allows is refused with its own
	// message, before anything starts.
	long := strings.Repeat("a", maxShimLine)
	s.calls, store.gets = 0, 0
	code, _, stderr := exec(t, context.Background(), vars, "run", "qwen", long)
	want := "ulgen: the arguments make a command line longer than the 8191 characters Windows allows, so Qwen Code cannot be started through cmd.exe; pass fewer or shorter arguments\n"
	if code != 2 || stderr != want || s.calls != 0 || store.gets != 0 {
		t.Errorf("a long command line: code %d stderr %q, %d starts, %d reads", code, stderr, s.calls, store.gets)
	}
	if s.calls != 0 || store.gets != 0 {
		t.Errorf("%d starts, the key was read %d times", s.calls, store.gets)
	}
	vars["LANG"] = "tr_TR.UTF-8"
	_, _, stderr = exec(t, context.Background(), vars, "run", "qwen", "-p", `it"s`)
	if stderr != "ulgen: 2. argümanda \" karakteri var; Windows'un cmd.exe üzerinden başlattığı bir araca bu karakter güvenle iletilemez. Qwen Code bu argüman olmadan başlatılabilir\n" {
		t.Errorf("Turkish: %q", stderr)
	}
	_, _, stderr = exec(t, context.Background(), vars, "run", "qwen", "a\nb")
	if stderr != "ulgen: 1. argümanda satır sonu var; Windows'un cmd.exe üzerinden başlattığı bir araca satır sonu güvenle iletilemez. Qwen Code bu argüman olmadan başlatılabilir\n" {
		t.Errorf("Turkish line break: %q", stderr)
	}
	delete(vars, "LANG")

	// What cmd.exe leaves alone is passed on, unchanged.
	s.calls = 0
	args := []string{"-p", "say hi, çalış; a=b", "", "--", "$HOME", "it's"}
	if code, _, stderr := exec(t, context.Background(), vars, append([]string{"run", "qwen"}, args...)...); code != 0 || stderr != "" || s.calls != 1 {
		t.Fatalf("allowed arguments: code %d stderr %q, %d starts", code, stderr, s.calls)
	}
	if s.binary != shim || strings.Join(s.argv[1:], "\x00") != strings.Join(args, "\x00") {
		t.Errorf("started %q with %q", s.binary, s.argv)
	}

	// Should the start itself find a character to refuse (it checks again
	// what it is given), the user gets the same answer.
	useFakeExec(t, 0, &shimArgError{Index: 3, Char: '%'})
	if code, _, stderr := exec(t, context.Background(), vars, "run", "qwen", "ok"); code != 2 || stderr != "ulgen: argument 3 contains %"+tail {
		t.Errorf("refused at the start: code %d stderr %q", code, stderr)
	}
}

// The refusal is about batch files on Windows only.
func TestRunPassesEverythingToAProgram(t *testing.T) {
	hostile := []string{"a&b", "x|y", "50%", "%PATH%", `it"s`, "a^b", "(x)", "wow!", "two\nlines"}
	for name, tc := range map[string]struct {
		goos, binary string
	}{
		"a program on Windows":        {"windows", testBin("qwen.exe")},
		"a batch file name elsewhere": {"linux", testBin("qwen.cmd")},
	} {
		env := cmdTool(t, tc.binary)
		env.GOOS = tc.goos
		useEnv(t, env)
		vars := signedIn(t)
		setUp(t, vars, "qwen")
		s := useFakeExec(t, 0, nil)
		code, _, stderr := exec(t, context.Background(), vars, append([]string{"run", "qwen"}, hostile...)...)
		if code != 0 || stderr != "" || s.calls != 1 || strings.Join(s.argv[1:], "\x00") != strings.Join(hostile, "\x00") {
			t.Errorf("%s: code %d stderr %q, %d starts with %q", name, code, stderr, s.calls, s.argv)
		}
	}
}

func TestRunRefusesABatchFileUnderAnUnsafePath(t *testing.T) {
	shim := filepath.Join(filepath.Dir(testBin("x")), "R&D tools", "qwen.cmd")
	cmdTool(t, shim)
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	s := useFakeExec(t, 0, nil)
	code, stdout, stderr := exec(t, context.Background(), vars, "run", "qwen")
	want := "ulgen: the path of Qwen Code, \"" + shim + "\", contains &, so Windows cannot start it safely through cmd.exe; install the tool in a directory without that character\n"
	if code != 2 || stdout != "" || stderr != want || s.calls != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d starts", code, stdout, stderr, s.calls)
	}
	vars["LANG"] = "tr_TR.UTF-8"
	_, _, stderr = exec(t, context.Background(), vars, "run", "qwen")
	want = "ulgen: Qwen Code aracının yolunda (\"" + shim + "\") & karakteri var; Windows bu aracı cmd.exe üzerinden güvenle başlatamaz. Aracı, yolunda bu karakter olmayan bir dizine kurun\n"
	if stderr != want {
		t.Errorf("Turkish: %q", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"run", "--help"}, {"run", "-h"}, {"run", "--trust-project", "-h"}} {
		code, stdout, stderr := exec(t, context.Background(), nil, args...)
		for _, w := range []string{
			"Usage:\n  ulgen run [options] <tool> [arguments...]\n",
			"Starts the tool with the EVREN key in its environment. Everything after the tool's name goes to the tool unchanged. Claude Code is started through a translator on this machine and does not get the key. On Windows",
			"a tool whose .cmd file is not the one npm writes is started through cmd.exe, and then an argument that contains one of \" % ^ ! or a control character, or a command line longer than 8191 characters, is refused.\n",
			"Tools: qwen, opencode, codex, claude\n\nOptions:\n  -base-url text  EVREN API base URL for a proxy or mirror (https, or http on this machine only)\n  -model text     the model to select as default (deepseek-v4.1-flash when empty)\n  -trust-project  start the tool even when a file of the project names the EVREN key or provider\n",
		} {
			if code != 0 || stderr != "" || !strings.Contains(stdout, w) {
				t.Errorf("%v: code %d stderr %q, stdout lacks %q:\n%s", args, code, stderr, w, stdout)
			}
		}
	}
	code, stdout, stderr := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "run", "--help")
	for _, w := range []string{
		"ulgen run [seçenekler] <araç> [argümanlar...]",
		"Araç adından sonraki her şey araca olduğu gibi iletilir. Claude Code bu makinede bir çevirici üzerinden başlatılır ve anahtarı almaz. Windows'ta",
		"içinde \" % ^ ! karakterlerinden biri ya da bir denetim karakteri bulunan argüman veya 8191 karakterden uzun bir komut satırı reddedilir.",
		"Araçlar: qwen, opencode, codex, claude",
		"-base-url metin  vekil ya da yansı için EVREN API temel URL'si (https ya da yalnızca bu makinede http)",
		"-model metin     varsayılan olarak seçilecek model (boşsa deepseek-v4.1-flash)",
		"-trust-project   projedeki bir dosyada EVREN anahtarının ya da sağlayıcısının adı geçse de aracı başlatır",
	} {
		if code != 0 || stderr != "" || !strings.Contains(stdout, w) {
			t.Errorf("Turkish: code %d stderr %q, stdout lacks %q:\n%s", code, stderr, w, stdout)
		}
	}
}
