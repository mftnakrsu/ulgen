// Package setup writes the EVREN provider into the configuration of coding
// tools. Each tool is an adapter that reads the tool's files and returns a
// plan; this file holds what the adapters share and the code that applies a
// plan to the disk.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/setup/jsonc"
)

// KeyEnv is the environment variable every tool reads the key from.
const KeyEnv = "EVREN_API_KEY"

// Env is what an adapter may learn about the machine. Tests build their own.
//
// Nothing an adapter reads, writes or starts may depend on the directory the
// command happens to run in: Home must be an absolute path (Validate checks
// it), a LookPath result that is not absolute is treated as "not found", and
// an environment variable that names a directory counts only when it holds
// an absolute path.
type Env struct {
	// GOOS is the operating system, as runtime.GOOS spells it. It decides
	// the form of the shell hint (ShellHint).
	GOOS string
	// Home is the user's home directory, an absolute path.
	Home string
	// Getenv reads an environment variable; "" when it is not set.
	Getenv func(string) string
	// LookPath finds a program by name on PATH, or checks a path that has a
	// separator in it, as os/exec.LookPath does.
	LookPath func(string) (string, error)
	// Version runs "<binary> --version" and returns what it printed.
	Version func(ctx context.Context, binary string) (string, error)
}

// Request is what the user asked for; it is the same for every tool.
type Request struct {
	BaseURL     string  // EVREN API root
	Models      []Model // tool-capable chat models, in the API's order
	Default     string  // id of the model to select
	ModelGiven  bool    // --model was on the command line
	MakeDefault bool    // --default
	ClaudeSkill []byte  // the EVREN workers skill (SKILL.md text); nil when not compiled in
}

// Model is one EVREN model as the adapters need it.
type Model struct {
	ID        string
	Context   int
	MaxOutput int // 0 when the API gives none
	Vision    bool
	// Effort says that the model takes a reasoning_effort, and EffortValues
	// are the values EVREN lists for it (nil: none listed). Only the
	// translator reads them.
	Effort       bool
	EffortValues []string
}

