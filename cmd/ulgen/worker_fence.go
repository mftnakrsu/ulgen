package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mftnakrsu/ulgen/internal/setup"
)

// This file holds the parts of "ulgen worker" that fence the child in besides
// the allow-list: Claude Code's sandbox settings and the --out file.

// workerGoEnvTimeout bounds one "go env" question asked for the go preset.
const workerGoEnvTimeout = 5 * time.Second

// workerGoEnv answers "go env <name>" when go is found on PATH. It is the seam
// the tests replace, so a test never starts a real go. An error means "no
// answer"; the caller skips it.
var workerGoEnv = func(name string) (string, error) {
	goBin, err := lookPathAbs("go")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), workerGoEnvTimeout)
	defer cancel()
	out, err := osexec.CommandContext(ctx, goBin, "env", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// workerHiddenFromHome lists, relative to the user's home directory, where
// credentials and the settings of the coding tools live: the paths a worker's
// sandboxed shell commands must not read, because the sandbox confines writing
// but by default not reading, and a model-chosen command could otherwise print
// a key into its output. It is a list of places known to hold secrets and
// cannot be complete; it is the second fence, after the allow-list.
var workerHiddenFromHome = []string{
	".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker",
	".netrc", ".npmrc", ".pypirc", ".git-credentials",
	filepath.Join(".config", "gh"), filepath.Join(".config", "gcloud"),
	".claude", ".codex", ".qwen", filepath.Join(".config", "opencode"),
	filepath.Join("Library", "Keychains"),
}

// workerSettingsDoc is the JSON given to Claude Code with --settings. The key
// names are Claude Code's.
type workerSettingsDoc struct {
	Permissions workerPermissionsDoc `json:"permissions"`
	Sandbox     *workerSandboxDoc    `json:"sandbox,omitempty"`
}

// workerPermissionsDoc is the "permissions" object: the rules that refuse the
// file-reading tools the places the sandbox hides from shell commands. They
// hold whatever --allow says, because a deny rule wins over an allow rule, so
// a wide "Read" rule still cannot reach a key.
type workerPermissionsDoc struct {
	Deny []string `json:"deny"`
}

// workerSandboxDoc is the "sandbox" object of the settings.
// AutoAllowBashIfSandboxed is always written, as false: left out, Claude Code
// runs every shell command that the sandbox can hold without asking, in any
// permission mode, so a command the allow-list does not name would run.
type workerSandboxDoc struct {
	Enabled                  bool                `json:"enabled"`
	AutoAllowBashIfSandboxed bool                `json:"autoAllowBashIfSandboxed"`
	AllowUnsandboxedCommands bool                `json:"allowUnsandboxedCommands"`
	FailIfUnavailable        bool                `json:"failIfUnavailable"`
	Filesystem               workerFilesystemDoc `json:"filesystem"`
}

// workerFilesystemDoc is the "sandbox.filesystem" object: what sandboxed shell
// commands may not read, and what they may write besides the working
// directory.
type workerFilesystemDoc struct {
	DenyRead   []string `json:"denyRead"`
	AllowWrite []string `json:"allowWrite,omitempty"`
}

// workerMakeDir creates a directory with the ones above it. It is the seam the
// tests replace, so a test never creates a directory outside its own.
var workerMakeDir = os.MkdirAll

// workerWritablePaths returns the directories outside the worker's own that the
// named presets let sandboxed shell commands write: for go, the build and
// module caches as "go env" names them; for node, the npm cache; for python
// and read, none. An answer of "go env" that is empty, relative or an error is
// skipped.
func workerWritablePaths(home string, presets []string) []string {
	var paths []string
	for _, p := range presets {
		switch p {
		case "go":
			for _, name := range []string{"GOCACHE", "GOMODCACHE"} {
				if dir, err := workerGoEnv(name); err == nil && dir != "" && filepath.IsAbs(dir) {
					paths = append(paths, dir)
				}
			}
		case "node":
			if home != "" {
				paths = append(paths, filepath.Join(home, ".npm"))
			}
		}
	}
	return paths
}

// workerSettings builds the --settings value. Always: the rules that refuse
// the file-reading tools the paths under home and in cfgDir where keys live.
// With sandbox set, also Claude Code's sandbox: on, no shell command run for
// being sandboxed alone (the allow-list still decides), no escape to
// unsandboxed commands, a refusal to start without it, the same paths hidden
// from shell commands, and writable as the extra directories commands may
// write (see workerWritablePaths).
func workerSettings(home, cfgDir string, writable []string, sandbox bool) (string, error) {
	var hidden []string
	if home != "" {
		for _, rel := range workerHiddenFromHome {
			hidden = append(hidden, filepath.Join(home, rel))
		}
	}
	if cfgDir != "" {
		hidden = append(hidden, cfgDir)
	}

	doc := workerSettingsDoc{Permissions: workerPermissionsDoc{Deny: []string{}}}
	for _, path := range hidden {
		// One rule for the path itself (some are files) and one for all below.
		rule := workerReadRulePath(home, path)
		doc.Permissions.Deny = append(doc.Permissions.Deny, "Read("+rule+")", "Read("+rule+"/**)")
	}

	if sandbox {
		doc.Sandbox = &workerSandboxDoc{
			Enabled:           true,
			FailIfUnavailable: true,
			Filesystem:        workerFilesystemDoc{DenyRead: hidden, AllowWrite: writable},
		}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// workerReadRulePath writes an absolute path the way Claude Code's permission
// rules name one: "~/…" for a path under the home directory, "//…" for any
// other absolute path, with forward slashes; a Windows drive becomes a first
// directory ("//c/…").
func workerReadRulePath(home, path string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	slash := filepath.ToSlash(path)
	if vol := filepath.VolumeName(path); len(vol) == 2 && vol[1] == ':' {
		// Claude Code names a Windows drive as a first directory: C:\x is /c/x.
		slash = "/" + strings.ToLower(vol[:1]) + slash[2:]
	}
	return "//" + strings.TrimPrefix(slash, "/")
}

// workerPresetsAsked returns the presets named among the --allow values, in
// the order given and without repeats.
func workerPresetsAsked(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		if _, ok := workerPresets[v]; ok && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// sandboxSettings makes the --settings value for this run: Ülgen's own
// configuration directory, found the way the other commands find it, goes into
// the list of paths that may not be read. The sandbox part is left out with
// --no-sandbox and on Windows, where Claude Code has none.
func (l *workerLaunch) sandboxSettings(env setup.Env) (string, *failure) {
	cfg, err := configDir(l.getenv)
	if err != nil {
		return "", &failure{msg: describeSetupError(err, env.Home, l.tr), code: exitError}
	}
	sandbox := !l.opts.noSandbox && env.GOOS != "windows"
	var writable []string
	if sandbox {
		writable = workerWritablePaths(env.Home, workerPresetsAsked(l.opts.allow))
		for _, dir := range writable {
			// The sandbox lets a command write under these directories but not
			// make the directories above them, so a cache that was never used
			// is made here; the tool would have made it anyway. A failure is
			// left to the tool to report.
			_ = workerMakeDir(dir, 0o755)
		}
	}
	settings, err := workerSettings(env.Home, cfg, writable, sandbox)
	if err != nil {
		return "", &failure{msg: oneLine(err.Error()), code: exitError}
	}
	return settings, nil
}

// The two refusals that keep --out from being written through a link. They
// are the reason part of a message, like the system's own error texts.
var (
	errWorkerOutNotAFile = errors.New("it is not a regular file")
	errWorkerOutReplaced = errors.New("the file was replaced while the worker ran")
)

// openWorkerOut prepares the --out file before the child is started and
// returns it open: the directory must exist and the path must name a regular
// file or nothing. The file is created, or emptied, with mode 0600. A symbolic
// link is refused, and so is anything else that is not a regular file. Ülgen
// writes --out itself, outside the sandbox, and the worker can write where
// --out often lies (under --dir, in the temporary directory): a link there
// would send the answer to a file of the link's choosing. The file is emptied
// only once the opened file is known to be the one the path names.
func openWorkerOut(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errWorkerOutNotAFile
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if !namesFile(path, f) {
		f.Close()
		return nil, errWorkerOutNotAFile
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// namesFile reports whether path names the regular file that f has open. A
// symbolic link at the path is not followed, so a link to that very file does
// not count either.
func namesFile(path string, f *os.File) bool {
	at, err := os.Lstat(path)
	if err != nil || !at.Mode().IsRegular() {
		return false
	}
	open, err := f.Stat()
	return err == nil && os.SameFile(at, open)
}

// writeWorkerOut writes the child's standard output, unchanged, to the file
// that was opened before the child started, and closes it. It writes to the
// open file and not to the path again: a link the worker put at the path
// while it ran is then not followed. A path that no longer names that file is
// an error, because the answer is not where the user will look for it.
func writeWorkerOut(path string, f *os.File, out []byte) error {
	err := f.Truncate(0)
	if err == nil {
		_, err = f.WriteAt(out, 0)
	}
	if err == nil && !namesFile(path, f) {
		err = errWorkerOutReplaced
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
