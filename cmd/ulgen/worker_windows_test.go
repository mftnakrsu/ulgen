//go:build windows

package main

import "testing"

// On Windows a path outside the home directory is named in a rule the way
// Claude Code names a drive: as a first directory, in lower case.
func TestWorkerReadRulePathWindowsDrive(t *testing.T) {
	home := `C:\Users\invented`
	for path, want := range map[string]string{
		`D:\srv\ulgen cfg`:             "//d/srv/ulgen cfg",
		`C:\ProgramData\ulgen`:         "//c/ProgramData/ulgen",
		`C:\Users\invented\.ssh`:       "~/.ssh",
		`C:\Users\invented\.config\gh`: "~/.config/gh",
		`C:\Users\invented-other\.ssh`: "//c/Users/invented-other/.ssh",
	} {
		if got := workerReadRulePath(home, path); got != want {
			t.Errorf("workerReadRulePath(%q) = %q, want %q", path, got, want)
		}
	}
}