// model returns the request's model with the given id.
func (r Request) model(id string) (Model, bool) {
	for _, m := range r.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// RequestError says a request cannot be turned into a file. The command
// line refuses every such request before an adapter sees it, so this is a
// mistake of the caller, not of the user. Problem says what is wrong.
type RequestError struct{ Problem string }

// Error says that the request is not usable, and why.
func (e *RequestError) Error() string { return "the setup request is not usable: " + e.Problem }

// check refuses a request no adapter can work with (*RequestError): an API
// root that is empty, is not valid UTF-8 or holds a control character (it is
// written into other programs' files); no models; a model without an id, or
// the same id twice (a file with the same key twice means different things
// to different readers); a default that is not one of the models. Every
// adapter's Plan calls it before it reads a file.
func (r Request) check() error {
	switch {
	case r.BaseURL == "":
		return &RequestError{Problem: "the API root is empty"}
	case !utf8.ValidString(r.BaseURL):
		return &RequestError{Problem: "the API root is not valid UTF-8"}
	case strings.IndexFunc(r.BaseURL, unicode.IsControl) >= 0:
		return &RequestError{Problem: "the API root holds a control character"}
	case len(r.Models) == 0:
		return &RequestError{Problem: "it has no models"}
	}
	seen := map[string]bool{}
	for _, m := range r.Models {
		switch {
		case m.ID == "":
			return &RequestError{Problem: "a model has no id"}
		case seen[m.ID]:
			return &RequestError{Problem: "the model " + strconv.Quote(m.ID) + " is listed twice"}
		}
		seen[m.ID] = true
	}
	if !seen[r.Default] {
		return &RequestError{Problem: "the default model " + strconv.Quote(r.Default) + " is not one of its models"}
	}
	return nil
}

// defaultState is what a tool's default model is to Ülgen.
type defaultState int

const (
	defaultNone    defaultState = iota // the tool has no default of its own
	defaultForeign                     // it has one, and it is not EVREN's
	defaultEvren                       // it is a model EVREN offers
	defaultRetired                     // it is EVREN's, of a model EVREN no longer offers
)

// changesDefault is the rule for a tool's default model, the same for Qwen
// Code and opencode. Ülgen adds, it does not take over: the request's
// default is written
//
//   - when the tool has no default of its own (an empty string or null is
//     none);
//   - with --default;
//   - when the default is EVREN's and --model names the model to use;
//   - when the default is EVREN's and names a model EVREN no longer offers:
//     left alone, the tool would start on a model that is not there.
//
// In every other case the default stays as it is. Whether a default is
// EVREN's is the adapter's to say, by the one rule it has for "Ülgen's
// entry".
func (r Request) changesDefault(state defaultState) bool {
	switch state {
	case defaultNone, defaultRetired:
		return true
	case defaultEvren:
		return r.MakeDefault || r.ModelGiven
	}
	return r.MakeDefault
}

// defaultNotes adds what the user should know about the default model after
// changesDefault: that the tool keeps a default that is not EVREN's, or that
// Ülgen replaced one whose model is gone without being asked for a model.
// was is the model the default named; name and id are the tool's.
func (p *Plan) defaultNotes(r Request, state defaultState, was, name, id string) {
	switch {
	case state == defaultForeign && !r.MakeDefault:
		p.note("setup.note.default_kept", name, id)
	case state == defaultRetired && !r.ModelGiven:
		p.note("setup.note.default_replaced", was, r.Default)
	}
}

// Detection says whether a tool is on the machine.
type Detection struct {
	Installed bool
	Binary    string // absolute path
	Version   string // "" when unknown
	Notes     []Note // for example: Node is too old for this tool
}

// FileChange is one file to create, rewrite or remove. Old is nil for a new
// file. With Delete set the file is removed and New is not looked at.
type FileChange struct {
	Path     string
	Old, New []byte
	Delete   bool
}

// Note is a line for the user: a translation key and its arguments.
type Note struct {
	Key  string
	Args []any
}

// Plan is what setting up one tool would do.
type Plan struct {
	Files []FileChange // only files whose content changes
	Notes []Note
}

// Tool is one coding tool Ülgen can set up.
type Tool interface {
	ID() string   // "qwen"
	Name() string // "Qwen Code"
	// Detect looks for the tool on the machine.
	Detect(ctx context.Context, env Env) Detection
	// Configured reports whether EVREN is set up in the tool. It reads
	// files only.
	Configured(env Env) (bool, error)
	// Plan reads the tool's files and returns the changes; it writes nothing.
	Plan(env Env, req Request) (Plan, error)
	// Remove returns the changes that undo Plan: what is Ülgen's goes, by
	// the same rule Plan knows its own by, and what the user wrote stays. It
	// needs neither the key nor EVREN, and it writes nothing.
	Remove(env Env) (Plan, error)
	// Launch returns the arguments "ulgen run" passes to the tool.
	Launch(env Env, args []string) []string
	// InstallHint tells the user how to install the tool.
	InstallHint(env Env) Note
}

// Proxied is implemented by a tool that is not pointed at EVREN by a file
// but started beside Ülgen's translator: "ulgen run" then runs the
// translator and gives the tool its local address instead of the key. Such a
// tool writes nothing, so its Plan has no files.
type Proxied interface {
	// Proxied reports that "ulgen run" must start the tool through the
	// translator.
	Proxied() bool
}

// ParseError says a tool's file could not be read as JSON or TOML. Ülgen
// never rewrites such a file.
type ParseError struct {
	Path   string
	Format string // "JSON" or "TOML"
	Detail string
}

// Error names the file, the format and the parser's complaint.
func (e *ParseError) Error() string {
	return fmt.Sprintf("%s is not valid %s (%s)", e.Path, e.Format, e.Detail)
}

// ShapeError says a key in a tool's file holds another kind of value than
// the tool's own format allows there, so Ülgen cannot write next to it.
type ShapeError struct {
	Path   string
	Detail string // for example "model is not an object"
}

// Error names the file and what is wrong in it.
func (e *ShapeError) Error() string { return e.Path + ": " + e.Detail }

// ForeignEntryError says a file of a tool defines, under the name Ülgen
// uses, an entry that is not Ülgen's: "provider.evren" in a configuration
// file of opencode, "model_providers.evren" in Codex's config.toml. Ülgen
// does not overwrite such an entry, and it does not write its own next to
// it either: the tool would merge the two, and the provider would be neither
// the user's nor Ülgen's.
type ForeignEntryError struct{ Path, Entry string }

// Error names the file and the entry.
func (e *ForeignEntryError) Error() string {
	return e.Path + " defines " + e.Entry + ", an entry that is not ulgen's"
}

// DefaultClashError says the default model of Qwen Code is not EVREN's, has
// no entry of its own in the settings and carries the name of a model EVREN
// offers. Qwen Code finds a default by its name, so the entry Ülgen writes
// for that model would become the default without anyone having asked for
// it. Model is the name. It stands in the settings file Path or, when the
// settings name no model, in the environment variable Var; the other of the
// two is empty.
type DefaultClashError struct {
	Path  string // the settings file that names the default, or ""
	Var   string // the environment variable that names it, or ""
	Model string
}

// Error names the file or the variable, and the model.
func (e *DefaultClashError) Error() string {
	where := e.Path
	if e.Var != "" {
		where = e.Var
	}
	return where + " names " + strconv.Quote(e.Model) + " as the default model without an entry of its own, and EVREN's entry of that name would take its place"
}

// KindError says a path holds another kind of thing than a tool keeps
// there: a directory where one of its files should be, or something else
// where a directory should be.
type KindError struct {
	Path    string
	WantDir bool   // a directory is expected there; a file otherwise
	Var     string // the environment variable that names the path, or ""
}

// Error names the path and what is wrong with it.
func (e *KindError) Error() string {
	switch {
	case e.Var != "":
		return e.Var + " points to " + e.Path + ", which is not a directory"
	case e.WantDir:
		return e.Path + " is not a directory"
	}
	return e.Path + " is a directory, not a file"
}

// LegacyProfileError says a file of Codex's selects or defines a profile in
// the old way, so that Codex refuses "--profile evren" and the profile file
// Ülgen writes could not be used: a top-level profile key, whatever profile
// it names, or a profiles.evren table. Key is what was found, as it would be
// written in the file: `profile = "work"` or `[profiles.evren]`.
type LegacyProfileError struct{ Path, Key string }

// Error names the file and what was found in it.
func (e *LegacyProfileError) Error() string {
	return e.Path + " has " + e.Key + ", the old way to select or define a profile, which Codex does not accept next to a profile file"
}

// MissingDirError says an environment variable points to a directory that
// does not exist.
type MissingDirError struct{ Var, Path string }

// Error names the variable and the directory.
func (e *MissingDirError) Error() string {
	return e.Var + " points to " + e.Path + ", which does not exist"
}

// sameURL compares two API roots, ignoring a final slash.
func sameURL(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// readFile returns a file's content, or nil when it does not exist.
func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if b == nil {
		b = []byte{}
	}
	return b, nil
}

// readRegular returns the content of a file, or nil when there is none. A
// directory in its place is a *KindError. Any other failure to look at the
// file or to read it is returned as it is: a file that is there and cannot
// be read is not a missing file.
func readRegular(path string) ([]byte, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	case info.IsDir():
		return nil, &KindError{Path: path}
	}
	return readFile(path)
}

