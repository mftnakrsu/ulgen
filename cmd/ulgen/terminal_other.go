//go:build !windows

package main

import "os"

// ptyPipe reports a terminal that the program can see only as a pipe. That
// happens on Windows alone; everywhere else a terminal is a tty.
func ptyPipe(*os.File) bool { return false }
