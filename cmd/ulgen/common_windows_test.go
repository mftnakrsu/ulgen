//go:build windows

package main

import "testing"

// Windows compares paths without regard to case and has homes on network
// shares; "~" must work for both.
func TestShowPathOnWindows(t *testing.T) {
	for _, tc := range []struct{ home, path, want string }{
		{`C:\Users\u`, `C:\Users\u\.qwen\settings.json`, `~\.qwen\settings.json`},
		{`C:\Users\u`, `c:\users\U\x`, `~\x`},
		{`c:\users\u\`, `C:\Users\u\x`, `~\x`},
		{`C:\Users\u`, `C:\Users\u2\x`, `C:\Users\u2\x`},
		{`C:\Users\u`, `D:\Users\u\x`, `D:\Users\u\x`},
		{`C:\Users\u`, `C:\Users\u\..cache\x`, `~\..cache\x`},
		// A home directory on a network share.
		{`\\server\homes\u`, `\\server\homes\u\.codex\config.toml`, `~\.codex\config.toml`},
		{`\\server\homes\u`, `\\SERVER\Homes\U\x`, `~\x`},
		{`\\server\homes\u`, `\\server\other\u\x`, `\\server\other\u\x`},
		{`\\server\homes\u`, `C:\x`, `C:\x`},
		// A space: the whole path, quoted.
		{`C:\Users\Ayşe Yılmaz`, `C:\Users\Ayşe Yılmaz\.qwen\settings.json`, `~\.qwen\settings.json`},
		{`C:\Users\u`, `C:\Users\u\AppData\Roaming\ulgen\my backups\x`, `"C:\Users\u\AppData\Roaming\ulgen\my backups\x"`},
		{`C:\Users\u`, `C:\Program Files\nodejs\qwen.cmd`, `"C:\Program Files\nodejs\qwen.cmd"`},
	} {
		if got := showPath(tc.home, tc.path); got != tc.want {
			t.Errorf("showPath(%q, %q) = %q, want %q", tc.home, tc.path, got, tc.want)
		}
	}
}