// wantDir checks that path can be a directory of a tool: it is one, or it
// is not there yet. Anything else in its place is a *KindError.
func wantDir(path string) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return &KindError{Path: path, WantDir: true}
	}
	return nil
}

// jsoncFile is a JSON file with comments of a tool, as an adapter found it.
type jsoncFile struct {
	path string
	old  []byte     // the content; nil when there is no file
	doc  *jsonc.Doc // the content parsed; an empty document when there is no file
	// blank says the file gives the tool nothing: it is not there, or it
	// holds nothing but whitespace and comments. For its content it is a new
	// file, so an adapter puts into it what it puts into a file it creates
	// ("$version", "$schema"); the comments stay. An existing "{}" is not
	// blank: its author wrote an object without that key.
	blank bool
}

// readJSONC reads and parses a JSON file with comments. Every adapter that
// has such a file reads it here, in Plan and in Configured alike, so the two
// agree about what cannot be used: a directory in the file's place
// (*KindError), a file that cannot be read (the system's error), one that
// does not parse (*ParseError, with the position and the kind of problem and
// without text of the file), and one that has a value that does not decode
// as a whole (*ShapeError, whose detail is the editor's plain sentence, for
// example "a number is too large to read"). The last case matters because
// Get answers "not there" both for a missing key and for a value it cannot
// decode: without this check an adapter would count such a value as absent
// and overwrite it. A file that is not there is not an error.
func readJSONC(path string) (*jsoncFile, error) {
	old, err := readRegular(path)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return &jsoncFile{path: path, doc: jsonc.New(), blank: true}, nil
	}
	doc, err := jsonc.Parse(old)
	if err != nil {
		return nil, &ParseError{Path: path, Format: "JSON", Detail: err.Error()}
	}
	if doc.HasValue() {
		if _, ok := doc.Get(); !ok {
			return nil, &ShapeError{Path: path, Detail: decodeDetail(old)}
		}
	}
	return &jsoncFile{path: path, old: old, doc: doc, blank: !doc.HasValue()}, nil
}

