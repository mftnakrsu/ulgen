//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// workerProcessGroup returns the process attributes a worker's child is
// started with. Windows has no process groups to lead, so the child's tree is
// taken down with taskkill instead (see killChild); the attributes are left as
// they are.
func workerProcessGroup(attr *syscall.SysProcAttr) *syscall.SysProcAttr { return attr }

// signalGroup has no Windows counterpart: Windows delivers Ctrl+C to every
// process on the console, so there is no group to signal; forwardSignal falls
// back to signalling the child alone.
func signalGroup(*osexec.Cmd, os.Signal) bool { return false }

// sweepChildGroup does nothing on Windows: the tree was taken down by taskkill
// while the child was alive, and the id of a process that has ended may
// already belong to another one.
func sweepChildGroup(*osexec.Cmd) {}

// taskkillTimeout bounds "taskkill /T /F", which itself never takes long.
// When it cannot be started in time, the process is killed directly instead.
const taskkillTimeout = 5 * time.Second

// exitCodeOf returns the code Windows reports for a finished child. Windows has
// no signal exit codes, so the code the system gives is used as it is.
func exitCodeOf(exit *osexec.ExitError) int {
	return exit.ExitCode()
}

// terminateChild ends a child that reached its deadline. Windows has no
// termination signal, so the child is killed; the design says so ("on Windows
// the kill").
func terminateChild(cmd *osexec.Cmd) { killChild(cmd) }

// killChild ends a child and everything it started with
// "taskkill /T /F /PID <pid>", which Windows runs against the whole process
// tree; a plain kill would leave the child's own children behind. When
// taskkill cannot be started, the process itself is killed, as it was before.
func killChild(cmd *osexec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if taskkillTree(cmd.Process.Pid) {
		return
	}
	_ = cmd.Process.Kill()
}

// taskkillTree runs "taskkill /T /F /PID pid" within taskkillTimeout and
// reports whether it was started and ended successfully.
func taskkillTree(pid int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), taskkillTimeout)
	defer cancel()
	// The program is named by its full path under the system directory, so a
	// taskkill found earlier on PATH is never the one that runs.
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	taskkill := filepath.Join(root, "System32", "taskkill.exe")
	err := osexec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
	return err == nil
}
