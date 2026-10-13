package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Codex sets up the OpenAI Codex CLI (github.com/openai/codex).
//
// Ülgen writes one file, the profile <Codex directory>/evren.config.toml. It
// holds the EVREN provider and the settings that select it; Codex lays it
// over the user's config.toml when it is started with "--profile evren".
// config.toml itself is the user's and Codex's own: Ülgen reads it, to refuse
// what would break the profile, and never writes it.
type Codex struct{}

const (
	// codexProfile is the profile name: "codex --profile evren" reads
	// evren.config.toml.
	codexProfile = "evren"

	// codexProvider is the id of the provider the profile defines and
	// selects: [model_providers.evren].
	codexProvider = "evren"

	// codexFallbackWindow is the context window Codex assumes for a model it
	// does not know; model_context_window can only lower it.
	codexFallbackWindow = 272000

	// codexKeyHelp is what Codex shows when the key variable is not set; the
	// command at its end is ShellHint for the system.
	codexKeyHelp = `Start Codex with "ulgen run codex", or put the key in your shell first: `

	// codexProfileHeader is the comment at the top of the profile file.
	codexProfileHeader = `# EVREN profile for Codex CLI, written by "ulgen setup codex". Use it with: codex --profile evren
#
# Ülgen rewrites this file when one of its own keys has to change. Your other
# keys are kept, with every value; comments are not, and neither is the layout
# (the order of the keys, the way strings and numbers are written, inline
# tables). The file as it was goes to Ülgen's backups.
# Ülgen sets: model, model_provider, web_search, model_context_window,
# [features] view_image, and in [model_providers.evren]: name, base_url,
# wire_api, env_key, env_key_instructions.

`
)

// ID is the name the tool has on Ülgen's command line: "ulgen setup codex".
func (Codex) ID() string { return "codex" }

// Name is the tool's name as it is shown to the user.
func (Codex) Name() string { return "Codex CLI" }

// Detect looks for the binary "codex" on PATH and asks it for its version.
func (Codex) Detect(ctx context.Context, env Env) Detection { return detect(ctx, env, "codex") }

// InstallHint is the note that tells how to install Codex CLI.
func (Codex) InstallHint(Env) Note { return Note{Key: "setup.install.codex"} }

// codexDir is Codex's directory, found the way Codex finds it
// (codex-rs/utils/home-dir/src/lib.rs): $CODEX_HOME when it is not empty,
// taken as it is and not trimmed, else ~/.codex. A CODEX_HOME that does not
// exist (*MissingDirError) or is not a directory (*KindError) is an error,
// as it is for Codex. Ülgen also refuses a relative one
// (*RelativePathError: Codex would read it from the directory it is started
// in, which Ülgen cannot know) and, without CODEX_HOME, a home directory
// that is unknown or relative (*HomeError): it never works in a path under
// the working directory.
//
// Whether a value names a directory is the system's to say, as it is for
// Codex. Windows drops a space at the end of a name, so there "dir " is the
// directory "dir"; everywhere else it is another name.
//
// A CODEX_HOME that passes those checks is returned as the real path of the
// directory it names: every file Ülgen reads or writes under it is then one
// under that directory, reached by its real name and not through a link. That
// keeps the file Ülgen refuses (a profile that is a link to config.toml), the
// file it writes, and the paths it prints all about one and the same place.
// When the real path cannot be found, the value is returned as it was.
func codexDir(env Env) (string, error) {
	dir := ""
	if env.Getenv != nil {
		dir = env.Getenv("CODEX_HOME")
	}
	if dir == "" {
		home, err := env.home()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".codex"), nil
	}
	if !filepath.IsAbs(dir) {
		return "", &RelativePathError{Var: "CODEX_HOME", Value: dir}
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", &MissingDirError{Var: "CODEX_HOME", Path: dir}
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", &KindError{Path: dir, WantDir: true, Var: "CODEX_HOME"}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir, nil
	}
	return filepath.Clean(real), nil
}