// decodeDetail says in plain words why a text that the editor parses does not
// decode as a whole: a number does not fit into a float64, or the values are
// nested deeper than encoding/json goes. It decodes the text again the way
// the editor does, so the sentence is the editor's, and holds no text of the
// file.
func decodeDetail(src []byte) string {
	var v any
	err := json.Unmarshal(src, &v)
	if err == nil {
		return "the text cannot be decoded"
	}
	var number *json.UnmarshalTypeError
	switch {
	case errors.As(err, &number):
		return "a number is too large to read"
	case strings.Contains(err.Error(), "max depth"):
		return "the values are nested too deeply"
	}
	return "the text cannot be decoded"
}

// shapeProblems are the ends of the editor's messages about the file it
// edits: a key on the way holds another kind of value than the edit needs,
// or stands twice in its object; a number in the file is too large to read;
// the file's values are nested deeper than can be read.
var shapeProblems = []string{
	" is not an object", " is not a list", " appears more than once",
	"a number is too large to read", "the values are nested too deeply",
}

// editError turns an error of the editor into the error the command
// reports. Only a complaint about the user's file is a *ShapeError, which
// tells the user to fix the file. Anything else (a value that cannot be
// written, the editor's own safety net) is not the file's fault and is
// returned with the file's name in front of it.
func (f *jsoncFile) editError(err error) error {
	if err == nil {
		return nil
	}
	for _, problem := range shapeProblems {
		if strings.HasSuffix(err.Error(), problem) {
			return &ShapeError{Path: f.path, Detail: err.Error()}
		}
	}
	return fmt.Errorf("%s: %w", f.path, err)
}

// set writes value at a path of object keys (jsonc.Doc.Set).
func (f *jsoncFile) set(value any, path ...string) error {
	return f.editError(f.doc.Set(value, path...))
}

// get returns the value at a path of object keys and whether it is there.
func (f *jsoncFile) get(path ...string) (any, bool) { return f.doc.Get(path...) }

// text returns the text at a path of object keys; a key that is absent or
// null gives "". Any other kind of value is a *ShapeError: Ülgen reads such
// a key to decide what to write, and does not guess what a number or a list
// there was meant to say.
func (f *jsoncFile) text(path ...string) (string, error) {
	value, _ := f.get(path...)
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	}
	return "", &ShapeError{Path: f.path, Detail: strings.Join(path, ".") + " is not text"}
}

// change puts the file into the plan when its text is no longer what was
// read.
func (f *jsoncFile) change(plan *Plan) { plan.change(f.path, f.old, f.doc.Bytes()) }

// change appends a file change to the plan when the content differs.
func (p *Plan) change(path string, old, new []byte) {
	if old != nil && string(old) == string(new) {
		return
	}
	p.Files = append(p.Files, FileChange{Path: path, Old: old, New: new})
}

// remove puts the removal of a file into the plan.
func (p *Plan) remove(path string, old []byte) {
	p.Files = append(p.Files, FileChange{Path: path, Old: old, Delete: true})
}

