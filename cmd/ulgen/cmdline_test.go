package main

import (
	"errors"
	"strings"
	"testing"
)

func TestIsBatchFile(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\Users\u\AppData\Roaming\npm\qwen.cmd`: true,
		`C:\tools\codex.CMD`:                      true,
		`C:\tools\run.bat`:                        true,
		`C:\tools\run.Bat`:                        true,
		"/usr/bin/qwen.cmd":                       true,
		`C:\tools\opencode.exe`:                   false,
		`C:\tools\qwen`:                           false,
		`C:\tools\qwen.cmd.exe`:                   false,
		`C:\tools.cmd\qwen.exe`:                   false,
		`C:\tools\qwen.cmd `:                      true, // Windows drops the trailing space
		`C:\tools\qwen.CMD.`:                      true, // and the trailing dot
		`C:\tools\qwen.cmd.. `:                    true,
		".cmd":                                    true,
		"cmd":                                     false,
		"":                                        false,
	} {
		if got := isBatchFile(path); got != want {
			t.Errorf("isBatchFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// shimCases are argument lists a batch file must receive unchanged; the
// Windows-only test sends the same lists through a real .cmd file.
var shimCases = []struct {
	name string
	args []string
	line string // the part of the command line after the path of the batch file
}{
	{"none", nil, ``},
	{"plain", []string{"-p", "hi"}, ` "-p" "hi"`},
	{"spaces", []string{"-p", "say hi to them"}, ` "-p" "say hi to them"`},
	{"Turkish letters", []string{"çalış", "İIıi ŞşĞğÜüÖöÇç"}, ` "çalış" "İIıi ŞşĞğÜüÖöÇç"`},
	{"an empty argument", []string{"a", "", "b"}, ` "a" "" "b"`},
	{"only an empty argument", []string{""}, ` ""`},
	{"options", []string{"-x", "--", "--flag=value", "-"}, ` "-x" "--" "--flag=value" "-"`},
	{"a path with a space", []string{`C:\Users\Ayşe Yılmaz\my project\main.go`}, ` "C:\Users\Ayşe Yılmaz\my project\main.go"`},
	{"a backslash at the end", []string{`C:\dir\`, `next`}, ` "C:\dir\\" "next"`},
	{"backslashes at the end", []string{`a\\`}, ` "a\\\\"`},
	{"backslashes inside", []string{`a\b\\c`, `\\server\share`}, ` "a\b\\c" "\\server\share"`},
	{"only a backslash", []string{`\`}, ` "\\"`},
	{"shell characters cmd leaves alone", []string{`it's`, "a;b,c=d", "$HOME", "`x`", "*?~#@[]{}+:/"}, ` "it's" "a;b,c=d" "$HOME" "` + "`x`" + `" "*?~#@[]{}+:/"`},
	{"a tab", []string{"a\tb"}, " \"a\tb\""},
	// Inside the double quotes cmd.exe reads these literally, so they reach
	// the tool unchanged.
	{"ampersand", []string{"a&b"}, ` "a&b"`},
	{"pipe", []string{"x|y"}, ` "x|y"`},
	{"parentheses", []string{"(see #12)", "fix foo() & bar | baz"}, ` "(see #12)" "fix foo() & bar | baz"`},
	{"redirection", []string{"a<b>c"}, ` "a<b>c"`},
}

func TestShimCommandLine(t *testing.T) {
	const shim = `C:\Users\u\AppData\Roaming\npm\qwen.cmd`
	for _, tc := range shimCases {
		got, err := shimCommandLine(shim, tc.args)
		want := `cmd.exe /d /s /c ""` + shim + `"` + tc.line + `"`
		if err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", tc.name, got, err, want)
		}
	}
	// A space in the path of the batch file together with a space in an
	// argument: four quotes on the line, which is the case that needs the
	// outer pair that /s strips.
	got, err := shimCommandLine(`C:\Users\Ayşe Yılmaz\npm\qwen.cmd`, []string{"say hi"})
	if want := `cmd.exe /d /s /c ""C:\Users\Ayşe Yılmaz\npm\qwen.cmd" "say hi""`; err != nil || got != want {
		t.Errorf("a space in both: %s (%v)", got, err)
	}
}