// Codex's own options, from "codex --help" of 0.162.1 and
// codex-rs/cli/src/main.rs, codex-rs/utils/cli/src/shared_options.rs and
// codex-rs/tui/src/cli.rs. Launch needs them to tell a subcommand from the
// value of an option.
var (
	// codexValueOptions take one value, in the same argument or the next.
	codexValueOptions = map[string]bool{
		"-c": true, "--config": true, "--enable": true, "--disable": true,
		"--remote": true, "--remote-auth-token-env": true, "-m": true, "--model": true,
		"--local-provider": true, "-s": true, "--sandbox": true, "-C": true, "--cd": true,
		"--add-dir": true, "-a": true, "--ask-for-approval": true,
	}

	// codexFlags take no value.
	codexFlags = map[string]bool{
		"--strict-config": true, "--oss": true, "--approve-for-me": true, "--not-so-yolo": true,
		"--dangerously-bypass-approvals-and-sandbox": true, "--yolo": true,
		"--dangerously-bypass-hook-trust": true, "--worktree": true, "--search": true,
		"--no-alt-screen": true, "--no-daemon": true, "-h": true, "--help": true, "-V": true, "--version": true,
	}

	// codexNoProfile are the subcommands that refuse --profile
	// (profile_v2_for_subcommand in codex-rs/cli/src/main.rs): every one
	// that is not a runtime command or "mcp". "debug" is not here because
	// "debug prompt-input" takes the option; codexTakesProfile looks at it.
	codexNoProfile = map[string]bool{
		"login": true, "logout": true, "plugin": true, "app-server": true, "remote-control": true,
		"app": true, "completion": true, "update": true, "doctor": true, "apply": true, "a": true,
		"migrate-rollouts": true, "cloud": true, "cloud-tasks": true, "exec-server": true,
		"features": true, "tcp-tunnel": true, "execpolicy": true, "responses-api-proxy": true,
		"stdio-to-uds": true,
	}
)

// Launch returns the arguments for "ulgen run codex": the user's own, with
// "--profile evren" in front unless they already select a profile or name a
// subcommand that refuses the option.
func (Codex) Launch(_ Env, args []string) []string {
	if codexSelectsProfile(args) || !codexTakesProfile(args) {
		return args
	}
	return append([]string{"--profile", codexProfile}, args...)
}

// codexSelectsProfile reports whether the arguments select a profile:
// --profile, --profile=NAME, -p, -pNAME or -p=NAME before any "--".
func codexSelectsProfile(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--profile" || strings.HasPrefix(a, "--profile=") || (strings.HasPrefix(a, "-p") && !strings.HasPrefix(a, "--")) {
			return true
		}
	}
	return false
}

// codexTakesProfile reports whether Codex accepts --profile with these
// arguments. It is false only when the word that names a subcommand, the
// first word that is not an option or an option's value, names one that
// refuses the option. Where Ülgen cannot tell (an option it does not know)
// the answer is true: Codex then says so itself, while without the profile it
// would run with another provider.
func codexTakesProfile(args []string) bool {
	images := false // after -i or --image every word is an image, until the next option
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return true // what follows is the prompt
		case len(a) > 1 && a[0] == '-':
			name, attached := codexOption(a)
			image := name == "-i" || name == "--image"
			images = image && !attached
			switch {
			case image, codexFlags[name]:
			case codexValueOptions[name]:
				if !attached {
					i++ // the value is the next argument
				}
			default:
				return true // an option Ülgen does not know
			}
		case images:
		case a == "debug":
			// "debug" is not a subcommand that refuses the option: it is a
			// group, and "debug prompt-input" takes it. What names the
			// subcommand is the first word after "debug" that is not an option
			// or an option's value, so the scan skips those like the loop
			// above: a "--config x" between them must not be read as one.
			return codexDebugTakesProfile(args[i+1:])
		default:
			return !codexNoProfile[a]
		}
	}
	return true
}