// unset removes the value at a path of object keys, and after it every
// object on the way that the removal left empty: an object Ülgen made for
// its value should not stay behind as "{}". A path that leads to nothing
// changes nothing.
func (f *jsoncFile) unset(path ...string) error {
	if _, has := f.get(path...); !has {
		return nil
	}
	if err := f.editError(f.doc.Delete(path...)); err != nil {
		return err
	}
	for n := len(path) - 1; n > 0; n-- {
		parent, _ := f.get(path[:n]...)
		if object, isObject := parent.(map[string]any); !isObject || len(object) > 0 {
			break
		}
		if err := f.editError(f.doc.Delete(path[:n]...)); err != nil {
			return err
		}
	}
	return nil
}

// note appends a note to the plan.
func (p *Plan) note(key string, args ...any) {
	p.Notes = append(p.Notes, Note{Key: key, Args: args})
}

// detect finds a binary on PATH and asks it for its version. A result that
// is not an absolute path is not used: started later, it would be whatever
// file of that name lies in the directory the command runs in.
func detect(ctx context.Context, env Env, name string) Detection {
	path, err := env.LookPath(name)
	if err != nil || !filepath.IsAbs(path) {
		return Detection{}
	}
	d := Detection{Installed: true, Binary: path}
	if out, err := env.Version(ctx, path); err == nil {
		d.Version = versionToken(out)
	}
	return d
}

// maxVersionLen bounds a version number as Ülgen shows it.
const maxVersionLen = 32

// versionToken returns the first word of s that looks like a version
// number ("0.25.1" in "codex-cli 0.25.1"), or "". The text is another
// program's output and ends up on the screen, so only the characters a
// version number is made of are kept (see clipVersion).
func versionToken(s string) string {
	for _, f := range strings.Fields(s) {
		f = strings.TrimRight(clipVersion(strings.TrimPrefix(f, "v")), ".")
		if len(f) > 0 && f[0] >= '0' && f[0] <= '9' && strings.Contains(f, ".") {
			return f
		}
	}
	return ""
}

// clipVersion returns the leading part of s that consists of letters,
// digits and ". + - _ ~", at most maxVersionLen characters of it. Escape
// sequences, brackets and anything else end the version.
func clipVersion(s string) string {
	end := 0
	for end < len(s) && end < maxVersionLen {
		c := s[end]
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c == '.' || c == '+' || c == '-' || c == '_' || c == '~'
		if !ok {
			break
		}
		end++
	}
	return s[:end]
}

// major returns the leading number of a version ("22" → 22, "v20.19.5" → 20),
// or -1.
func major(version string) int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	n, seen := 0, false
	for _, r := range version {
		if r < '0' || r > '9' {
			break
		}
		n, seen = n*10+int(r-'0'), true
	}
	if !seen {
		return -1
	}
	return n
}

// WriteError says a file of a plan could not be written. Done lists the
// files of the same plan that were written before it. Backup is the path of
// the copy that was made of the file before the write failed, or "" when
// none was made. Err can be a *ChangedError, a *ReadOnlyError, a *LinkError
// or a *BackupExistsError; in each case the file was not touched.
type WriteError struct {
	Path   string
	Err    error
	Done   []string
	Backup string
}

// Error names the file, the cause and what was already written.
func (e *WriteError) Error() string {
	msg := fmt.Sprintf("could not write %s: %v", e.Path, e.Err)
	if e.Backup != "" {
		msg += fmt.Sprintf(" (a backup is at %s)", e.Backup)
	}
	if len(e.Done) > 0 {
		msg += fmt.Sprintf(" (already written: %s)", strings.Join(e.Done, ", "))
	}
	return msg
}

// Unwrap returns the cause.
func (e *WriteError) Unwrap() error { return e.Err }

// ChangedError says a file is no longer what the plan was made from: it was
// edited, created or removed in between. Writing it would lose that change.
type ChangedError struct{ Path string }

// Error says that the file changed.
func (e *ChangedError) Error() string {
	return e.Path + " changed while setup was working"
}

// ReadOnlyError says a file is marked read-only. Ülgen takes that as the
// owner's wish and leaves the file alone, on every system.
type ReadOnlyError struct{ Path string }

