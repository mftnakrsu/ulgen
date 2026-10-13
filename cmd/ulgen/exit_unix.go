//go:build !windows

package main

import (
	"os"
	osexec "os/exec"
	"syscall"
)

// exitCodeOf turns a finished child's exit error into the code a shell would
// report. A child ended by a signal has ExitCode -1; the code is then 128 plus
// the signal's number, as a shell reports it (SIGKILL → 137).
func exitCodeOf(exit *osexec.ExitError) int {
	if code := exit.ExitCode(); code != -1 {
		return code
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return -1
}

// terminateChild asks a child that reached its deadline to end: SIGTERM, which
// a program may catch and wind down on. A worker's child runs in a process
// group of its own (see childSignals), so the signal goes to the whole group
// and everything the child started comes down with it. A child in Ülgen's own
// group ("ulgen run claude", whose child stays in the terminal's foreground)
// is signalled directly.
func terminateChild(cmd *osexec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if processGroupOf(cmd) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
}

// killChild ends a child that did not end after the termination signal, the
// whole process group when the child leads one.
func killChild(cmd *osexec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if processGroupOf(cmd) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}

// sweepChildGroup kills what is left of the process group of a child that has
// already ended, when the child led one. The group's id stays taken for as
// long as one member lives, so the signal reaches only what the child started;
// with no member left it reaches nothing.
func sweepChildGroup(cmd *osexec.Cmd) {
	if processGroupOf(cmd) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// processGroupOf reports whether the child leads a process group of its own,
// which no process can join. It is so only when the child was started with a
// positive group id; a process that is not there yet has no group to signal,
// and the caller's own group must never be signalled by mistake.
func processGroupOf(cmd *osexec.Cmd) bool {
	return cmd.Process != nil && cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid
}

// workerProcessGroup returns the process attributes a worker's child is
// started with: a process group of its own, led by the child, so that a
// termination signal or a kill can go to -pid and reach everything the child
// started. "ulgen run claude" keeps the child in the terminal's foreground
// group instead and does not use this.
func workerProcessGroup(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Setpgid = true
	return attr
}

// signalGroup passes a signal to the child's whole process group when the
// child leads one, and reports whether it did. Anything the group cannot
// reach (a process that is already gone) is ignored, as a direct signal's
// failure is.
func signalGroup(cmd *osexec.Cmd, sig os.Signal) bool {
	if !processGroupOf(cmd) {
		return false
	}
	s, ok := sig.(syscall.Signal)
	if !ok {
		return false
	}
	_ = syscall.Kill(-cmd.Process.Pid, s)
	return true
}