// codexDebugTakesProfile reports whether the arguments after "debug" name the
// subcommand "prompt-input"; the same option rules apply (an option that
// takes a value keeps its value out of the scan).
func codexDebugTakesProfile(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' {
			name, attached := codexOption(a)
			if codexValueOptions[name] && !attached {
				i++
			}
			continue
		}
		return a == "prompt-input"
	}
	return false
}

// codexOption splits an option argument into its name and whether its value
// is attached: "--model=x" and "-mx" are "--model" and "-m" with a value,
// "--model" and "-m" are without.
func codexOption(arg string) (name string, attached bool) {
	if strings.HasPrefix(arg, "--") {
		name, _, attached = strings.Cut(arg, "=")
		return name, attached
	}
	return arg[:2], len(arg) > 2
}

// codexFiles is what Codex's two files hold, after codexRead has accepted
// them.
type codexFiles struct {
	profilePath string
	oldProfile  []byte         // the profile file's content; nil when there is none
	profile     map[string]any // the profile decoded; empty when there is none
}

// codexRead reads and classifies config.toml and the profile file. Plan and
// Configured both start here, so they agree about every refusal:
//
//   - the Codex directory cannot be found (codexDir);
//   - config.toml does not parse (*ParseError), defines model_providers.evren
//     (*ForeignEntryError) or selects or defines a profile in the old way
//     (*LegacyProfileError, see codexLegacyProfile);
//   - the profile is not a file, does not parse (*ParseError), selects a
//     profile in the old way (*LegacyProfileError) or has one of Ülgen's keys
//     in another shape than Codex allows (*ShapeError).
func codexRead(env Env) (codexFiles, error) {
	dir, err := codexDir(env)
	if err != nil {
		return codexFiles{}, err
	}
	if err := codexCheckConfig(filepath.Join(dir, "config.toml")); err != nil {
		return codexFiles{}, err
	}
	files := codexFiles{profilePath: filepath.Join(dir, codexProfile+".config.toml")}
	info, err := os.Stat(files.profilePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		files.profile = map[string]any{}
		return files, nil
	case err != nil:
		return codexFiles{}, err
	case info.IsDir():
		return codexFiles{}, &ShapeError{Path: files.profilePath, Detail: "it is a directory, not a file"}
	}
	if err := codexProfileLeadsToConfig(files.profilePath, filepath.Join(dir, "config.toml")); err != nil {
		return codexFiles{}, err
	}
	if files.oldProfile, err = readFile(files.profilePath); err != nil {
		return codexFiles{}, err
	}
	if err := toml.Unmarshal(files.oldProfile, &files.profile); err != nil {
		return codexFiles{}, &ParseError{Path: files.profilePath, Format: "TOML", Detail: codexParseDetail(err)}
	}
	if files.profile == nil {
		files.profile = map[string]any{}
	}
	if place, found := codexEmptyKey(files.profile, ""); found {
		return codexFiles{}, &ShapeError{Path: files.profilePath, Detail: "it holds an empty key at " + place +
			"; the TOML library misreads an array that holds a table with an empty key, so ulgen cannot rewrite this profile without risking array items"}
	}
	return files, codexCheckProfile(files.profilePath, files.profile)
}

// codexEmptyKey reports whether a decoded TOML value holds a key that is the
// empty string, at any depth, and names the place. The walk is over the
// decoded value: the TOML library, given an array that holds a table with an
// empty key, decodes it into a []map[string]any and drops the items around
// that table, so the raw text's metadata cannot be trusted to see them. The
// table with the empty key itself survives the decode (measured with the
// library in use), so the walk sees it and the place is named.
func codexEmptyKey(v any, path string) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range tomlKeys(x) {
			here := key
			if path != "" {
				here = path + "." + key
			}
			if key == "" {
				if path == "" {
					here = `""`
				} else {
					here = path + `.""`
				}
				return here, true
			}
			if place, found := codexEmptyKey(x[key], here); found {
				return place, true
			}
		}
	// An array keeps no index in the place: the library drops the items
	// around a table with an empty key, so an index into the decoded array
	// would point at the wrong item of the file.
	case []any:
		for _, item := range x {
			if place, found := codexEmptyKey(item, path); found {
				return place, true
			}
		}
	case []map[string]any:
		for _, item := range x {
			if place, found := codexEmptyKey(item, path); found {
				return place, true
			}
		}
	}
	return "", false
}