// Error says that the file is read-only.
func (e *ReadOnlyError) Error() string { return e.Path + " is read-only" }

// LinkError says a file of a plan is a symbolic link that leads to a file
// that does not exist (a link to nothing, or a loop). Writing there would
// replace the link by a file, so Ülgen leaves it alone.
type LinkError struct{ Path string }

// Error says that the link leads nowhere.
func (e *LinkError) Error() string {
	return e.Path + " is a symbolic link that points to a file that does not exist"
}

// BackupExistsError says a backup of a file of a plan is already there and
// holds other content than the file does now. A backup is never overwritten,
// so the file is left alone.
type BackupExistsError struct{ Path, Backup string }

// Error names the file and the backup that is in the way.
func (e *BackupExistsError) Error() string {
	return e.Backup + " is already a backup of " + e.Path + ", with other content"
}

// Check runs the checks Apply runs before it writes anything, for every file
// of the plan, and returns the first refusal as a *WriteError. It writes
// nothing and creates no directory. The command calls it for a dry run, so a
// plan with a refusal at its second file is reported before the first file
// is written.
func Check(plan Plan) error {
	for _, f := range plan.Files {
		if err := checkOne(f); err != nil {
			return &WriteError{Path: f.Path, Err: err}
		}
	}
	return nil
}

// checkOne runs the read-only checks for one file: the link leads to a real
// file, and the file is still what the plan was made from.
func checkOne(f FileChange) error {
	target, err := resolveLink(f.Path)
	if err != nil {
		return err
	}
	current, err := readFile(target)
	if err != nil {
		return err
	}
	if (current == nil) != (f.Old == nil) || !bytes.Equal(current, f.Old) {
		return &ChangedError{Path: f.Path}
	}
	if current != nil {
		if _, err := writeMode(f, target); err != nil {
			return err
		}
	}
	return nil
}

// Apply writes a plan, one file after the other in plan order. For each file:
//
//   - the checks of Check are run first (the link, the comparison with the
//     plan's content, read-only), so a refusal in a later file leaves an
//     earlier file untouched;
//   - a symbolic link is followed, so the file it points to is rewritten and
//     the link stays; a link that leads nowhere is refused (*LinkError);
//   - an existing file is copied to <backupDir>/<toolID>/<file name> first; a
//     backup that is already there with other content is never overwritten
//     (*BackupExistsError);
//   - a file to remove (Delete) is removed after the same last look, and
//     the directory it was in goes with it when nothing else is in it; of a
//     symbolic link the link goes and what it points to stays;
//   - the new content goes to a temporary file in the same directory and is
//     flushed to disk;
//   - then, right before the file is put in place, it is read once more and
//     compared with the plan's content again: a difference removes the
//     temporary file and is a *ChangedError (the backup just made stays).
//     A file the plan expects to be new is put in place with a hard link,
//     which fails when the file has appeared meanwhile (*ChangedError); where
//     the file system has no hard links the rename is used after one more
//     existence check. An existing file is renamed into place with the mode
//     it had (a new file gets 0600).
//
// A failure stops the plan and is reported as a *WriteError; the file it
// names was not changed. A change in the instant between that last look and
// the rename cannot be seen.
func Apply(plan Plan, toolID, backupDir string) error {
	if err := Check(plan); err != nil {
		return err
	}
	var done []string
	for _, f := range plan.Files {
		we, err := applyOne(f, toolID, backupDir)
		if err != nil {
			we.Path, we.Err, we.Done = f.Path, err, done
			return we
		}
		done = append(done, f.Path)
	}
	return nil
}

// applyOne writes one file of a plan. It returns a *WriteError on failure,
// because the backup path it may carry is only known inside.
func applyOne(f FileChange, toolID, backupDir string) (*WriteError, error) {
	target, err := resolveLink(f.Path)
	if err != nil {
		return &WriteError{}, err
	}
	backupPath := ""
	mode := os.FileMode(0o600)
	if f.Old != nil {
		if mode, err = writeMode(f, target); err != nil {
			return &WriteError{}, err
		}
		backupPath = BackupPath(backupDir, toolID, f.Path)
		if err := backup(backupPath, f.Old); err != nil {
			var exists *BackupExistsError
			if errors.As(err, &exists) {
				exists.Path = f.Path
			}
			return &WriteError{}, err // no backup was made
		}
	}
	we := &WriteError{Backup: backupPath}
	if f.Delete {
		return we, removeFile(target, f)
	}
	return we, placeFile(target, f, mode)
}

