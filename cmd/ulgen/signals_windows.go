//go:build windows

package main

import (
	"os"
	"syscall"
)

// terminationSignals end a run: Ctrl+C and a polite kill.
var terminationSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
