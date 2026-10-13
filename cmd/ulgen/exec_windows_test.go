//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// These tests run only on Windows. They start real processes, always the
// test binary itself: directly, and through a batch file of the kind npm
// writes for a tool.

// openDescriptors has nothing to report on Windows.
func openDescriptors() []int { return nil }

// platformHelper runs the helper modes that only Windows has:
//
//	ulgen-waits  be "ulgen run": start the test binary again in the mode
//	             "tool-waits" through platformExec, with the signal handling
//	             main sets up, and print the tool's exit code
//	tool-waits   be a tool that stays until the second Ctrl+Break, then
//	             exits with 7
func platformHelper(mode string) int {
	switch mode {
	case "ulgen-waits":
		_, stop := signalContext()
		defer stop()
		self, err := os.Executable()
		if err != nil {
			fmt.Println("helper:", err)
			return 98
		}
		code, err := platformExec(self, []string{self}, helperEnv(os.Environ(), "tool-waits"))
		fmt.Printf("ulgen saw the tool end: code=%d err=%v\n", code, err)
		return 0
	case "tool-waits":
		breaks := make(chan os.Signal, 2)
		signal.Notify(breaks, os.Interrupt)
		fmt.Println("tool ready")
		for n := 1; n <= 2; n++ {
			select {
			case <-breaks:
				fmt.Printf("tool got break %d\n", n)
			case <-time.After(30 * time.Second):
				return 3
			}
		}
		return 7
	}
	return 97
}

// writeShim writes a batch file with the text npm's cmd-shim writes for a
// tool (see npm/node_modules/cmd-shim/lib/index.js, the head constant and the
// cmd string of one that calls node): it calls :find_dp0 through CALL, keeps
// the result in dp0, and runs the program with %*. npm's "node" is the copy
// of the test binary that placeNode puts beside the shim; when there is none,
// a "node" is looked up on PATH. The file itself is plain ASCII; the program
// is found relative to the shim.
func writeShim(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "tool.cmd")
	if err := os.WriteFile(shim, []byte(npmShim()), 0o700); err != nil {
		t.Fatal(err)
	}
	placeNode(t, dir, "node.exe") // what the shim's IF EXIST finds
	return shim
}

// npmShim is npm's real cmd-shim text for a tool that runs node: the head
// constant verbatim, the IF EXIST/ELSE choice of the program, and the
// endLocal & goto line that runs "%_prog%" with the arguments %*. The only
// change is that the node npm would run is the copy of the test binary
// (node.exe beside the shim) or a node on PATH.
func npmShim() string {
	return "@ECHO off\r\n" +
		"GOTO start\r\n" +
		":find_dp0\r\n" +
		"SET dp0=%~dp0\r\n" +
		"EXIT /b\r\n" +
		":start\r\n" +
		"SETLOCAL\r\n" +
		"CALL :find_dp0\r\n" +
		"\r\n" +
		"IF EXIST \"%dp0%\\node.exe\" (\r\n" +
		"  SET \"_prog=%dp0%\\node.exe\"\r\n" +
		") ELSE (\r\n" +
		"  SET \"_prog=node\"\r\n" +
		"  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n" +
		")\r\n" +
		"\r\n" +
		"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\" %*\r\n"
}

// placeNode copies the test binary to dir\name, the way npm's shim finds
// node beside itself, and returns the path.
func placeNode(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// onlyInChild is set in the tool's environment and nowhere else; its value
// must never turn up in an argument.
const (
	onlyInChildVar   = "ULGEN_TEST_ONLY_IN_CHILD"
	onlyInChildValue = "expanded-by-cmd-exe"
)

// toolEnv is the environment a stand-in tool is started with: the test's
// own (cmd.exe needs SystemRoot and friends), the helper mode "report" with
// the file to report to, exit code 7, the key, and the marker variable.
func toolEnv(t *testing.T, report string) []string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(helperEnv(os.Environ(), "report"),
		helperSelfVar+"="+self, helperOutVar+"="+report, helperExitVar+"=7", onlyInChildVar+"="+onlyInChildValue)
	return withKey(env, testKey, true)
}

// readReport returns what the stand-in tool wrote.
func readReport(t *testing.T, path string) helperReport {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the tool did not report: %v", err)
	}
	var rep helperReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatalf("%v in %q", err, b)
	}
	return rep
}

