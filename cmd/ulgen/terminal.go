package main

import (
	"os"
	"regexp"

	"github.com/charmbracelet/x/term"
)

// terminalKind says what kind of terminal, if any, a standard stream is.
type terminalKind int

const (
	// notTerminal is a file, a pipe to another program, or nothing at all.
	notTerminal terminalKind = iota
	// consoleTerminal is a terminal the operating system knows as one: a tty,
	// or a Windows console. Echo can be switched off on it and the
	// interactive interface can draw on it.
	consoleTerminal
	// ptyPipeTerminal is a screen and a keyboard that a Windows program sees
	// only as a named pipe: the window of Git Bash, MSYS2 or Cygwin (mintty).
	// What is written to it appears on the screen, but the console functions
	// do not work on it, so echo cannot be switched off.
	ptyPipeTerminal
)

// terminalOf decides what kind of terminal f is. It is the one place in the
// program that answers "is this a terminal": "ulgen token" asks it before it
// prints the key and "ulgen login" before it reads the key.
func terminalOf(f *os.File) terminalKind {
	switch {
	case term.IsTerminal(f.Fd()):
		return consoleTerminal
	case ptyPipe(f):
		return ptyPipeTerminal
	}
	return notTerminal
}

// ptyPipeName matches the name of a pipe that is one end of a Cygwin or
// MSYS2 pseudo terminal.
var ptyPipeName = regexp.MustCompile(`^(?:\\Device\\NamedPipe)?\\(?:msys|cygwin)-[^-\\]+-pty[0-9]+-(?:from|to)-master(?:-[0-9A-Za-z]+)?$`)

// isPtyPipeName reports whether name is the name of a pipe behind a Cygwin
// or MSYS2 pseudo terminal, as Windows reports it for the handle:
//
//	\msys-dd50a72ab4668b33-pty0-to-master
//	\cygwin-e022582115c10879-pty1-from-master
//
// The part after the first hyphen identifies the installation and the
// number is the terminal's. Two variations are accepted as well: the prefix
// \Device\NamedPipe, which is how the same name reads through another
// Windows function, and one more word at the end, which some Cygwin
// releases append to the handles they give to Windows programs. Accepting
// too much only makes "ulgen token" ask for --show once more. An ordinary
// pipe of the same family ("\msys-…-pipe-0x3", what "ulgen token | cat"
// writes to) is not a terminal.
func isPtyPipeName(name string) bool {
	return ptyPipeName.MatchString(name)
}
