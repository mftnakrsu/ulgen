//go:build windows

package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// namedPipe creates a pipe with the given name (the part after \\.\pipe\)
// and returns its server end as a file.
func namedPipe(t *testing.T, name string) *os.File {
	t.Helper()
	path, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(path, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatalf("CreateNamedPipe(%s): %v", name, err)
	}
	f := os.NewFile(uintptr(h), name)
	t.Cleanup(func() { f.Close() })
	return f
}

// The window of Git Bash gives a Windows program pipes with telling names.
// The test makes pipes with such names (and with other names) and asks the
// real check, which reads the name back from Windows.
func TestPtyPipeIsRecognisedByItsName(t *testing.T) {
	// An installation id of its own, so that the names are free on any
	// machine: sixteen hexadecimal digits, as Cygwin and MSYS2 write them.
	id := fmt.Sprintf("%016x", uint64(os.Getpid())<<20|0xabcde)
	for name, want := range map[string]terminalKind{
		"msys-" + id + "-pty0-to-master":     ptyPipeTerminal,
		"msys-" + id + "-pty0-from-master":   ptyPipeTerminal,
		"cygwin-" + id + "-pty7-to-master":   ptyPipeTerminal,
		"cygwin-" + id + "-pty7-from-master": ptyPipeTerminal,
		"msys-" + id + "-1820-pipe-0x3":      notTerminal, // a pipe between two programs in that window
		"ulgen-test-" + id + "-to-master":    notTerminal,
	} {
		f := namedPipe(t, name)
		if got := terminalOf(f); got != want {
			got, _ := pipeName(windows.Handle(f.Fd()))
			t.Errorf("%s: terminalOf = %v, want %v (Windows calls the pipe %q)", name, terminalOf(f), want, got)
		}
		if got, ok := pipeName(windows.Handle(f.Fd())); !ok || got != `\`+name {
			t.Errorf("%s: pipeName = %q, %v", name, got, ok)
		}
	}
}

// An anonymous pipe and a file are not terminals, and asking does not fail
// on a handle that is no pipe at all.
func TestPtyPipeOnOtherHandles(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for name, f := range map[string]*os.File{"the read end of a pipe": r, "the write end of a pipe": w, "NUL": null} {
		if ptyPipe(f) || terminalOf(f) != notTerminal {
			t.Errorf("%s counts as a terminal", name)
		}
	}
}
