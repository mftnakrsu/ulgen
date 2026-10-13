package setup

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mftnakrsu/ulgen/internal/setup/jsonc"
)

// Opencode sets up opencode (github.com/anomalyco/opencode).
//
// opencode merges several configuration files of the user, a later one over
// an earlier one, key by key. Ülgen writes one of them and reads all of
// them (opencodeRead): what opencode ends up with decides whether it has a
// default model, and none of them may hold a provider.evren of the user's
// own. Only the directory Ülgen writes in decides whether EVREN is set up.
//
// Ülgen's entry is a provider.evren that carries the reference to the key:
// its options.apiKey is "{env:EVREN_API_KEY}", or its env list holds
// EVREN_API_KEY. That one rule decides what Plan writes into, what
// Configured counts and what is refused: any other provider.evren is the
// user's, and Ülgen does not touch it.
type Opencode struct{}

const (
	// opencodeProvider is the id of the provider Ülgen writes: provider.evren.
	opencodeProvider = "evren"

	// opencodeSmall is the model opencode uses for titles when EVREN offers it.
	opencodeSmall = "deepseek-v4-flash"

	// opencodeOutputFallback is the output limit written for a model whose
	// limit the API does not state; opencode wants both limits or neither.
	opencodeOutputFallback = 8192

	// opencodeSchema is the "$schema" of a configuration file Ülgen starts.
	opencodeSchema = "https://opencode.ai/config.json"

	// opencodeKeyRef is how the provider names the key: opencode puts the
	// value of the variable in its place when it loads the file.
	opencodeKeyRef = "{env:" + KeyEnv + "}"
)

// ID is the name the tool has on Ülgen's command line: "ulgen setup opencode".
func (Opencode) ID() string { return "opencode" }

// Name is the tool's name as it is shown to the user.
func (Opencode) Name() string { return "opencode" }

// opencodeGlobalDir is opencode's global configuration directory:
// $XDG_CONFIG_HOME/opencode, else ~/.config/opencode, on every system. A
// relative XDG_CONFIG_HOME counts as not set (the XDG rule), and an unknown
// home directory is a *HomeError.
func opencodeGlobalDir(env Env) (string, error) {
	if x := env.getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "opencode"), nil
	}
	home, err := env.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode"), nil
}

// opencodeDir is the directory Ülgen writes in: $OPENCODE_CONFIG_DIR, else
// the global one. It is always an absolute path: a relative
// OPENCODE_CONFIG_DIR is a *RelativePathError.
func opencodeDir(env Env) (string, error) {
	if d := env.getenv("OPENCODE_CONFIG_DIR"); d != "" {
		if !filepath.IsAbs(d) {
			return "", &RelativePathError{Var: "OPENCODE_CONFIG_DIR", Value: d}
		}
		return d, nil
	}
	return opencodeGlobalDir(env)
}

// opencodeLayout is where opencode's configuration files of the user are.
type opencodeLayout struct {
	paths []string // the files opencode merges, lowest first
	json  string   // the file Ülgen writes, when there is no opencode.jsonc
	jsonc string   // the file Ülgen writes, when it exists
	home  string   // <home>/.opencode
}