// tomlKeys returns a table's keys, sorted, so a walk visits them in a fixed
// order.
func tomlKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// codexProfileLeadsToConfig refuses a profile file that is a symbolic link to
// the user's config.toml: Ülgen writes the profile, so it would write
// config.toml, which is the user's and Codex's own.
func codexProfileLeadsToConfig(profilePath, configPath string) error {
	info, err := os.Lstat(profilePath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil // not a link: readFile handles the rest
	}
	profileInfo, perr := os.Stat(profilePath)
	configInfo, cerr := os.Stat(configPath)
	if perr != nil || cerr != nil || !os.SameFile(profileInfo, configInfo) {
		return nil
	}
	return &ShapeError{Path: profilePath, Detail: "it is a symbolic link that leads to " + configPath + "; ulgen writes the profile and would write config.toml, which is Codex's own file"}
}

// codexCheckConfig reads the user's config.toml for the two things in it
// that would break the profile. It decides by the decoded value, so every
// TOML spelling of a table counts. A file that does not exist is fine.
//
//   - model_providers.evren: Codex merges that table with the profile's key
//     by key, so the provider would be neither the user's nor Ülgen's.
//   - a top-level profile, whatever it names, or profiles.evren: Codex
//     refuses "--profile evren" while either is there (codexLegacyProfile).
func codexCheckConfig(path string) error {
	data, err := readFile(path)
	if err != nil || data == nil {
		return err
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return &ParseError{Path: path, Format: "TOML", Detail: codexParseDetail(err)}
	}
	if providers, ok := doc["model_providers"].(map[string]any); ok {
		if _, has := providers[codexProvider]; has {
			return &ForeignEntryError{Path: path, Entry: "model_providers." + codexProvider}
		}
	}
	if key := codexLegacyProfile(doc, true); key != "" {
		return &LegacyProfileError{Path: path, Key: key}
	}
	return nil
}

// codexParseProblems are the TOML library's short messages that hold no text
// of the user's file; each stands as it is. The library quotes the file in
// its message for a value it cannot read (an unquoted string, for example),
// which can be a secret, so only messages on this list are passed on.
var codexParseProblems = []string{
	"expected newline but got %#U",
	"expected value but found '\\n' instead",
	"expected key but found %#U",
	"unexpected EOF; expected ']'",
	"unexpected EOF; expected '\"'",
	"unexpected EOF; expected '\"'\"'\"'",
	"unexpected EOF; expected '\"\"\"'",
	"unexpected EOF; expected '''",
	"unexpected EOF; expected '.'",
	"strings cannot contain newlines",
	"newlines not allowed within inline tables",
	"=",
	":",
	"empty table name",
	"empty key",
	"control characters are not allowed inside strings; use \\uXXXX or \\UXXXXXXXX to specify control characters",
	"invalid UTF-8 byte",
}

// codexParseDetail turns the TOML library's error into the detail of a
// *ParseError: the position (and column when the library gives a place) and a
// short description of the problem, and never any text of the file. An error
// that is not the library's, or whose message is not recognised, becomes "the
// text does not parse".
func codexParseDetail(err error) string {
	place := ""
	var perr toml.ParseError
	if errors.As(err, &perr) {
		place = fmt.Sprintf("line %d", perr.Position.Line)
		if perr.Position.Col > 0 {
			place = fmt.Sprintf("line %d, column %d", perr.Position.Line, perr.Position.Col)
		}
	}
	msg := plainMessage(perr.Message)
	if place == "" {
		return msg // no position: the description alone, without a leading ": "
	}
	return place + ": " + msg
}