// checkReport fails when the tool did not get exactly args, or got the key
// more than once, or got the value of the marker variable in an argument.
func checkReport(t *testing.T, name string, rep helperReport, args []string) {
	t.Helper()
	if len(rep.Args) != len(args) || (len(args) > 0 && !reflect.DeepEqual(rep.Args, args)) {
		t.Errorf("%s: the tool got %q, want %q", name, rep.Args, args)
	}
	if len(rep.Keys) != 1 || rep.Keys[0] != "EVREN_API_KEY="+testKey {
		t.Errorf("%s: the key in the tool's environment: %d entries", name, len(rep.Keys))
	}
	for _, arg := range rep.Args {
		if strings.Contains(arg, onlyInChildValue) || strings.Contains(arg, testKey) {
			t.Errorf("%s: a variable was expanded into the argument %q", name, arg)
		}
	}
}

// A batch file gets every allowed argument unchanged and its exit code is
// passed on, also when its own path has a space or Turkish letters in it.
func TestBatchFileGetsItsArgumentsUnchanged(t *testing.T) {
	cases := append(shimCases[:len(shimCases):len(shimCases)], []struct {
		name string
		args []string
		line string
	}{
		{name: "a slash option", args: []string{"/?", "/c", "/d"}},
		{name: "names of variables without percent signs", args: []string{onlyInChildVar, "$" + onlyInChildVar, "$env:" + onlyInChildVar, "EVREN_API_KEY", "~dp0", "*"}},
		{name: "spaces only", args: []string{" ", "  two  "}},
		{name: "many", args: strings.Fields(strings.Repeat("word ", 200))},
		// Closed inside the quotes, cmd.exe reads these literally.
		{name: "what cmd.exe reads literally inside quotes", args: []string{"a&b", "x|y", "(see #12)", "a<b>c", "fix foo() & bar | baz"}},
	}...)
	for _, dirName := range []string{"plain", "with a space", "çalışma dizini", "Program Files (x86)"} {
		shim := writeShim(t, filepath.Join(t.TempDir(), dirName))
		for _, tc := range cases {
			name := dirName + "/" + tc.name
			report := filepath.Join(t.TempDir(), "report.json")
			code, err := platformExec(shim, append([]string{shim}, tc.args...), toolEnv(t, report))
			if err != nil || code != 7 {
				t.Errorf("%s: code %d, err %v", name, code, err)
				continue
			}
			checkReport(t, name, readReport(t, report), tc.args)
		}
	}
}

// An argument cmd.exe would interpret is refused and nothing is started:
// no second command runs, and no variable of the tool's environment, the
// key least of all, is expanded into an argument.
func TestBatchFileIsNotStartedWithWhatCmdWouldInterpret(t *testing.T) {
	shim := writeShim(t, filepath.Join(t.TempDir(), "with a space"))
	marker := filepath.Join(t.TempDir(), "second-command-ran")
	for _, arg := range []string{
		"50%", "%PATH%", "%EVREN_API_KEY%", "%" + onlyInChildVar + "%", "!" + onlyInChildVar + "!",
		`it"s`, "a^b", "two\nlines", "two\r\nlines", "a\x01b",
		// A quotation mark inside the quotes would end them, letting a
		// following &, |, < or > start another command; the character
		// itself is what is refused.
		`x" & echo ran > "` + marker + `" & rem "`,
		`x"| echo ran > ` + marker,
	} {
		report := filepath.Join(t.TempDir(), "report.json")
		code, err := platformExec(shim, []string{shim, "-p", arg}, toolEnv(t, report))
		var bad *shimArgError
		if !errors.As(err, &bad) || bad.Index != 2 || code != 0 {
			t.Errorf("%q: code %d, err %v", arg, code, err)
		}
		if _, statErr := os.Stat(report); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%q: the tool was started (%v)", arg, statErr)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a second command ran (%v)", err)
	}
	// A batch file under a path with such a character is not started either.
	odd := writeShim(t, filepath.Join(t.TempDir(), "R&D (x86)"))
	report := filepath.Join(t.TempDir(), "report.json")
	_, err := platformExec(odd, []string{odd}, toolEnv(t, report))
	var bad *shimArgError
	if !errors.As(err, &bad) || bad.Index != 0 || bad.Char != '&' {
		t.Errorf("an unsafe path: err %v", err)
	}
	if _, statErr := os.Stat(report); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("an unsafe path: the tool was started (%v)", statErr)
	}
}

// A program (.exe) is started directly: cmd.exe is not involved and every
// argument arrives as it is, the ones a batch file is refused included.
func TestProgramGetsEveryArgumentUnchanged(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range append(shimCases[:len(shimCases):len(shimCases)], struct {
		name string
		args []string
		line string
	}{name: "what a batch file is refused", args: []string{"50%", "%PATH%", "%" + onlyInChildVar + "%", `it"s`, `a\"b`, "a^b", "wow!", "a\x01b", `tr\ailing\\`, `"`}}) {
		report := filepath.Join(t.TempDir(), "report.json")
		code, err := platformExec(self, append([]string{self}, tc.args...), toolEnv(t, report))
		if err != nil || code != 7 {
			t.Errorf("%s: code %d, err %v", tc.name, code, err)
			continue
		}
		checkReport(t, tc.name, readReport(t, report), tc.args)
	}
	missing := filepath.Join(t.TempDir(), "no-such-tool.exe")
	if code, err := platformExec(missing, []string{missing}, os.Environ()); err == nil || code != 0 {
		t.Errorf("a missing program: code %d, err %v", code, err)
	}
}

