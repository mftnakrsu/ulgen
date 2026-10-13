// Package termsafe makes untrusted text safe to print to a terminal. Model
// output, tool names, tool output and server error messages can all contain
// escape sequences; printed raw they could move the cursor, change the
// window title or hide text, so every front end passes them through here.
package termsafe

import (
	"regexp"
	"strings"
)

// escape matches OSC sequences (window title, hyperlinks), CSI sequences
// (cursor movement, colours) and any other two-character escape.
var escape = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b.")

// Sanitize removes escape sequences and control characters other than tab
// and newline. A carriage return is removed too, so CRLF text prints as LF.
func Sanitize(s string) string {
	if !needsWork(s) {
		return s
	}
	s = escape.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

func needsWork(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}
