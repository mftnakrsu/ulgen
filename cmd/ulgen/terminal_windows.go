//go:build windows

package main

import (
	"os"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptyPipe reports whether f is one end of a Cygwin or MSYS2 pseudo terminal.
// In the window of Git Bash (mintty) a Windows program has no console: its
// standard streams are named pipes whose names say that a terminal is at the
// other end.
func ptyPipe(f *os.File) bool {
	h := windows.Handle(f.Fd())
	if kind, err := windows.GetFileType(h); err != nil || kind != windows.FILE_TYPE_PIPE {
		return false
	}
	name, ok := pipeName(h)
	return ok && isPtyPipeName(name)
}

// pipeName returns the name of the pipe behind h as
// GetFileInformationByHandleEx reports it: the part after \\.\pipe, with a
// leading backslash. ok is false when Windows gives no name (an anonymous
// pipe of another kind, or a name longer than any pseudo terminal's).
func pipeName(h windows.Handle) (name string, ok bool) {
	// FILE_NAME_INFO: the length of the name in bytes, then the name in
	// UTF-16 without a final zero.
	var info struct {
		length uint32
		name   [windows.MAX_PATH]uint16
	}
	err := windows.GetFileInformationByHandleEx(h, windows.FileNameInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		return "", false
	}
	n := int(info.length / 2)
	if n > len(info.name) {
		return "", false
	}
	return string(utf16.Decode(info.name[:n])), true
}