// A batch file is started with NoDefaultCurrentDirectoryInExePath in
// cmd.exe's environment, so a program the shim names without a path is not
// looked up in the working directory. The shim calls a bare helper-tool: a
// same-named file in the working directory would exit 9 (and would run
// whatever the project put there), the one on PATH exits 7.
func TestBatchFileDoesNotRunAPathShadowFromTheWorkingDir(t *testing.T) {
	dir := t.TempDir()
	// The real helper-tool on PATH: the test binary copied under that name.
	bin := t.TempDir()
	placeNode(t, bin, "helper-tool.exe")
	// The shadow in the working directory: a batch file of the same name
	// that ends with exit code 9.
	work := t.TempDir()
	shadow := filepath.Join(work, "helper-tool.cmd")
	if err := os.WriteFile(shadow, []byte("@ECHO off\r\nexit /b 9\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The shim calls helper-tool through CALL and passes the mode on.
	shim := filepath.Join(dir, "caller.cmd")
	body := "@ECHO off\r\ncall helper-tool %*\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	t.Setenv("PATH", bin+";"+os.Getenv("PATH"))
	// cmd.exe, not this process, must do the lookup, so toolCommand is used
	// directly (it is what a real start builds on).
	cmd, err := toolCommand(context.Background(), shim, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = withNoDefaultCurrentDir(append(helperEnv(os.Environ(), "version"), "PATH="+bin+";"+os.Getenv("PATH")))
	out, err := cmd.Output()
	if err != nil {
		var exit *osexec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 9 {
			t.Fatalf("cmd.exe ran the shadow from the working directory (exit 9); %s is missing from its environment", noDefaultCurrentDir)
		}
		t.Fatalf("the shim did not run: %v, out %q", err, out)
	}
	if !strings.Contains(string(out), "stand-in 1.2.3") {
		t.Errorf("the tool on PATH did not answer: %q", out)
	}
	// Without the variable, the shadow is found: the test measures what it
	// means to.
	plain, err := toolCommand(context.Background(), shim, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain.Env = append(helperEnv(os.Environ(), "version"), "PATH="+bin+";"+os.Getenv("PATH"))
	_, err = plain.Output()
	var exit *osexec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 9 {
		t.Errorf("without %s the shadow was not found (err %v); the test proves nothing", noDefaultCurrentDir, err)
	}
}

// The version probe goes through the same start as "ulgen run": a batch
// file answers through cmd.exe, and one under an unsafe path is not run.
func TestProbeVersionOfABatchFile(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperSelfVar, self)
	t.Setenv(helperVar, "version") // for the children only; this process is past TestMain
	// The probe of a batch file builds its environment from the process's
	// own (withNoDefaultCurrentDir); the two variables above have to be in it.
	saved := environ
	environ = os.Environ
	t.Cleanup(func() { environ = saved })
	shim := writeShim(t, filepath.Join(t.TempDir(), "with a space"))
	out, err := probeVersion(context.Background(), shim)
	if err != nil || !strings.Contains(out, "stand-in 1.2.3") {
		t.Errorf("probe: %q, %v", out, err)
	}
	odd := writeShim(t, filepath.Join(t.TempDir(), "100% (x86)"))
	var bad *shimArgError
	if out, err := probeVersion(context.Background(), odd); !errors.As(err, &bad) || out != "" {
		t.Errorf("an unsafe path: %q, %v", out, err)
	}
}

// Ctrl+Break (and Ctrl+C) goes to every process on the console. The tool
// decides what it means; Ülgen has to stay until the tool ends, at the
// second signal as much as at the first, and pass the tool's exit code on.
//
// The test starts "ulgen" (the test binary in the mode "ulgen-waits") in a
// process group of its own, so that the events it sends reach that process
// and the tool it started and nothing else.
func TestUlgenOutlivesCtrlBreakWhileTheToolRuns(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := osexec.Command(self)
	cmd.Env = helperEnv(os.Environ(), "ulgen-waits")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := uint32(cmd.Process.Pid)
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	done := make(chan error, 1)
	// expect waits for the next line of output and fails the test, ending
	// the process this test started, when another line or none comes.
	expect := func(want string) bool {
		t.Helper()
		select {
		case line, ok := <-lines:
			if ok && line == want {
				return true
			}
			t.Errorf("got %q (open %v), want %q", line, ok, want)
		case <-time.After(20 * time.Second):
			t.Errorf("no output for 20 s, want %q", want)
		}
		cmd.Process.Kill()
		return false
	}
	// sendBreak sends one Ctrl+Break to the process group. The first call
	// may skip the test: a test process without a console can never send one.
	// A later call must not skip — the console was there a moment ago, so a
	// failure now is the test's own to report.
	sendBreak := func(first bool) bool {
		t.Helper()
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, pid); err != nil {
			if first {
				cmd.Process.Kill()
				go func() { done <- cmd.Wait() }()
				<-done
				t.Skipf("this process has no console to send Ctrl+Break through: %v", err)
			}
			t.Errorf("the second Ctrl+Break could not be sent: %v", err)
			return false
		}
		return true
	}
	ok := expect("tool ready") && sendBreak(true) && expect("tool got break 1")
	if ok {
		// Give "ulgen" time to act on the first signal the way main does
		// (its own registration ends), so that the second one finds it in
		// the state a second Ctrl+C would.
		time.Sleep(500 * time.Millisecond)
		ok = sendBreak(false) && expect("tool got break 2") && expect("ulgen saw the tool end: code=7 err=<nil>")
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ok && err != nil {
			t.Errorf("ulgen did not end normally: %v", err)
		}
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		t.Error("ulgen did not end")
	}
}

// writeNpmShim writes npm's shim for a tool whose script is
// node_modules\tool\cli.js, with the script and, as node.exe beside the
// shim, a copy of the test binary: it reports the arguments it was given.
func writeNpmShim(t *testing.T, dir string) (shim, script string) {
	t.Helper()
	script = filepath.Join(dir, "node_modules", "tool", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("// the tool\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim = filepath.Join(dir, "tool.cmd")
	text := strings.Replace(npmShim(), `"%_prog%" %*`, `"%_prog%"  "%dp0%\node_modules\tool\cli.js" %*`, 1)
	if err := os.WriteFile(shim, []byte(text), 0o700); err != nil {
		t.Fatal(err)
	}
	placeNode(t, dir, "node.exe")
	return shim, script
}

// npm's own shim is not run through cmd.exe: node gets the script and every
// argument as it is, the ones cmd.exe would interpret included, and no
// variable is expanded into them.
func TestNpmShimIsStartedWithoutCmd(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "second-command-ran")
	args := []string{
		"50%", "%PATH%", "%EVREN_API_KEY%", "%" + onlyInChildVar + "%", "!" + onlyInChildVar + "!",
		`it"s`, `say "hi"`, "a^b", "two\nlines", "a&b", "x|y", `trailing\`, "",
		`x" & echo ran > "` + marker + `" & rem "`,
	}
	for _, dirName := range []string{"plain", "with a space", "çalışma dizini", "R&D (x86)"} {
		shim, script := writeNpmShim(t, filepath.Join(t.TempDir(), dirName))
		report := filepath.Join(t.TempDir(), "report.json")
		code, err := platformExec(shim, append([]string{shim}, args...), toolEnv(t, report))
		if err != nil || code != 7 {
			t.Errorf("%s: code %d, err %v", dirName, code, err)
			continue
		}
		rep := readReport(t, report)
		if len(rep.Args) != len(args)+1 || rep.Args[0] != script || !reflect.DeepEqual(rep.Args[1:], args) {
			t.Errorf("%s: the tool got %q", dirName, rep.Args)
		}
		if len(rep.Keys) != 1 || rep.Keys[0] != "EVREN_API_KEY="+testKey {
			t.Errorf("%s: the key in the tool's environment: %d entries", dirName, len(rep.Keys))
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a second command ran (%v)", err)
	}
}

// A shim whose script is missing, or that is not npm's to the letter, is
// started through cmd.exe as before, with its refusals.
func TestOtherShimsStillGoThroughCmd(t *testing.T) {
	shim, script := writeNpmShim(t, filepath.Join(t.TempDir(), "gone"))
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := npmShimTarget(shim); ok {
		t.Error("a shim without its script was taken apart")
	}
	_, err := platformExec(shim, []string{shim, "50%"}, toolEnv(t, filepath.Join(t.TempDir(), "report.json")))
	var bad *shimArgError
	if !errors.As(err, &bad) {
		t.Errorf("err = %v, want the refusal of cmd.exe's path", err)
	}
	plain := writeShim(t, filepath.Join(t.TempDir(), "plain"))
	if _, _, ok := npmShimTarget(plain); ok {
		t.Error("a shim that names no script was taken apart")
	}
}
