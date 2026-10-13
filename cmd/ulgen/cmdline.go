package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// This file builds the command line for a tool that Windows starts through
// cmd.exe, and the environment that start is given. It has no build
// constraint, although only Windows uses it, so that its tests run on every
// system.
//
// npm installs qwen, opencode and codex as batch files (qwen.cmd), and
// Windows runs a batch file by handing the whole command line to cmd.exe.
// The quoting Go applies to arguments is the one programs written in C
// expect; cmd.exe reads a line differently: %NAME% is replaced by a variable
// even inside quotes, a quotation mark inside an argument ends the quoting,
// and a line break ends the command. Inside the double quotes Ülgen puts
// around every argument, however, cmd.exe reads & | < > ( ) literally, so an
// argument such as "a&calc.exe" is passed on unchanged rather than run as a
// second command. Ülgen writes the line itself and refuses the few
// characters it cannot pass safely (see checkShimArgs) instead of escaping
// them.

// noDefaultCurrentDir is the variable Windows reads to decide whether a
// program named without a path may be looked up in the current directory.
// cmd.exe would otherwise find a same-named file in the directory the command
// runs in before the installed tool, so the environment given to cmd.exe for
// a batch file always sets it. It is added only when the caller has not set
// it: a variable's name on Windows does not tell the cases apart.
const noDefaultCurrentDir = "NoDefaultCurrentDirectoryInExePath"

// withNoDefaultCurrentDir returns env with noDefaultCurrentDir set, unless it
// already sets it (in any case). It is the system-independent half of what
// the Windows start and version probe do, so that this part is tested on
// every system.
func withNoDefaultCurrentDir(env []string) []string {
	name := noDefaultCurrentDir + "="
	for _, kv := range env {
		if len(kv) >= len(name) && strings.EqualFold(kv[:len(name)], name) {
			return env
		}
	}
	out := make([]string, 0, len(env)+1)
	out = append(out, env...)
	return append(out, name+"1")
}

// shimArgError says a tool cannot be started through cmd.exe as asked,
// because of one character. It names the place, never the text: an argument
// can be a prompt that holds anything.
type shimArgError struct {
	// Index is 0 for the path of the batch file and n for the nth argument.
	Index int
	// Char is the character that is refused.
	Char rune
}

// Error names the place and the character.
func (e *shimArgError) Error() string {
	if e.Index == 0 {
		return fmt.Sprintf("the path of the batch file contains %q, which cmd.exe would interpret", e.Char)
	}
	return fmt.Sprintf("argument %d contains %q, which cmd.exe would interpret", e.Index, e.Char)
}

// shimLineError says the command line built for cmd.exe is longer than
// Windows allows a command line to be.
type shimLineError struct {
	// Length is the length of the line that was built, in characters.
	Length int
}

// Error names the length.
func (e *shimLineError) Error() string {
	return fmt.Sprintf("the command line for cmd.exe is %d characters long", e.Length)
}

// maxShimLine is the longest command line Windows passes to a program. A
// longer one is refused before anything is started.
const maxShimLine = 8191

// unsafeInArg reports whether r has to be refused inside a double-quoted
// argument to cmd.exe:
//
//   - " ends the quoting;
//   - % is replaced by a variable's value even inside quotes;
//   - ! is replaced by a variable's value when delayed expansion is on, which
//     a batch file may turn on for itself;
//   - ^ is an escape for the CALL the batch file shims are started through;
//   - a control character (below U+0020), a line break included, ends the
//     command.
//
// Inside quotes cmd.exe reads & | < > ( ) literally, so they are passed on;
// a tab is not special either.
func unsafeInArg(r rune) bool {
	switch r {
	case '"', '%', '!', '^':
		return true
	case '\t':
		return false
	}
	return r < 0x20
}

// unsafeInPath reports whether r has to be refused in the path of the batch
// file itself. The path is quoted as well, but cmd.exe looks at the whole
// line before the quotes are stripped, so & is refused there too. Parentheses
// are common in an installed tool's path (C:\Program Files (x86)) and are
// read literally inside the quotes, so they are allowed.
func unsafeInPath(r rune) bool {
	return r == '&' || unsafeInArg(r)
}

// isBatchFile reports whether path names a batch file, which Windows runs
// through cmd.exe: its extension is .cmd or .bat, in any case. Windows drops
// trailing spaces and dots from a file name before it looks at the extension,
// so "qwen.cmd " and "qwen.CMD." name the same batch file and are treated as
// one.
func isBatchFile(path string) bool {
	lower := strings.ToLower(strings.TrimRight(path, " ."))
	return strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat")
}