// plainMessage returns the short description of the library's message, or
// "the text does not parse" when it is not one Ülgen recognises.
func plainMessage(message string) string {
	for _, problem := range codexParseProblems {
		if message == problem {
			return problem
		}
	}
	// "expected value but found \"secret\" instead": the token is dropped,
	// the kind of problem stays. The library quotes the file's text here (the
	// token, which can be a secret), so the message itself never passes on.
	if strings.HasPrefix(message, "expected value but found ") {
		return "expected value but found"
	}
	// "invalid escape in string '\q'": the escape is dropped, the kind stays.
	if strings.HasPrefix(message, "invalid escape in string") {
		return "invalid escape in string"
	}
	// "invalid datetime: \"...\"": the value is dropped.
	if strings.HasPrefix(message, "invalid datetime") {
		return "invalid datetime"
	}
	// "invalid duration: \"...\"": the value is dropped.
	if strings.HasPrefix(message, "invalid duration") {
		return "invalid duration"
	}
	return "the text does not parse"
}

// codexLegacyProfile returns what in a decoded file of Codex's selects or
// defines a profile in the old way so that Codex refuses "--profile evren",
// as it would be written in the file, or "". Measured with Codex 0.162.1:
//
//   - a top-level profile that is a string stops it whatever the name, also
//     an empty one, in config.toml and in the profile file alike (in
//     config.toml it stops plain "codex" as well);
//   - a profiles.evren of any kind stops it in config.toml (tables is true);
//     the tables of other profiles do not, and inside the profile file no
//     profiles table does.
//
// A profile or profiles of the wrong type is not looked at: Codex fails on
// it by itself and names it, like any other mistyped key.
func codexLegacyProfile(doc map[string]any, tables bool) string {
	if name, ok := doc["profile"].(string); ok {
		return "profile = " + strconv.Quote(name)
	}
	if profiles, ok := doc["profiles"].(map[string]any); ok && tables {
		if _, has := profiles[codexProfile]; has {
			return "[profiles." + codexProfile + "]"
		}
	}
	return ""
}

// codexShapes are the keys of the profile Ülgen reads or writes into, with
// the kind of value Codex allows there; in is the table a key lives in.
var codexShapes = []struct {
	in   []string
	key  string
	kind string
}{
	{nil, "model", "a string"},
	{nil, "model_provider", "a string"},
	{nil, "model_context_window", "an integer"},
	{nil, "features", "a table"},
	{[]string{"features"}, "view_image", "a boolean"},
	{nil, "model_providers", "a table"},
	{[]string{"model_providers"}, codexProvider, "a table"},
}

// codexCheckProfile refuses a profile that selects a profile in the old way
// (a top-level profile key, which stops Codex here as it does in
// config.toml), one in which a key of codexShapes holds another kind of
// value, and one whose EVREN provider has a token command: Codex does not
// load a provider that has both "auth" and the "env_key" Ülgen sets.
func codexCheckProfile(path string, profile map[string]any) error {
	if key := codexLegacyProfile(profile, false); key != "" {
		return &LegacyProfileError{Path: path, Key: key}
	}
	for _, shape := range codexShapes {
		table := profile
		for _, name := range shape.in {
			table, _ = table[name].(map[string]any)
		}
		if value, has := table[shape.key]; has && tomlKind(value) != shape.kind {
			key := strings.Join(append(append([]string{}, shape.in...), shape.key), ".")
			return &ShapeError{Path: path, Detail: fmt.Sprintf("%s is %s, not %s", key, tomlKind(value), shape.kind)}
		}
	}
	providers, _ := profile["model_providers"].(map[string]any)
	evren, _ := providers[codexProvider].(map[string]any)
	if _, has := evren["auth"]; has {
		return &ShapeError{Path: path, Detail: "model_providers.evren.auth is set, and Codex does not accept it next to env_key, which ulgen writes"}
	}
	return nil
}

