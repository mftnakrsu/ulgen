package setup

import (
	"path/filepath"
	"strings"
)

// HomeError says the home directory is not known, or is not an absolute
// path. Home is what was found; it is empty when nothing was. Every path an
// adapter uses starts at the home directory, so without one nothing is read,
// written or started.
type HomeError struct{ Home string }

// Error says that the home directory is unknown or relative.
func (e *HomeError) Error() string {
	if e.Home == "" {
		return "the home directory is not known"
	}
	return "the home directory " + e.Home + " is not an absolute path"
}

// RelativePathError says an environment variable that moves a tool's
// directory holds a relative path. Such a path would mean a different
// directory in every place the command runs from, so it is refused.
type RelativePathError struct{ Var, Value string }

// Error names the variable and its value.
func (e *RelativePathError) Error() string {
	return e.Var + " is set to " + e.Value + ", which is not an absolute path"
}

// Path is a file path given to a note as an argument. The command shows it
// the way it shows every other path (with "~" under the home directory).
type Path string

// dirVars are the environment variables that move a tool's directory and
// must hold an absolute path when they are set. XDG_CONFIG_HOME is not among
// them: the XDG rule is to treat a relative value as not set (opencodeDir).
var dirVars = []string{"QWEN_HOME", "OPENCODE_CONFIG_DIR", "CODEX_HOME", "CLAUDE_CONFIG_DIR"}

// Validate checks that nothing in the environment would make an adapter use
// a path relative to the current directory: the home directory is known and
// absolute (*HomeError otherwise), and QWEN_HOME, OPENCODE_CONFIG_DIR,
// CODEX_HOME and CLAUDE_CONFIG_DIR, when set, are absolute
// (*RelativePathError otherwise; QWEN_HOME may also start with "~"). The
// commands call it before they read or start anything.
func (e Env) Validate() error {
	if _, err := e.home(); err != nil {
		return err
	}
	for _, name := range dirVars {
		v := e.getenv(name)
		if name == "QWEN_HOME" && isTilde(v) {
			continue
		}
		if v != "" && !filepath.IsAbs(v) {
			return &RelativePathError{Var: name, Value: v}
		}
	}
	return nil
}

// home returns the home directory, or a *HomeError when it is unknown or
// not absolute.
func (e Env) home() (string, error) {
	if e.Home == "" || !filepath.IsAbs(e.Home) {
		return "", &HomeError{Home: e.Home}
	}
	return e.Home, nil
}

// getenv reads an environment variable without the spaces around its value;
// an Env without a Getenv has no variables.
func (e Env) getenv(name string) string {
	if e.Getenv == nil {
		return ""
	}
	return strings.TrimSpace(e.Getenv(name))
}

// isTilde reports a value that starts at the home directory: "~", "~/x" or
// `~\x`.
func isTilde(v string) bool {
	return v == "~" || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`)
}

// ShellHint is the command line that puts the key into the user's shell, in
// the form the usual shell of the system takes: PowerShell on Windows, a
// POSIX shell everywhere else.
func ShellHint(goos string) string {
	if goos == "windows" {
		return "$env:" + KeyEnv + " = (ulgen token)"
	}
	return "export " + KeyEnv + `="$(ulgen token)"`
}