// checkShimArgs returns the first reason why the batch file shim cannot be
// started through cmd.exe with args, or nil when it can.
func checkShimArgs(shim string, args []string) *shimArgError {
	for _, r := range shim {
		if unsafeInPath(r) {
			return &shimArgError{Index: 0, Char: r}
		}
	}
	for n, arg := range args {
		for _, r := range arg {
			if unsafeInArg(r) {
				return &shimArgError{Index: n + 1, Char: r}
			}
		}
	}
	return nil
}

// shimCommandLine returns the command line that starts the batch file shim
// with args through cmd.exe:
//
//	cmd.exe /d /s /c ""C:\path\tool.cmd" "first" "second""
//
// /d skips the AutoRun commands from the registry. /s makes cmd.exe strip
// exactly the outer pair of quotes and run the rest as it stands, which is
// the one form that survives a space in the path together with a space in
// an argument. Every argument, and the path, is put in double quotes; inside
// them cmd.exe reads spaces and ; , = literally, and & | < > ( ) have no
// special meaning. An argument or a path with a character cmd.exe would
// interpret is a *shimArgError, and a line longer than maxShimLine is a
// *shimLineError; neither gives a line.
func shimCommandLine(shim string, args []string) (string, error) {
	if bad := checkShimArgs(shim, args); bad != nil {
		return "", bad
	}
	var b strings.Builder
	b.WriteString(`cmd.exe /d /s /c "`)
	b.WriteString(`"` + shim + `"`)
	for _, arg := range args {
		b.WriteString(" " + quoteShimArg(arg))
	}
	b.WriteString(`"`)
	line := b.String()
	if n := utf8.RuneCountInString(line); n > maxShimLine {
		return "", &shimLineError{Length: n}
	}
	return line, nil
}

// quoteShimArg puts one argument in double quotes. cmd.exe hands the text to
// the batch file as it is, and the batch file hands it on (%*) to a program
// that splits its command line by the C rules, where backslashes right
// before a quotation mark escape it. So the backslashes at the end of the
// argument, which stand before the closing quote, are doubled; all others
// are literal for both readers. The argument holds no quotation mark of its
// own (checkShimArgs).
func quoteShimArg(arg string) string {
	trailing := len(arg) - len(strings.TrimRight(arg, `\`))
	return `"` + arg + strings.Repeat(`\`, trailing) + `"`
}

// npmShimRun is the line of npm's cmd-shim that runs the tool: node, the
// script beside the shim, and the caller's arguments. The older form has
// only the command; the newer one ends the batch file's own scope first. A
// shim whose line has anything else (another program, an option for node, a
// script outside node_modules) does not match.
var npmShimRun = regexp.MustCompile(`^(?:endLocal & goto #_undefined_# 2>NUL \|\| title %COMSPEC% & )?"%_prog%"\s+"%dp0%\\(node_modules\\[^"%!^&|<>\r\n]+)"\s+%\*$`)

// npmShimHead is everything npm's cmd-shim writes in front of that line: it
// finds the shim's own directory and picks node, the copy beside the shim
// when there is one, else the node on PATH.
const npmShimHead = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)
`

// npmShimScript reads a batch file and, when it is npm's shim for a tool
// that runs on node, returns the script it starts, as a path relative to the
// shim's directory (node_modules\...\cli.js). Such a shim only picks node and
// runs "node <script> <arguments>", so Ülgen can do the same without
// cmd.exe, and then no argument has to be refused. Anything that is not
// recognised with certainty gives ok false, and the batch file is started
// through cmd.exe as before: the text in front of the last line must be
// npm's, to the letter (line breaks and blank lines aside), so a shim that
// does anything more is not taken apart. A script path that climbs out of
// its directory is not recognised either.
func npmShimScript(shim []byte) (script string, ok bool) {
	lines := shimLines(string(shim))
	head := shimLines(npmShimHead)
	if len(lines) != len(head)+1 || !slices.Equal(lines[:len(head)], head) {
		return "", false
	}
	m := npmShimRun.FindStringSubmatch(lines[len(head)])
	if m == nil {
		return "", false
	}
	for _, part := range strings.Split(m[1], `\`) {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return m[1], true
}

// shimLines returns the lines of a batch file that are not blank, without
// the spaces at their ends.
func shimLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
