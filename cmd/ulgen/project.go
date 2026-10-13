package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/tailscale/hujson"

	"github.com/mftnakrsu/ulgen/internal/setup"
)

// This file looks at the project a tool is about to be started in.
//
// Qwen Code and opencode read configuration files from the project as well
// as from the user's own directory, and a project's file wins: it can list
// a model that reads EVREN_API_KEY and sends it to any address, or give the
// EVREN provider another address. "ulgen run" puts the key into the tool's
// environment, so before it starts the tool it looks for such a file and
// does not start the tool when it finds one, unless the user says the
// project is trusted. Codex needs no such check: its project files cannot
// define providers.
//
// The check reads text. It does not follow what a file may load in turn
// (plugins, hooks, programs the tool is told to run), so it is a warning
// about the plain case, not a sandbox.

// projectConfig lists, per tool id, the configuration files the tool reads
// from a project, relative to a directory.
var projectConfig = map[string][]string{
	"qwen": {filepath.Join(".qwen", "settings.json")},
	"opencode": {
		"opencode.json", "opencode.jsonc",
		filepath.Join(".opencode", "opencode.json"), filepath.Join(".opencode", "opencode.jsonc"),
	},
}

// resolveLinks is filepath.EvalSymlinks; tests replace it to play a file
// system that cannot resolve links.
var resolveLinks = filepath.EvalSymlinks

// maxProjectFile is the largest project file that is read; a configuration
// file is far smaller, and anything larger is treated as unreadable.
const maxProjectFile = 4 << 20

// projectFinding is a project file that keeps a tool from being started.
type projectFinding struct {
	Path string
	// Unreadable is true when the file could not be read, so that nothing
	// is known about it; otherwise it names the EVREN key or provider.
	Unreadable bool
}

// userFiler is implemented by the adapters whose tool reads project files:
// it names the user's own files of the same kind, the ones "ulgen setup"
// writes, which name the EVREN key by design.
type userFiler interface {
	UserFiles(env setup.Env) ([]string, error)
}