// opencodePaths returns the user's configuration files that opencode merges,
// lowest first, and which of them Ülgen writes. The two lines of opencode
// read these files:
//
//   - 1.x merges config.json, opencode.json and opencode.jsonc of the global
//     directory, in that order, then opencode.json and opencode.jsonc of
//     ~/.opencode, then those of $OPENCODE_CONFIG_DIR when it is set.
//   - 2.x reads opencode.json and opencode.jsonc of one directory,
//     $OPENCODE_CONFIG_DIR when it is set and the global one otherwise, and
//     ~/.opencode over it as a project directory whenever it is started
//     beneath the home directory.
//
// A file counts when either line reads it, in the order of 1.x. Ülgen writes
// in $OPENCODE_CONFIG_DIR when it is set and in the global directory
// otherwise, so ~/.opencode lies beneath its file in the first case and over
// it in the second.
func opencodePaths(env Env) (opencodeLayout, error) {
	dir, err := opencodeDir(env)
	if err != nil {
		return opencodeLayout{}, err
	}
	global, err := opencodeGlobalDir(env)
	if err != nil {
		return opencodeLayout{}, err
	}
	home, err := env.home()
	if err != nil {
		return opencodeLayout{}, err
	}
	layout := opencodeLayout{
		json: filepath.Join(dir, "opencode.json"), jsonc: filepath.Join(dir, "opencode.jsonc"),
		home: filepath.Join(home, ".opencode"),
	}
	paths := []string{
		filepath.Join(global, "config.json"), filepath.Join(global, "opencode.json"), filepath.Join(global, "opencode.jsonc"),
		filepath.Join(layout.home, "opencode.json"), filepath.Join(layout.home, "opencode.jsonc"),
	}
	if env.getenv("OPENCODE_CONFIG_DIR") != "" {
		paths = append(paths, layout.json, layout.jsonc)
	}
	// A directory can be named twice (OPENCODE_CONFIG_DIR set to ~/.opencode):
	// a file counts where opencode reads it last.
	for i, path := range paths {
		if !slices.Contains(paths[i+1:], path) {
			layout.paths = append(layout.paths, path)
		}
	}
	return layout, nil
}

// Detect looks on PATH and in the directory opencode's own installer uses.
// Without a known home directory there is no such directory to look in.
func (Opencode) Detect(ctx context.Context, env Env) Detection {
	if d := detect(ctx, env, "opencode"); d.Installed {
		return d
	}
	home, err := env.home()
	if err != nil {
		return Detection{}
	}
	return detect(ctx, env, filepath.Join(home, ".opencode", "bin", "opencode"))
}

// InstallHint is the note that tells how to install opencode: through npm,
// on every system.
func (Opencode) InstallHint(Env) Note { return Note{Key: "setup.install.opencode"} }

// Launch passes the arguments through.
func (Opencode) Launch(_ Env, args []string) []string { return args }

// UserFiles returns the two names the configuration file Ülgen writes can
// have. "ulgen run" uses them to tell the user's own file from a project's.
func (Opencode) UserFiles(env Env) ([]string, error) {
	dir, err := opencodeDir(env)
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc")}, nil
}

// opencodeFiles are the user's configuration files as Ülgen found them.
type opencodeFiles struct {
	layers []*jsoncFile // the files that are there, lowest first; a later one wins, key by key
	target *jsoncFile   // the file Ülgen writes; it is among the layers also when it is new
	above  []*jsoncFile // the layers opencode reads over the target: those of ~/.opencode, at most
	// copies and homeCopies are the other files that hold an entry of
	// Ülgen's: those beneath the target, and those of ~/.opencode wherever
	// it stands. Ülgen does not keep them up to date.
	copies, homeCopies []*jsoncFile
	// ours says that EVREN is set up: one of the two files of the directory
	// Ülgen writes in holds its entry. Both lines of opencode read those two.
	ours bool
}

// opencodeRead reads the files of opencodePaths. Plan and Configured both
// start here, so they agree about every refusal:
//
//   - a directory cannot be found (opencodeDir, opencodeGlobalDir);
//   - a file cannot be used (readJSONC);
//   - a file has a provider that is not an object, or a provider.evren that
//     is not one (*ShapeError);
//   - a file has a provider.evren that is not Ülgen's (*ForeignEntryError).
//     That holds for every file, not only the one Ülgen writes: opencode
//     merges the entries of all of them into one provider, and the key
//     reference of Ülgen's entry would end up next to the user's address.
func opencodeRead(env Env) (*opencodeFiles, error) {
	layout, err := opencodePaths(env)
	if err != nil {
		return nil, err
	}
	files := &opencodeFiles{}
	var read []*jsoncFile
	for _, path := range layout.paths {
		file, err := readJSONC(path)
		if err != nil {
			return nil, err
		}
		// Ülgen writes opencode.jsonc when it exists, else opencode.json,
		// there or not.
		if path == layout.json || path == layout.jsonc && file.old != nil {
			files.target = file
		}
		read = append(read, file)
	}
	for _, file := range read {
		if file.old == nil && file != files.target {
			continue // not there
		}
		ours, err := opencodeOurs(file)
		if err != nil {
			return nil, err
		}
		files.add(file, ours, layout)
	}
	return files, nil
}

