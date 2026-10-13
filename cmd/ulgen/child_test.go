package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runChildReport starts the test binary as a stand-in tool through runChild
// and returns what it reported: the process it ran as, the arguments it saw
// and its whole environment. The report goes to a file, so nothing of the
// child reaches the test's own standard output.
func runChildReport(t *testing.T, args []string, env []string) helperReport {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "report")
	childEnv := append([]string{helperVar + "=report", helperEnvVar + "=1", helperOutVar + "=" + out, helperExitVar + "=0"}, env...)
	if code, err := runChild(self, append([]string{self}, args...), childEnv); err != nil || code != 0 {
		t.Fatalf("runChild: code %d err %v", code, err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep helperReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatalf("%v in %q", err, b)
	}
	return rep
}

// The child runs as a separate process of its own, with the arguments it was
// given and nothing else, and the environment is exactly the one passed: a
// variable of the parent's that is not in it does not reach the child.
func TestRunChildStartsWithTheGivenArgumentsAndEnvironment(t *testing.T) {
	rep := runChildReport(t, []string{"-p", "say hi", "", "a&b"}, []string{"LANG=C", "ULGEN_ONLY=1"})
	if got := strings.Join(rep.Args, "\x00"); got != strings.Join([]string{"-p", "say hi", "", "a&b"}, "\x00") {
		t.Errorf("arguments = %q", rep.Args)
	}
	joined := "|" + strings.Join(rep.Env, "|") + "|"
	for _, want := range []string{"|LANG=C|", "|ULGEN_ONLY=1|"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the child's environment lacks %q: %q", want, joined)
		}
	}
	// A variable the test did not pass is not there: the environment is the
	// one given, not the parent's.
	if strings.Contains(joined, "|EVREN_API_KEY=") {
		t.Errorf("the child got an EVREN key: %q", joined)
	}
}

// A child that ends with a non-zero code is not an error: the code is
// returned as it is.
func TestRunChildPassesOnTheExitCode(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "report")
	env := []string{helperVar + "=report", helperOutVar + "=" + out, helperExitVar + "=7"}
	code, err := runChild(self, []string{self}, env)
	if err != nil || code != 7 {
		t.Errorf("code %d err %v, want 7, nil", code, err)
	}
}

// A program that cannot be started is an error and no code.
func TestRunChildReportsAFailedStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tool")
	code, err := runChild(missing, []string{missing}, []string{"A=1"})
	if err == nil || code != 0 {
		t.Errorf("code %d err %v, want 0 and an error", code, err)
	}
}