// findProjectConfig looks for a project file of the tool that could send the
// key elsewhere. It starts in cwd, as the tool will see it (symbolic links
// resolved), and goes up through the parent directories to the home
// directory or, outside of it, to the root of the file system. The tool's
// user-level files are not project files, wherever they lie. It returns the
// first finding, or nil.
func findProjectConfig(tool setup.Tool, env setup.Env, cwd string) (*projectFinding, error) {
	names := projectConfig[tool.ID()]
	if len(names) == 0 {
		return nil, nil
	}
	var own []string
	if u, ok := tool.(userFiler); ok {
		files, err := u.UserFiles(env)
		if err != nil {
			return nil, err
		}
		own = files
	}
	dir, err := startDir(cwd)
	if err != nil {
		return nil, err
	}
	for {
		for _, name := range names {
			path := filepath.Join(dir, name)
			if isOneOf(path, own) {
				continue
			}
			if f := inspectProjectFile(tool.ID(), path); f != nil {
				f.Path = fromHome(env.Home, f.Path)
				return f, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir || samePath(dir, env.Home) {
			return nil, nil
		}
		dir = parent
	}
}

// startDir returns the directory the search starts in: cwd as the tool
// will see it, with symbolic links resolved. Some file systems cannot
// resolve links (a RAM disk on Windows, for one); a directory that is there
// is then searched as it was named, because refusing every start on such a
// drive would only teach its user to pass --trust-project always.
func startDir(cwd string) (string, error) {
	dir, err := resolveLinks(cwd)
	if err != nil {
		if _, statErr := os.Stat(cwd); statErr != nil {
			return "", err
		}
		dir = filepath.Clean(cwd)
	}
	if !filepath.IsAbs(dir) {
		return "", &fs.PathError{Op: "resolve", Path: dir, Err: errors.New("not an absolute path")}
	}
	return dir, nil
}

// fromHome rewrites a path that lies under the home directory's resolved
// location so that it starts at the home directory as the user writes it
// (the search works on resolved paths, and a message should show "~").
func fromHome(home, path string) string {
	resolved, err := resolveLinks(home)
	if err != nil || resolved == home {
		return path
	}
	rel, err := filepath.Rel(resolved, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(home, rel)
}

// inspectProjectFile reads one candidate. It returns nil when there is no
// such file or the file says nothing about EVREN. A file that is there but
// cannot be read (no permission, not a regular file, too large) is a
// finding too: Ülgen cannot say that it is harmless.
func inspectProjectFile(toolID, path string) *projectFinding {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return nil
	case err != nil:
		return &projectFinding{Path: path, Unreadable: true}
	case info.IsDir():
		return nil // a directory of that name is not a configuration file
	case !info.Mode().IsRegular() || info.Size() > maxProjectFile:
		return &projectFinding{Path: path, Unreadable: true}
	}
	file, err := os.Open(path)
	if err != nil {
		return &projectFinding{Path: path, Unreadable: true}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProjectFile+1))
	if err != nil || len(data) > maxProjectFile {
		return &projectFinding{Path: path, Unreadable: true}
	}
	if namesEVREN(toolID, data) {
		return &projectFinding{Path: path}
	}
	return nil
}

// namesEVREN reports whether the text of a project file could concern the
// EVREN key: it mentions EVREN_API_KEY (in any case: Windows does not tell
// the cases of a variable's name apart), as plain text or as a JSON string
// written with escapes, or, for opencode, it defines the provider "evren",
// whose address a project file may replace.
//
// A file that cannot be understood counts as naming the key: one that does
// not decode as JSON with comments, one that holds a NUL byte, and one that
// is not valid UTF-8. Ülgen reads the file to decide that it is harmless, so
// a file it cannot read that way may say anything — a following version of
// the tool may read it where Ülgen does not. A leading UTF-8 byte-order mark
// is removed first, and numbers of any size are accepted (the decoder is
// given UseNumber), so neither makes a file undecodable.
func namesEVREN(toolID string, src []byte) bool {
	src = bytes.TrimPrefix(src, utf8BOM)
	if len(bytes.TrimSpace(src)) == 0 {
		return false // nothing but white space: it names nothing
	}
	if !utf8.Valid(src) || bytes.IndexByte(src, 0) >= 0 {
		return true
	}
	const keyVar = "evren_api_key" // setup.KeyEnv in lower case
	if bytes.Contains(bytes.ToLower(src), []byte(keyVar)) {
		return true
	}
	var value any
	standard, err := hujson.Standardize(bytes.Clone(src))
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(standard))
		dec.UseNumber()
		err = dec.Decode(&value)
	}
	if err != nil {
		return true
	}
	if anyString(value, func(s string) bool { return strings.Contains(strings.ToLower(s), keyVar) }) {
		return true
	}
	if toolID != "opencode" {
		return false
	}
	root, _ := value.(map[string]any)
	providers, _ := root["provider"].(map[string]any)
	for id := range providers {
		if strings.EqualFold(id, "evren") {
			return true
		}
	}
	return false
}

// utf8BOM is the UTF-8 byte-order mark, which an editor may put at the start
// of a file. namesEVREN removes it before it reads the file as JSON.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// anyString reports whether match holds for some key or string anywhere in
// a decoded JSON value.
func anyString(value any, match func(string) bool) bool {
	switch v := value.(type) {
	case string:
		return match(v)
	case []any:
		for _, e := range v {
			if anyString(e, match) {
				return true
			}
		}
	case map[string]any:
		for k, e := range v {
			if match(k) || anyString(e, match) {
				return true
			}
		}
	}
	return false
}

// samePath reports whether two paths name the same file or directory: they
// are written the same, or both exist and are the same file (through a
// symbolic link, or in another case on a file system that ignores it).
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// isOneOf reports whether path is one of the files in list (see samePath).
func isOneOf(path string, list []string) bool {
	for _, other := range list {
		if samePath(path, other) {
			return true
		}
	}
	return false
}
