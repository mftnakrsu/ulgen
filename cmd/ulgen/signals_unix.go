//go:build !windows

package main

import (
	"os"
	"syscall"
)

// terminationSignals end a run: Ctrl+C, a polite kill, and a closed terminal.
var terminationSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