// add puts a file that was read among the layers and says what an entry of
// Ülgen's in it means (see the fields of opencodeFiles).
func (o *opencodeFiles) add(file *jsoncFile, ours bool, layout opencodeLayout) {
	if slices.Contains(o.layers, o.target) && file != o.target {
		o.above = append(o.above, file)
	}
	o.layers = append(o.layers, file)
	inDir := file.path == layout.json || file.path == layout.jsonc
	switch {
	case !ours:
	case inDir:
		o.ours = true
		if file != o.target {
			o.copies = append(o.copies, file)
		}
	case filepath.Dir(file.path) == layout.home:
		o.homeCopies = append(o.homeCopies, file)
	default:
		o.copies = append(o.copies, file)
	}
}

// last returns the value opencode ends up with for a top-level key, that
// of the last file that has it, and that file; nil without one.
func (o *opencodeFiles) last(key string) (value any, from *jsoncFile) {
	for _, file := range o.layers {
		if v, has := file.get(key); has {
			value, from = v, file
		}
	}
	return value, from
}

// opencodeOurs reports whether a file holds an entry of Ülgen's: a
// provider.evren with the reference to the key. A file without the entry
// holds none; a provider.evren of any other kind is refused (see
// opencodeRead).
func opencodeOurs(file *jsoncFile) (bool, error) {
	providers, has := file.get("provider")
	if !has {
		return false, nil
	}
	if _, isObject := providers.(map[string]any); !isObject {
		return false, &ShapeError{Path: file.path, Detail: "provider is not an object"}
	}
	entry, has := file.get("provider", opencodeProvider)
	if !has {
		return false, nil
	}
	provider, isObject := entry.(map[string]any)
	if !isObject {
		return false, &ShapeError{Path: file.path, Detail: "provider." + opencodeProvider + " is not an object"}
	}
	env, _ := provider["env"].([]any)
	if asObject(provider["options"])["apiKey"] != opencodeKeyRef && !slices.Contains(env, any(KeyEnv)) {
		return false, &ForeignEntryError{Path: file.path, Entry: "provider." + opencodeProvider}
	}
	return true, nil
}

// Configured is true when opencode.json or opencode.jsonc of the directory
// Ülgen writes in holds an entry of Ülgen's. An entry in another file that
// only one line of opencode reads (config.json, the global directory beneath
// $OPENCODE_CONFIG_DIR) or that "ulgen run" takes for a project file
// (~/.opencode) does not count. The refusals of opencodeRead apply to every
// file.
func (Opencode) Configured(env Env) (bool, error) {
	files, err := opencodeRead(env)
	if err != nil {
		return false, err
	}
	return files.ours, nil
}

// Plan computes the changes to the configuration file Ülgen writes:
// opencode.jsonc of its directory when that exists, opencode.json otherwise.
func (Opencode) Plan(env Env, req Request) (Plan, error) {
	var plan Plan
	if err := req.check(); err != nil {
		return plan, err
	}
	files, err := opencodeRead(env)
	if err != nil {
		return plan, err
	}
	if files.target.blank {
		if err := files.target.set(opencodeSchema, "$schema"); err != nil {
			return plan, err
		}
	}
	if err := opencodeSetProvider(&plan, files.target, req); err != nil {
		return plan, err
	}
	if err := opencodeSetDefaults(&plan, files, req); err != nil {
		return plan, err
	}
	files.notes(&plan, env)
	plan.note("setup.note.export", ShellHint(env.GOOS))
	files.target.change(&plan)
	return plan, nil
}

