//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

// platformHelper has the modes that exist only on Unix:
//
//	exec-signals  register for termination signals as main does, then hand
//	              the process to the test binary in the mode "signals"
//	signals       report, as the tool, whether SIGHUP and SIGINT are ignored
func platformHelper(mode string) int {
	switch mode {
	case "exec-signals":
		return helperExecSignals()
	case "signals":
		return helperWriteSignals()
	}
	return 97
}

// helperExecSignals is the mode "exec-signals": it does what "ulgen run"
// does, from a process whose caller had ignored SIGHUP and SIGINT, including
// the signal registration main makes before it hands over.
func helperExecSignals() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	_, _ = signalContext()
	code, err := platformExec(self, []string{self}, helperEnv(os.Environ(), "signals"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper: platformExec:", err)
		return 98
	}
	return code
}

// helperWriteSignals is the mode "signals": the tool reports whether the two
// signals a caller may have ignored still are. It sends itself both first,
// so a disposition that is not "ignored" is seen for what it is.
func helperWriteSignals() int {
	rep := map[string]bool{"HUP": signal.Ignored(syscall.SIGHUP), "INT": signal.Ignored(syscall.SIGINT)}
	syscall.Kill(os.Getpid(), syscall.SIGHUP)
	syscall.Kill(os.Getpid(), syscall.SIGINT)
	b, err := json.Marshal(rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		return 98
	}
	os.Stdout.Write(b)
	return 0
}

// A signal the caller had ignored is ignored again when the tool takes over:
// the registration main made must not turn an inherited ignore into a caught
// signal, which exec would reset to the default.
func TestPlatformExecLeavesIgnoredSignalsIgnored(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
	t.Cleanup(func() { signal.Reset(syscall.SIGHUP, syscall.SIGINT) })
	cmd := osexec.Command(self)
	cmd.Env = []string{helperVar + "=exec-signals", "LANG=C"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the tool did not report its signals: %v, stderr %q", err, stderrOf(err))
	}
	var rep map[string]bool
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("%v in %q", err, out)
	}
	if !rep["HUP"] || !rep["INT"] {
		t.Errorf("the tool's signals: SIGHUP ignored %v, SIGINT ignored %v; both were ignored before the hand-over", rep["HUP"], rep["INT"])
	}
}

// openDescriptors lists the file descriptors above standard error that are
// open in this process.
func openDescriptors() []int {
	var open []int
	for fd := 3; fd < 64; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil {
			open = append(open, fd)
		}
	}
	return open
}

// The real platformExec, with the test binary as the tool: the process is
// replaced, so the tool has Ülgen's process id, working directory and
// standard streams and nothing else of it; the arguments arrive as they
// were given, the key is in the environment exactly once, and the exit code
// is the tool's.
func TestPlatformExecHandsTheProcessOver(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	args := []string{"-p", "say hi", "", "--", "-x", `q"uo` + "`te$HOME", "a&b|c", "%PATH%", "çğıöşü İ", " lead and trail ", "new\nline", `back\slash\`}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	const stale = "evren_llm_an_older_export"
	// start runs the test binary as a helper in the given mode and returns
	// what the stand-in tool reported, with the process id the test started.
	start := func(mode string, direct []string) (helperReport, int) {
		t.Helper()
		cmd := osexec.Command(self, direct...)
		cmd.Dir = dir
		cmd.Env = []string{
			helperVar + "=" + mode, helperArgsVar + "=" + string(argsJSON), helperKeyVar + "=" + testKey,
			helperExitVar + "=7", "EVREN_API_KEY=" + stale, "LANG=C",
		}
		out, err := cmd.Output()
		var exit *osexec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 7 {
			t.Fatalf("mode %s: the exit code was not passed on: %v, stderr %q", mode, err, stderrOf(err))
		}
		var rep helperReport
		if err := json.Unmarshal(out, &rep); err != nil {
			t.Fatalf("mode %s: %v in %q", mode, err, out)
		}
		return rep, cmd.Process.Pid
	}
	// The stand-in started directly is the measure: whatever a fresh process
	// of this binary has open, it has open there too.
	plain, _ := start("report", args)
	tool, pid := start("exec", nil)

	if !reflect.DeepEqual(tool.Args, args) {
		t.Errorf("the arguments changed:\n got  %q\n want %q", tool.Args, args)
	}
	if tool.Arg0 != self {
		t.Errorf("argv[0] = %q, want %q", tool.Arg0, self)
	}
	if tool.Pid != pid {
		t.Errorf("the tool runs as process %d, not as the process that was started (%d)", tool.Pid, pid)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got, _ := filepath.EvalSymlinks(tool.Dir); got != want || tool.Dir != plain.Dir {
		t.Errorf("working directory %q, want %q", tool.Dir, plain.Dir)
	}
	if len(tool.Keys) != 1 || tool.Keys[0] != "EVREN_API_KEY="+testKey {
		t.Errorf("the key in the tool's environment: %q", tool.Keys)
	}
	if len(plain.Keys) != 1 || plain.Keys[0] != "EVREN_API_KEY="+stale {
		t.Errorf("the measure is off: %q", plain.Keys)
	}
	if !reflect.DeepEqual(tool.Open, plain.Open) {
		t.Errorf("open descriptors above standard error: %v, a plain start has %v", tool.Open, plain.Open)
	}
}

// stderrOf returns what a failed command wrote to standard error.
func stderrOf(err error) string {
	var exit *osexec.ExitError
	if errors.As(err, &exit) {
		return string(exit.Stderr)
	}
	return ""
}

// A program that cannot be started is an error, not a new process.
func TestPlatformExecReportsAFailedStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tool")
	code, err := platformExec(missing, []string{missing}, []string{"A=1"})
	if err == nil || !errors.Is(err, os.ErrNotExist) || code != 0 {
		t.Errorf("code %d, err %v", code, err)
	}
}