func TestShimCommandLineRefusals(t *testing.T) {
	const shim = `C:\npm\qwen.cmd`
	// Refused in an argument: a quotation mark, %, ^, ! and every control
	// character below U+0020 (a tab excepted).
	for _, c := range []rune{'"', '%', '^', '!', '\r', '\n', '\x00', '\x01', '\x1f'} {
		// In an argument, wherever it stands.
		for _, arg := range []string{string(c), "a" + string(c), string(c) + "b", "a " + string(c) + " b"} {
			args := []string{"-p", arg, "later \" never reported"}
			line, err := shimCommandLine(shim, args)
			var bad *shimArgError
			if !errors.As(err, &bad) || bad.Index != 2 || bad.Char != c || line != "" {
				t.Errorf("argument %q: line %q, err %v", arg, line, err)
			}
			if got := checkShimArgs(shim, args); got == nil || got.Index != 2 || got.Char != c {
				t.Errorf("checkShimArgs with %q = %+v", arg, got)
			}
		}
		// In the path of the batch file itself.
		path := `C:\np` + string(c) + `m\qwen.cmd`
		line, err := shimCommandLine(path, []string{"ok"})
		var bad *shimArgError
		if !errors.As(err, &bad) || bad.Index != 0 || bad.Char != c || line != "" {
			t.Errorf("path %q: line %q, err %v", path, line, err)
		}
	}
	// & is read literally inside an argument's quotes but is refused in the
	// path of the batch file, where cmd.exe sees the line before the quotes
	// are stripped.
	if bad := checkShimArgs(`C:\R&D\qwen.cmd`, []string{"ok"}); bad == nil || bad.Index != 0 || bad.Char != '&' {
		t.Errorf("an ampersand in the path = %+v", bad)
	}
	// The well-known attacks, each refused at the character that makes it one.
	for arg, c := range map[string]rune{
		`it"s broken`:             '"',
		"what is %EVREN_API_KEY%": '%',
		"a^b":                     '^',
		"50%":                     '%',
		"!EVREN_API_KEY!":         '!',
		"two\nlines":              '\n',
		"a \x01 control":          '\x01',
		"a\tb is fine, a \x1b is": '\x1b',
	} {
		_, err := shimCommandLine(shim, []string{arg})
		var bad *shimArgError
		if !errors.As(err, &bad) || bad.Index != 1 || bad.Char != c {
			t.Errorf("%q: err %v", arg, err)
		}
	}
	// The error text names the place and never repeats the argument: a
	// prompt can hold anything, a key included.
	_, err := shimCommandLine(shim, []string{"evren_llm_SECRET % more"})
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "argument 1") {
		t.Errorf("err = %v", err)
	}

	// A command line longer than Windows allows is refused on its own, and
	// the message is the caller's to write.
	long := strings.Repeat("a", maxShimLine)
	line, err := shimCommandLine(shim, []string{long})
	var tooLong *shimLineError
	if !errors.As(err, &tooLong) || line != "" || tooLong.Length <= maxShimLine {
		t.Errorf("a long line: line %d chars, err %v", len(line), err)
	}
	// One that is just short of the limit is passed.
	fit := strings.Repeat("a", maxShimLine-len(`cmd.exe /d /s /c ""`+shim+`" ""`)-1)
	if line, err := shimCommandLine(shim, []string{fit}); err != nil || line == "" {
		t.Errorf("a line that fits: %q, err %v", line, err)
	}
}

func TestCheckShimArgsAcceptsEveryCase(t *testing.T) {
	for _, tc := range shimCases {
		if bad := checkShimArgs(`C:\Users\Ayşe Yılmaz\npm\qwen.cmd`, tc.args); bad != nil {
			t.Errorf("%s: refused: %+v", tc.name, bad)
		}
	}
	// A path with the parentheses Windows itself puts in "Program Files
	// (x86)" is accepted.
	if bad := checkShimArgs(`C:\Program Files (x86)\nodejs\qwen.cmd`, []string{"-p", "hi"}); bad != nil {
		t.Errorf("a path with parentheses: %+v", bad)
	}
}