// Configured is true when the profile file exists and selects a provider
// "evren" that reads the key from EVREN_API_KEY. A CODEX_HOME that does not
// exist is "not set up"; everything else codexRead refuses is an error.
func (Codex) Configured(env Env) (bool, error) {
	files, err := codexRead(env)
	var missing *MissingDirError
	if errors.As(err, &missing) {
		return false, nil
	}
	if err != nil || files.oldProfile == nil {
		return false, err
	}
	providers, _ := files.profile["model_providers"].(map[string]any)
	evren, _ := providers[codexProvider].(map[string]any)
	return files.profile["model_provider"] == codexProvider && evren["env_key"] == KeyEnv, nil
}

// Remove removes the profile file when it is Ülgen's, by the rule of
// Configured. It may hold keys the user added; the whole file goes to
// Ülgen's backups first. config.toml is not touched.
func (c Codex) Remove(env Env) (Plan, error) {
	var plan Plan
	ours, err := c.Configured(env)
	if err != nil || !ours {
		return plan, err
	}
	files, err := codexRead(env)
	if err != nil {
		return plan, err
	}
	plan.remove(files.profilePath, files.oldProfile)
	return plan, nil
}

// Plan computes the change to the profile file. config.toml is never in a
// plan. A profile that already says everything is not in it either, so the
// user's comments and key order survive every run that changes nothing.
func (Codex) Plan(env Env, req Request) (Plan, error) {
	var plan Plan
	if err := req.check(); err != nil {
		return plan, err
	}
	files, err := codexRead(env)
	if err != nil {
		return plan, err
	}
	want, notes, err := codexWanted(files.profile, req, env.GOOS)
	if err != nil {
		return plan, err
	}
	if _, differs := tomlDiff(files.profile, want); differs {
		data, err := codexEncode(files.profilePath, want)
		if err != nil {
			return plan, err
		}
		plan.change(files.profilePath, files.oldProfile, data)
		if files.oldProfile != nil {
			plan.note("setup.note.codex_rewritten", Path(files.profilePath))
		}
	}
	plan.Notes = append(plan.Notes, notes...)
	plan.note("setup.note.export", ShellHint(env.GOOS))
	plan.note("setup.note.codex_profile")
	if req.MakeDefault {
		plan.note("setup.note.codex_default")
	}
	return plan, nil
}

// codexWanted returns the profile as it should be: a copy of old with
// Ülgen's keys set and every other key as it was. old has passed
// codexCheckProfile.
//
// The model the profile names stays when EVREN offers it and --model was not
// given. When it has to change, the notes say so if Ülgen did it on its own
// (a model EVREN does not offer), and name every restriction of the old
// model that did not outlive it (codexSetLimits).
func codexWanted(old map[string]any, req Request, goos string) (want map[string]any, notes []Note, err error) {
	want = tomlCopy(old, false).(map[string]any)
	id := req.Default
	named, hadModel := old["model"].(string)
	if _, offered := req.model(named); hadModel && offered && !req.ModelGiven {
		id = named
	}
	model, offered := req.model(id)
	if !offered { // Request.check has made sure of the default
		return nil, nil, &RequestError{Problem: "the default model " + strconv.Quote(id) + " is not one of its models"}
	}
	// A profile that named no model has no restriction of an old model.
	changed := hadModel && named != id
	if changed && !req.ModelGiven {
		notes = append(notes, Note{Key: "setup.note.codex_model_replaced", Args: []any{named, id}})
	}
	want["model"] = id
	want["model_provider"] = codexProvider
	want["web_search"] = "disabled" // EVREN has no hosted search; the tool would never answer
	for _, key := range codexSetLimits(want, model, changed) {
		notes = append(notes, Note{Key: "setup.note.codex_followed_model", Args: []any{key}})
	}
	provider := tomlTable(tomlTable(want, "model_providers"), codexProvider)
	provider["name"] = "EVREN"
	provider["base_url"] = req.BaseURL
	provider["wire_api"] = "responses"
	provider["env_key"] = KeyEnv
	provider["env_key_instructions"] = codexKeyHelp + ShellHint(goos)
	return want, notes, nil
}