// Remove undoes Plan in the file Ülgen writes: provider.evren goes when it
// is Ülgen's, and "model" and "small_model" go when that file sets them to a
// model of EVREN's. The refusals of opencodeRead apply, so an entry of the
// user's under EVREN's name is not removed. Other files that hold a copy of
// Ülgen's entry are named in a note; Ülgen does not write them.
func (Opencode) Remove(env Env) (Plan, error) {
	var plan Plan
	files, err := opencodeRead(env)
	if err != nil {
		return plan, err
	}
	target := files.target
	if target.old == nil {
		return plan, nil
	}
	if ours, _ := opencodeOurs(target); ours {
		if err := target.unset("provider", opencodeProvider); err != nil {
			return plan, err
		}
	}
	for _, key := range []string{"model", "small_model"} {
		value, _ := target.get(key)
		provider := ""
		switch v := value.(type) {
		case string:
			provider, _, _ = strings.Cut(v, "/")
		case map[string]any:
			provider, _ = v["providerID"].(string)
		}
		if provider == opencodeProvider {
			if err := target.unset(key); err != nil {
				return plan, err
			}
		}
	}
	for _, copy := range append(slices.Clone(files.copies), files.homeCopies...) {
		plan.note("setup.note.remove_copy", Path(copy.path))
	}
	target.change(&plan)
	return plan, nil
}

// opencodeSetProvider writes provider.evren. A file without the entry gets
// it whole. In an entry that is there (it is Ülgen's, see opencodeRead) only
// Ülgen's keys are set, each by its path: npm, name, options.baseURL,
// options.apiKey and, in models, the keys of each model. What the user added
// next to them, a timeout or headers under options for example, stays.
//
// Two things of the user's do not stay, and each gets a note: another
// options.apiKey than the reference to the key (the entry was Ülgen's by its
// env list), and models EVREN does not have (opencodeSetModels).
func opencodeSetProvider(plan *Plan, target *jsoncFile, req Request) error {
	entry := []string{"provider", opencodeProvider}
	own := jsonc.Object{
		{Key: "npm", Value: "@ai-sdk/openai-compatible"},
		{Key: "name", Value: "EVREN"},
		{Key: "options", Value: jsonc.Object{{Key: "baseURL", Value: req.BaseURL}, {Key: "apiKey", Value: opencodeKeyRef}}},
	}
	had, has := target.get(entry...)
	if !has {
		own = slices.Insert(own, 2, jsonc.Member{Key: "env", Value: []string{KeyEnv}})
		return target.set(append(own, jsonc.Member{Key: "models", Value: opencodeModels(req)}), entry...)
	}
	if key, written := asObject(asObject(had)["options"])["apiKey"]; written && key != opencodeKeyRef {
		plan.note("setup.note.opencode_key_replaced", Path(target.path))
	}
	if err := opencodeSetLeaves(target, own, entry); err != nil {
		return err
	}
	return opencodeSetModels(plan, target, req, asObject(had)["models"])
}

// opencodeSetLeaves writes the members of value under path, each by its own
// path, and the members of an object among them the same way. So only what
// value names is written, and everything else under path stays as it is.
func opencodeSetLeaves(file *jsoncFile, value jsonc.Object, path []string) error {
	for _, member := range value {
		at := append(slices.Clip(path), member.Key)
		if inner, isObject := member.Value.(jsonc.Object); isObject {
			if err := opencodeSetLeaves(file, inner, at); err != nil {
				return err
			}
			continue
		}
		if err := file.set(member.Value, at...); err != nil {
			return err
		}
	}
	return nil
}

