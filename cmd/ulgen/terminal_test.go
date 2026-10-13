package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPtyPipeName(t *testing.T) {
	for name, want := range map[string]bool{
		// Git Bash and MSYS2, both directions.
		`\msys-dd50a72ab4668b33-pty0-to-master`:   true,
		`\msys-dd50a72ab4668b33-pty0-from-master`: true,
		`\msys-1888ae32e00d56aa-pty12-to-master`:  true,
		// Cygwin.
		`\cygwin-e022582115c10879-pty1-from-master`:   true,
		`\cygwin-e022582115c10879-pty1-to-master`:     true,
		`\cygwin-e022582115c10879-pty3-to-master-nat`: true,
		// The same name as another Windows function spells it.
		`\Device\NamedPipe\msys-dd50a72ab4668b33-pty0-to-master`: true,

		// An ordinary pipe between two programs in the same window: what
		// "ulgen token | cat" writes to. Not a terminal.
		`\msys-dd50a72ab4668b33-1820-pipe-0x3`:      false,
		`\cygwin-e022582115c10879-1820-pipe-nt-0x5`: false,
		// Pipes of other programs.
		`\mojo.1234.5678.9012`:                  false,
		`\pipe-to-master`:                       false,
		`\LOCAL\crashpad_1234_ABCDEF`:           false,
		`\Winsock2\CatalogChangeListener-1c8-0`: false,
		// Names that only look alike.
		`\msys-dd50a72ab4668b33-pty-to-master`:         false, // no number
		`\msys-dd50a72ab4668b33-ptyx-to-master`:        false,
		`\msys--pty0-to-master`:                        false, // no installation
		`\msys-dd50a72ab4668b33-pty0-to-slave`:         false,
		`\msys-dd50a72ab4668b33-pty0-at-master`:        false,
		`\mingw-dd50a72ab4668b33-pty0-to-master`:       false,
		`msys-dd50a72ab4668b33-pty0-to-master`:         false, // no leading backslash
		`\x\msys-dd50a72ab4668b33-pty0-to-master`:      false,
		`\msys-dd50a72ab4668b33-pty0-to-master\x`:      false,
		`\msys-dd50a72ab4668b33-pty0-to-master-a-b`:    false,
		`evil\msys-dd50a72ab4668b33-pty0-to-master`:    false,
		"\\msys-dd50a72ab4668b33-pty0-to-master\nmore": false,
		``: false,
	} {
		if got := isPtyPipeName(name); got != want {
			t.Errorf("isPtyPipeName(%q) = %v, want %v", name, got, want)
		}
	}
}

// What is certainly not a terminal on any system: a file and a pipe.
func TestTerminalOfFilesAndPipes(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for name, f := range map[string]*os.File{"a file": file, "the read end of a pipe": r, "the write end of a pipe": w} {
		if got := terminalOf(f); got != notTerminal {
			t.Errorf("%s: terminalOf = %v", name, got)
		}
	}
}

// In the window of Git Bash the key cannot be hidden while it is typed. The
// prompt says so instead of promising, and the key is read as a line.
func TestLoginSaysWhenTheKeyWillBeVisible(t *testing.T) {
	store := useFileStore(t)
	useFakeBrowser(t, nil) // the hidden read fails the test when it is used
	stdinTerminal = func() terminalKind { return ptyPipeTerminal }
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := execIn(t, context.Background(), nil, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "Paste the API key (this terminal cannot hide it; it will be visible). To hide it, start again as \"winpty ulgen login\": \n") || strings.Contains(stdout, "stays hidden") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if got, err := store.Get("evren"); err != nil || got != testKey {
		t.Errorf("stored %q, %v", got, err)
	}
	_, stdout, _ = execIn(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, testKey+"\n", "login", "--no-browser", "--base-url", srv.URL)
	if !strings.Contains(stdout, "API anahtarını yapıştırın (bu terminal gizleyemiyor; anahtar ekranda görünecek). Gizlemek için \"winpty ulgen login\" ile yeniden başlatın: \n") {
		t.Errorf("Turkish: %q", stdout)
	}
}