// codexSetLimits sets the two keys that hold Codex to what the model can do:
// model_context_window and [features] view_image. A restriction belongs to a
// model, so there are two cases.
//
// The model stays (changed is false): what Ülgen needs is written, and a
// value that restricts further is the user's and stays. That is a positive
// window below the model's, and a false for a model that sees images.
//
// The model changes: both keys are set for the new model from the request,
// whatever the file held, because what it held was for the old model.
// followed names the keys whose value was dropped or raised by that, for a
// note; a value that only got lower, or was zero or less, needs none.
func codexSetLimits(profile map[string]any, model Model, changed bool) (followed []string) {
	if codexSetWindow(profile, model.Context, changed) {
		followed = append(followed, "model_context_window")
	}
	if codexSetViewImage(profile, model.Vision, changed) {
		followed = append(followed, "[features] view_image")
	}
	return followed
}

// codexSetWindow sets model_context_window for a model whose context length
// the API gives as length (0 or less: unknown). Ülgen writes the key only
// when the length is smaller than the window Codex assumes anyway. Without
// fresh, a positive value that is already there stays unless it is above
// that length. With fresh, the key is exactly what Ülgen writes: the length,
// or nothing. A value of zero or less is never left: Codex would take it as
// the window. The result says whether fresh dropped or raised a positive
// value.
func codexSetWindow(profile map[string]any, length int, fresh bool) (lifted bool) {
	current, _ := profile["model_context_window"].(int64)
	needed := length > 0 && length < codexFallbackWindow
	switch {
	case needed && (fresh || current <= 0 || current > int64(length)):
		profile["model_context_window"] = int64(length)
		return current > 0 && current < int64(length)
	case !needed && (fresh || current <= 0):
		delete(profile, "model_context_window")
		return current > 0
	}
	return false
}

// codexSetViewImage sets [features] view_image. For a model without vision
// it is false. For a model that sees images Ülgen writes nothing, and never
// true: without fresh the key is left as it is, with fresh it is removed,
// together with a [features] table that holds nothing else. The result says
// whether fresh removed a value.
func codexSetViewImage(profile map[string]any, vision, fresh bool) (lifted bool) {
	if !vision {
		tomlTable(profile, "features")["view_image"] = false
		return false
	}
	features, _ := profile["features"].(map[string]any)
	if _, has := features["view_image"]; !has || !fresh {
		return false
	}
	delete(features, "view_image")
	if len(features) == 0 {
		delete(profile, "features")
	}
	return true
}

// codexEncode writes the profile as TOML under Ülgen's header and makes sure
// that the text means what was intended: it is decoded again and compared.
// Should the encoder ever change a value, the result is a *ShapeError that
// names the key, never a file.
func codexEncode(path string, profile map[string]any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(codexProfileHeader)
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(tomlCopy(profile, true)); err != nil {
		return nil, &ShapeError{Path: path, Detail: "its values cannot be written as TOML: " + err.Error()}
	}
	var back map[string]any
	if err := toml.Unmarshal(b.Bytes(), &back); err != nil {
		return nil, &ShapeError{Path: path, Detail: "its values cannot be written as TOML: " + err.Error()}
	}
	if key, differs := tomlDiff(profile, back); differs {
		return nil, &ShapeError{Path: path, Detail: "the value of " + key + " cannot be written back unchanged"}
	}
	return b.Bytes(), nil
}

// tomlLocalLayouts are the zones the TOML library puts a date or time into
// that is written without an offset, each with the layout of such a value.
var tomlLocalLayouts = map[string]string{
	"datetime-local": "2006-01-02T15:04:05.999999999",
	"date-local":     "2006-01-02",
	"time-local":     "15:04:05.999999999",
}