// writeMode returns the mode to write a file with: the mode it has now, and
// a *ReadOnlyError when the owner cannot write it.
func writeMode(f FileChange, target string) (os.FileMode, error) {
	info, err := os.Stat(target)
	if err != nil {
		return 0, err
	}
	mode := info.Mode().Perm()
	if mode&0o200 == 0 {
		return 0, &ReadOnlyError{Path: f.Path}
	}
	return mode, nil
}

// removeFile removes f.Path after one more look at target, the file it
// leads to: it must still be what the plan was made from. The directory the
// file was in is removed too when it is empty now; os.Remove refuses one
// that is not.
func removeFile(target string, f FileChange) error {
	current, err := readFile(target)
	if err != nil {
		return err
	}
	if current == nil || !bytes.Equal(current, f.Old) {
		return &ChangedError{Path: f.Path}
	}
	if err := os.Remove(f.Path); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(f.Path))
	return nil
}

// placeFile writes f.New for f.Path at target. A file the plan expects to be
// new is put in place without overwriting anything: see the two steps.
func placeFile(target string, f FileChange, mode os.FileMode) error {
	tmp, err := writeAtomic(target, f.New, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // a no-op once the file is in place
	afterTemp(target)

	// The last look: the file must still be what the plan was made from.
	current, err := readFile(target)
	if err != nil {
		return err
	}
	if (current == nil) != (f.Old == nil) || !bytes.Equal(current, f.Old) {
		return &ChangedError{Path: f.Path}
	}
	if f.Old != nil {
		return rename(tmp, target)
	}
	// A new file: a hard link fails when the target exists, so a file that
	// appeared in the meantime is kept. Where the file system has no hard
	// links, fall back to the rename after one more existence check.
	if err := link(tmp, target); err != nil {
		if _, statErr := os.Lstat(target); statErr == nil {
			return &ChangedError{Path: f.Path}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return rename(tmp, target)
	}
	return nil
}

// writeAtomic writes data to a temporary file next to path, flushes it to
// disk and returns the temporary file's name, creating the directory when
// needed. The caller moves it into place (rename or link) or removes it; on
// any failure here the temporary file is removed and path is as it was. A
// temporary file left by a run that was killed between the flush and the
// move (a ".ulgen-*" next to the target) is not removed by a later run and
// is harmless: it is not the target, and a later run writes a fresh one.
func writeAtomic(path string, data []byte, mode os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".ulgen-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// rename is os.Rename and link is os.Link; tests replace them to make the
// last step fail or to exercise the hard-link path. afterTemp runs right
// after the temporary file is complete and before the last look.
var (
	rename    = os.Rename
	link      = os.Link
	afterTemp = func(string) {}
)

// resolveLink returns the file a path leads to: the path itself, or, for a
// symbolic link, what it points to. A link that leads nowhere (to a file
// that does not exist, or a loop) is a *LinkError, because writing there
// would replace the link by a file.
func resolveLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", &LinkError{Path: path}
	}
	return target, nil
}

// BackupPath is where Apply puts the copy of a file it replaces. The name is
// the file's own name: two files of one tool whose paths have the same base
// name share one backup path, and Apply accepts that only when their content
// is identical, otherwise the second is refused (*BackupExistsError), so a
// backup is never written over.
func BackupPath(backupDir, toolID, path string) string {
	return filepath.Join(backupDir, toolID, filepath.Base(path))
}

// backup writes data to path, creating the directory. A file that is already
// there is left alone: when it holds the same data that is fine, otherwise it
// is a *BackupExistsError, so a backup is never overwritten.
func backup(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return &BackupExistsError{Backup: path}
	}
	if err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return fmt.Errorf("backup %s: %w", path, err)
	}
	return nil
}