// opencodeSetModels makes the models of an existing entry the request's.
// had is what the entry has under models. A model that is there keeps what
// the user added to it: only Ülgen's keys in it are set. A new model is
// added whole.
//
// A model the request does not have is dropped, with a note that names it.
// The editor cannot take a member out of an object, so models is then
// written whole, each model with the user's additions behind Ülgen's keys.
func opencodeSetModels(plan *Plan, target *jsoncFile, req Request, had any) error {
	path := []string{"provider", opencodeProvider, "models"}
	existing, isObject := had.(map[string]any)
	var dropped []string
	for _, id := range slices.Sorted(maps.Keys(existing)) {
		if _, offered := req.model(id); !offered {
			dropped = append(dropped, id)
		}
	}
	if len(dropped) > 0 {
		plan.note("setup.note.opencode_models_dropped", strings.Join(dropped, ", "), Path(target.path))
	}
	if !isObject || len(dropped) > 0 {
		models := jsonc.Object{}
		for _, member := range opencodeModels(req) {
			member.Value = withExtras(member.Value.(jsonc.Object), existing[member.Key])
			models = append(models, member)
		}
		return target.set(models, path...)
	}
	for _, member := range opencodeModels(req) {
		at := append(slices.Clip(path), member.Key)
		if _, has := existing[member.Key]; !has {
			if err := target.set(member.Value, at...); err != nil {
				return err
			}
		} else if err := opencodeSetLeaves(target, member.Value.(jsonc.Object), at); err != nil {
			return err
		}
	}
	return nil
}

// withExtras returns own with the members of had, a decoded object, that
// own does not name behind its own, in the order of their keys; a member
// that is an object in both is treated the same way. It is how what the
// user added survives a value that has to be written whole.
func withExtras(own jsonc.Object, had any) jsonc.Object {
	old := asObject(had)
	out := jsonc.Object{}
	named := map[string]bool{}
	for _, member := range own {
		named[member.Key] = true
		if inner, isObject := member.Value.(jsonc.Object); isObject {
			member.Value = withExtras(inner, old[member.Key])
		}
		out = append(out, member)
	}
	for _, key := range slices.Sorted(maps.Keys(old)) {
		if !named[key] {
			out = append(out, jsonc.Member{Key: key, Value: old[key]})
		}
	}
	return out
}

// opencodeModels is the models object of the provider, in the API's order.
// A model's limit is written only when its context length is known:
// opencode wants both limits or neither, and without them it assumes limits
// of its own.
func opencodeModels(req Request) jsonc.Object {
	models := jsonc.Object{}
	for _, m := range req.Models {
		input := []string{"text"}
		if m.Vision {
			input = []string{"text", "image"}
		}
		model := jsonc.Object{{Key: "name", Value: m.ID}, {Key: "tool_call", Value: true}}
		if m.Context > 0 {
			output := m.MaxOutput
			if output <= 0 {
				output = opencodeOutputFallback
			}
			model = append(model, jsonc.Member{Key: "limit", Value: jsonc.Object{{Key: "context", Value: m.Context}, {Key: "output", Value: output}}})
		}
		model = append(model, jsonc.Member{Key: "modalities", Value: jsonc.Object{{Key: "input", Value: input}, {Key: "output", Value: []string{"text"}}}})
		models = append(models, jsonc.Member{Key: m.ID, Value: model})
	}
	return models
}

// defaultOf says what one of opencode's two defaults, "model" or
// "small_model", is to Ülgen, names its model and returns the file that
// decides it (nil without one). The value counts that opencode ends up with:
// that of the last file that has the key. It is EVREN's when it is
// "evren/<model>", or an object whose providerID is "evren".
func (o *opencodeFiles) defaultOf(key string, req Request) (state defaultState, model string, from *jsoncFile, err error) {
	value, from := o.last(key)
	provider := ""
	switch v := value.(type) {
	case nil:
		return defaultNone, "", from, nil
	case string:
		if v == "" {
			return defaultNone, "", from, nil
		}
		provider, model, _ = strings.Cut(v, "/")
		model, _, _ = strings.Cut(model, "#") // "#variant" is not part of the id
	case map[string]any:
		provider, _ = v["providerID"].(string)
		model, _ = v["model"].(string)
	default:
		return 0, "", from, &ShapeError{Path: from.path, Detail: key + " is neither text nor an object"}
	}
	switch _, offered := req.model(model); {
	case provider != opencodeProvider:
		return defaultForeign, model, from, nil
	case offered, model == "":
		return defaultEvren, model, from, nil // an object without a model is not judged
	}
	return defaultRetired, model, from, nil
}