// withNoDefaultCurrentDir adds the variable that keeps cmd.exe from running a
// same-named file in the working directory, unless the caller already set it
// (in any case).
func TestWithNoDefaultCurrentDir(t *testing.T) {
	added := withNoDefaultCurrentDir([]string{"PATH=C:\\Windows"})
	if len(added) != 2 || added[1] != noDefaultCurrentDir+"=1" {
		t.Errorf("added = %q", added)
	}
	// A caller's own setting is left as it is, in any case. On Windows a
	// variable's name does not tell the cases apart.
	for _, kv := range []string{
		noDefaultCurrentDir + "=",
		noDefaultCurrentDir + "=0",
		strings.ToUpper(noDefaultCurrentDir) + "=1",
		strings.ToLower(noDefaultCurrentDir) + "=yes",
	} {
		env := []string{"PATH=C:\\Windows", kv}
		got := withNoDefaultCurrentDir(env)
		if len(got) != 2 || got[1] != kv {
			t.Errorf("with %q: %q", kv, got)
		}
	}
	// A name that only begins with the same letters is not the variable.
	env := []string{noDefaultCurrentDir + "Other=1"}
	if got := withNoDefaultCurrentDir(env); len(got) != 2 {
		t.Errorf("a longer name: %q", got)
	}
}

// npmShimText is what npm's cmd-shim writes for a tool that runs on node; run
// is its last line.
func npmShimText(run string) string {
	return "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\n" +
		"IF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n)\r\n\r\n" +
		run + "\r\n"
}

func TestNpmShimScript(t *testing.T) {
	const modern = `endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & `
	for _, tc := range []struct{ name, text, want string }{
		{"npm's shim", npmShimText(modern + `"%_prog%"  "%dp0%\node_modules\@qwen-code\qwen-code\cli.js" %*`), `node_modules\@qwen-code\qwen-code\cli.js`},
		{"the older form", npmShimText(`"%_prog%"  "%dp0%\node_modules\opencode-ai\bin\opencode" %*`), `node_modules\opencode-ai\bin\opencode`},
		{"Unix line breaks", strings.ReplaceAll(npmShimText(`"%_prog%" "%dp0%\node_modules\x\x.js" %*`), "\r\n", "\n"), `node_modules\x\x.js`},
		// Anything else is not taken apart: it is started through cmd.exe.
		{"an option for node", npmShimText(modern + `"%_prog%" --no-warnings "%dp0%\node_modules\x\x.js" %*`), ""},
		{"no script", npmShimText(modern + `"%_prog%" %*`), ""},
		{"a script outside node_modules", npmShimText(`"%_prog%"  "%dp0%\..\elsewhere\x.js" %*`), ""},
		{"a script that climbs out", npmShimText(`"%_prog%"  "%dp0%\node_modules\..\..\x.js" %*`), ""},
		{"a variable in the script", npmShimText(`"%_prog%"  "%dp0%\node_modules\%X%\x.js" %*`), ""},
		{"a second command", npmShimText(`"%_prog%"  "%dp0%\node_modules\x\x.js" %* & calc.exe`), ""},
		{"more arguments", npmShimText(`"%_prog%"  "%dp0%\node_modules\x\x.js" --flag %*`), ""},
		{"another program", "@ECHO off\r\n\"%~dp0\\tool.exe\" \"%dp0%\\node_modules\\x\\x.js\" %*\r\n", ""},
		{"the run line is not the last", npmShimText(`"%_prog%"  "%dp0%\node_modules\x\x.js" %*`) + "calc.exe\r\n", ""},
		{"a command in front of the run line", strings.Replace(npmShimText(`"%_prog%"  "%dp0%\\node_modules\\x\\x.js" %*`), "SETLOCAL\r\n", "SETLOCAL\r\nSET NODE_OPTIONS=--require evil\r\n", 1), ""},
		{"empty", "", ""},
	} {
		got, ok := npmShimScript([]byte(tc.text))
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}
