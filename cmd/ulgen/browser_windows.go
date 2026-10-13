//go:build windows

package main

import osexec "os/exec"

// detach is a no-op on Windows: the console has no session to leave.
func detach(cmd *osexec.Cmd) {}