// tomlLocal is a date or time without an offset, as the text TOML has for
// it. The library's encoder would write such a time.Time shifted by the
// machine's zone (it converts it to UTC first), so tomlCopy hands it this
// text instead, which the encoder writes as it is.
type tomlLocal string

// MarshalTOML returns the text.
func (l tomlLocal) MarshalTOML() ([]byte, error) { return []byte(l), nil }

// tomlCopy returns a deep copy of a decoded TOML value. With forEncoder, a
// date or time without an offset becomes a tomlLocal.
func tomlCopy(v any, forEncoder bool) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = tomlCopy(item, forEncoder)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, item := range v {
			out[i] = tomlCopy(item, forEncoder).(map[string]any)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = tomlCopy(item, forEncoder)
		}
		return out
	case time.Time:
		if layout, local := tomlLocalLayouts[v.Location().String()]; local && forEncoder {
			return tomlLocal(v.Format(layout))
		}
	}
	return v
}

// tomlTable returns the table at key, which it creates when it is not there.
func tomlTable(parent map[string]any, key string) map[string]any {
	table, ok := parent[key].(map[string]any)
	if !ok {
		table = map[string]any{}
		parent[key] = table
	}
	return table
}

// tomlKind names the kind of a decoded TOML value, for a message.
func tomlKind(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case int64:
		return "an integer"
	case float64:
		return "a float"
	case bool:
		return "a boolean"
	case time.Time:
		return "a date or time"
	case map[string]any:
		return "a table"
	case []any, []map[string]any:
		return "an array"
	}
	return fmt.Sprintf("a value of type %T", v)
}

// tomlDiff compares two decoded TOML documents and returns the dotted path of
// the first value that does not mean the same in both, and whether there is
// such a value. The path is of a place that differs; it can be empty, because
// the empty string is a valid TOML key, so the caller must go by differs and
// not by the path being "".
func tomlDiff(a, b map[string]any) (path string, differs bool) {
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, both := a[key]; !both {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		x, inA := a[key]
		y, inB := b[key]
		if !inA || !inB {
			return key, true
		}
		tx, isTableX := x.(map[string]any)
		ty, isTableY := y.(map[string]any)
		if isTableX && isTableY {
			if inner, innerDiffers := tomlDiff(tx, ty); innerDiffers {
				return key + "." + inner, true
			}
		} else if !tomlSame(x, y) {
			return key, true
		}
	}
	return "", false
}

// tomlSame reports whether two decoded TOML values mean the same.
func tomlSame(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		_, differs := tomlDiff(x, y)
		return ok && !differs
	case []map[string]any, []any:
		xs, ys := tomlItems(a), tomlItems(b)
		if ys == nil || len(xs) != len(ys) {
			return false
		}
		for i := range xs {
			if !tomlSame(xs[i], ys[i]) {
				return false
			}
		}
		return true
	case time.Time:
		y, ok := b.(time.Time)
		return ok && tomlTimeText(x) == tomlTimeText(y)
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	case string, int64, bool:
		return a == b
	}
	return false // a kind the decoder does not produce
}

// tomlItems returns the items of an array, which the decoder gives as
// []map[string]any when all are tables and as []any otherwise; nil when v is
// not an array.
func tomlItems(v any) []any {
	switch v := v.(type) {
	case []any:
		if v == nil {
			return []any{}
		}
		return v
	case []map[string]any:
		items := make([]any, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return items
	}
	return nil
}

// tomlTimeText writes a date or time the way TOML would: by its own layout
// when it has no offset, with the offset otherwise.
func tomlTimeText(t time.Time) string {
	if layout, local := tomlLocalLayouts[t.Location().String()]; local {
		return t.Location().String() + " " + t.Format(layout)
	}
	return t.Format(time.RFC3339Nano)
}
