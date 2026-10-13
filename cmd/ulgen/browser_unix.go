//go:build !windows

package main

import (
	osexec "os/exec"
	"syscall"
)

// detach starts the opener in its own session, so a Ctrl+C in the terminal
// does not reach a browser that xdg-open has just started.
func detach(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