// opencodeSetDefaults applies the rule for the default model
// (changesDefault) to "model", and writes "small_model", the model for
// titles, next to it: EVREN's deepseek-v4-flash when it is offered, the
// default model otherwise. small_model is written
//
//   - together with model, when there is no small_model;
//   - with --default;
//   - when it is EVREN's and names a model that is not among EVREN's, with a
//     note that says which.
//
// Either key is written into the file Ülgen writes, and only there: when a
// file opencode reads over it has the key, see setDefault.
func opencodeSetDefaults(plan *Plan, files *opencodeFiles, req Request) error {
	state, was, from, err := files.defaultOf("model", req)
	if err != nil {
		return err
	}
	smallState, smallWas, smallFrom, err := files.defaultOf("small_model", req)
	if err != nil {
		return err
	}
	writes := req.changesDefault(state)
	if !writes || !slices.Contains(files.above, from) {
		plan.defaultNotes(req, state, was, Opencode{}.Name(), Opencode{}.ID())
	}
	if writes {
		if err := files.setDefault(plan, "model", req.Default, from); err != nil {
			return err
		}
	}
	small := req.Default
	if _, offered := req.model(opencodeSmall); offered {
		small = opencodeSmall
	}
	switch {
	case smallState == defaultRetired:
		if !slices.Contains(files.above, smallFrom) {
			plan.note("setup.note.small_replaced", smallWas, small)
		}
	case req.MakeDefault, writes && smallState == defaultNone:
	default:
		return nil
	}
	return files.setDefault(plan, "small_model", small, smallFrom)
}

// setDefault writes "evren/<model>" under key into the file Ülgen writes.
// When the file that decides the key, from, is one opencode reads over that
// file, nothing is written: the value would lose there. A note names the
// file and the value to put into it instead.
func (o *opencodeFiles) setDefault(plan *Plan, key, model string, from *jsoncFile) error {
	value := opencodeProvider + "/" + model
	if slices.Contains(o.above, from) {
		plan.note("setup.note.opencode_set_above", Path(from.path), key, value)
		return nil
	}
	return o.target.set(value, key)
}

// notes adds what the user should know about the files besides the default:
// other files that hold an entry of Ülgen's, which is not kept up to date
// there, and settings that keep EVREN from working although it is set up.
// The two lists of providers are judged as opencode ends up with them: by
// the last file that has the key.
func (o *opencodeFiles) notes(plan *Plan, env Env) {
	for _, file := range o.copies {
		plan.note("setup.note.opencode_copy", Path(file.path), Path(o.target.path))
	}
	for _, file := range o.homeCopies {
		plan.note("setup.note.opencode_home_copy", Path(file.path))
	}
	if list, from := o.last("disabled_providers"); from != nil {
		if disabled, _ := list.([]any); slices.Contains(disabled, any(opencodeProvider)) {
			plan.note("setup.note.opencode_disabled", Path(from.path))
		}
	}
	// A list of enabled providers shuts out every provider it does not name,
	// in both lines of opencode; an empty one shuts out all of them.
	if list, from := o.last("enabled_providers"); from != nil {
		if enabled, isList := list.([]any); isList && !slices.Contains(enabled, any(opencodeProvider)) {
			plan.note("setup.note.opencode_not_enabled", Path(from.path))
		}
	}
	for _, file := range o.layers {
		if _, native := file.get("providers", opencodeProvider); native {
			plan.note("setup.note.opencode_native", Path(file.path))
		}
	}
	if env.getenv("OPENCODE_CONFIG") != "" {
		plan.note("setup.note.opencode_config_var")
	}
}
