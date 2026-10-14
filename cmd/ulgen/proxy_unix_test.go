//go:build !windows

package main

import (
	osexec "os/exec"
	"strings"
	"testing"
)

// The lines "ulgen proxy --print-env" prints are pasted into a shell that then
// starts Claude Code. Run by a POSIX shell, every variable reaches the program
// the shell starts, with its value unchanged, a value with a quote in it
// included. (An assignment without export stays in the shell, and Claude Code
// was started without the translator's address.)
func TestPrintEnvLinesReachTheShellsChildren(t *testing.T) {
	s := translatorSetup{def: "deepseek-v4.1-flash", small: "deepseek-v4-flash", maxOutputs: 65536, context: 1048576}
	token := `tok'en"$HOME`
	script := claudeEnvLines("http://127.0.0.1:41234", token, s, "posix") + "env\n"
	out, err := osexec.Command("/bin/sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("sh: %v", err)
	}
	got := strings.Split(string(out), "\n")
	for _, want := range []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:41234",
		"ANTHROPIC_AUTH_TOKEN=" + token,
		"ANTHROPIC_MODEL=deepseek-v4.1-flash",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-v4-flash",
		"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=16000",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS=1048576",
	} {
		if !hasLine(got, want) {
			t.Errorf("the shell's child lacks %q; the lines were:\n%s", want, script)
		}
	}
}

// hasLine reports whether lines holds want as one whole line.
func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
