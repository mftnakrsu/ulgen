package setup

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Qwen sets up Qwen Code (github.com/QwenLM/qwen-code).
//
// Ülgen's entries are the elements of modelProviders.openai in the user's
// settings.json whose envKey is EVREN_API_KEY. That one rule decides what
// Plan rewrites, what Configured counts and whether the default model is
// EVREN's. Every other element of the list is the user's and stays as it
// is, whatever address it has.
type Qwen struct{}

const (
	// qwenMinNode is the oldest Node.js release Qwen Code runs on.
	qwenMinNode = 22

	// qwenSettingsVersion is the "$version" of a settings file Ülgen starts.
	qwenSettingsVersion = 4
)

// qwenLegacyKeys are top-level keys of Qwen Code's first settings format.
// Qwen Code moves them into the current layout when it starts, and what
// Ülgen wrote next to them would lose against them there.
var qwenLegacyKeys = []string{"selectedAuthType", "usageStatisticsEnabled", "theme"}

// qwenModelVars are the environment variables Qwen Code takes its model
// from, in its order, when the settings name none (AUTH_ENV_MODEL_VARS in
// packages/cli/src/utils/modelConfigUtils.ts).
var qwenModelVars = []string{"OPENAI_MODEL", "QWEN_MODEL"}

// ID is the name the tool has on Ülgen's command line: "ulgen setup qwen".
func (Qwen) ID() string { return "qwen" }

// Name is the tool's name as it is shown to the user.
func (Qwen) Name() string { return "Qwen Code" }

// qwenDir is Qwen Code's directory: $QWEN_HOME, else ~/.qwen. It is always
// an absolute path: an unknown home directory is a *HomeError, and a
// QWEN_HOME that is relative (Qwen Code itself would read it from the
// current directory) is a *RelativePathError.
func qwenDir(env Env) (string, error) {
	h := env.getenv("QWEN_HOME")
	if h != "" && !isTilde(h) {
		if !filepath.IsAbs(h) {
			return "", &RelativePathError{Var: "QWEN_HOME", Value: h}
		}
		return h, nil
	}
	home, err := env.home()
	if err != nil {
		return "", err
	}
	switch {
	case h == "":
		return filepath.Join(home, ".qwen"), nil
	case h == "~":
		return home, nil
	}
	return filepath.Join(home, h[2:]), nil
}

// Detect looks for the qwen binary. When it is missing, a note says what
// the npm install needs.
func (Qwen) Detect(ctx context.Context, env Env) Detection {
	d := detect(ctx, env, "qwen")
	if d.Installed {
		return d
	}
	node, err := env.LookPath("node")
	if err != nil || !filepath.IsAbs(node) {
		d.Notes = append(d.Notes, Note{Key: "setup.note.node_missing"})
		return d
	}
	// The version is another program's output and goes into a note, so only
	// the characters of a version number are kept.
	out, err := env.Version(ctx, node)
	if v := clipVersion(strings.TrimSpace(out)); err == nil && major(v) >= 0 && major(v) < qwenMinNode {
		d.Notes = append(d.Notes, Note{Key: "setup.note.node_old", Args: []any{v}})
	}
	return d
}

// InstallHint is the note that tells how to install Qwen Code: through npm,
// on every system.
func (Qwen) InstallHint(Env) Note { return Note{Key: "setup.install.qwen"} }

// Launch passes the arguments through.
func (Qwen) Launch(_ Env, args []string) []string { return args }

// UserFiles returns the user-level settings file, the one Configured reads
// and Plan writes. "ulgen run" uses it to tell this file from a project's.
func (Qwen) UserFiles(env Env) ([]string, error) {
	dir, err := qwenDir(env)
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(dir, "settings.json")}, nil
}

// qwenRead finds Qwen Code's directory and reads the user's settings.json.
// Plan and Configured both start here, so they agree about every refusal:
// the directory cannot be found (qwenDir), the file cannot be used
// (readJSONC), or it is in Qwen Code's older format (qwenCheckFormat).
func qwenRead(env Env) (dir string, settings *jsoncFile, err error) {
	if dir, err = qwenDir(env); err != nil {
		return "", nil, err
	}
	if settings, err = readJSONC(filepath.Join(dir, "settings.json")); err != nil {
		return "", nil, err
	}
	return dir, settings, qwenCheckFormat(settings)
}

// qwenCheckFormat refuses a settings file of Qwen Code's first format: one
// without "$version" that has one of qwenLegacyKeys at the top level. Qwen
// Code converts such a file when it starts, and in that conversion the old
// keys win over the nested ones Ülgen would write: the user's
// usageStatisticsEnabled would stand, and selectedAuthType would be replaced
// by Ülgen's. So Ülgen waits for the conversion.
func qwenCheckFormat(settings *jsoncFile) error {
	if _, versioned := settings.get("$version"); versioned {
		return nil
	}
	for _, key := range qwenLegacyKeys {
		if _, has := settings.get(key); has {
			return &ShapeError{Path: settings.path, Detail: "it is in Qwen Code's older format (" + key + " at the top level); starting Qwen Code once converts it"}
		}
	}
	return nil
}

// qwenEntries returns the elements of modelProviders.openai, in the file's
// order; nil when the list is not there.
func qwenEntries(settings *jsoncFile) []any {
	list, _ := settings.get("modelProviders", "openai")
	entries, _ := list.([]any)
	return entries
}

// qwenOurs is the one rule for "Ülgen's entry": an element of
// modelProviders.openai that reads the EVREN key.
func qwenOurs(entry any) bool {
	m, ok := entry.(map[string]any)
	return ok && m["envKey"] == KeyEnv
}

// qwenID returns the id of an element of modelProviders.openai, or "".
func qwenID(entry any) string {
	m, _ := entry.(map[string]any)
	id, _ := m["id"].(string)
	return id
}

// Configured is true when the settings list an entry of Ülgen's (qwenOurs).
func (Qwen) Configured(env Env) (bool, error) {
	_, settings, err := qwenRead(env)
	if err != nil {
		return false, err
	}
	for _, entry := range qwenEntries(settings) {
		if qwenOurs(entry) {
			return true, nil
		}
	}
	return false, nil
}

// qwenEntry is one model in modelProviders.openai; the field order is the
// order in the file.
type qwenEntry struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	EnvKey           string  `json:"envKey"`
	BaseURL          string  `json:"baseUrl"`
	GenerationConfig qwenGen `json:"generationConfig"`
}

// qwenGen is an entry's generationConfig. ContextWindowSize is left out when
// the model's context length is not known: Qwen Code would take a 0 as the
// window, while without the key it assumes a window of its own.
type qwenGen struct {
	ContextWindowSize     int            `json:"contextWindowSize,omitempty"`
	Modalities            qwenModalities `json:"modalities"`
	EnableRequestMetadata bool           `json:"enableRequestMetadata"`
}

// qwenModalities says what an entry's model takes besides text.
type qwenModalities struct {
	Image bool `json:"image"`
}

// qwenEntryFor is Ülgen's entry for a model.
func qwenEntryFor(m Model, root string) qwenEntry {
	return qwenEntry{
		ID: m.ID, Name: "EVREN " + m.ID, EnvKey: KeyEnv, BaseURL: root,
		GenerationConfig: qwenGen{ContextWindowSize: max(m.Context, 0), Modalities: qwenModalities{Image: m.Vision}},
	}
}

// Plan computes the changes to settings.json.
func (Qwen) Plan(env Env, req Request) (Plan, error) {
	var plan Plan
	if err := req.check(); err != nil {
		return plan, err
	}
	dir, settings, err := qwenRead(env)
	if err != nil {
		return plan, err
	}
	if settings.blank {
		if err := settings.set(qwenSettingsVersion, "$version"); err != nil {
			return plan, err
		}
	}
	// The default is judged by the entries the file has now: afterwards the
	// entry of a model EVREN retired is gone.
	choice, err := qwenDefault(settings, env, req)
	if err != nil {
		return plan, err
	}
	qwenNoteStrangers(&plan, qwenEntries(settings), req)
	if err := qwenSetModels(settings, req); err != nil {
		return plan, err
	}
	if err := qwenSetDefault(&plan, settings, req, choice); err != nil {
		return plan, err
	}
	if err := qwenSetPrivacy(&plan, settings); err != nil {
		return plan, err
	}
	if settings.doc.TrailingComma() {
		plan.note("setup.note.trailing_comma", Path(settings.path))
	}
	qwenEnvFileNote(&plan, env, dir)
	plan.note("setup.note.export", ShellHint(env.GOOS))
	settings.change(&plan)
	return plan, nil
}

// Remove undoes Plan in settings.json:
//
//   - Ülgen's entries in modelProviders.openai go (qwenOurs);
//   - the default goes when the entry Qwen Code would start with for it was
//     one of those, and with it the auth type when no entry of that type is
//     left; a default of the user's stays.
//
// privacy.usageStatisticsEnabled stays: nothing tells Ülgen's "false" from
// the user's.
func (Qwen) Remove(env Env) (Plan, error) {
	var plan Plan
	_, settings, err := qwenRead(env)
	if err != nil || settings.old == nil {
		return plan, err
	}
	if err := qwenUnset(settings); err != nil {
		return plan, err
	}
	settings.change(&plan)
	return plan, nil
}

// qwenUnset takes what is Ülgen's out of the settings (see Remove).
func qwenUnset(settings *jsoncFile) error {
	name, err := settings.text("model", "name")
	if err != nil {
		return err
	}
	base, err := settings.text("model", "baseUrl")
	if err != nil {
		return err
	}
	entry, _ := qwenResolve(settings, name, base)
	ourDefault := name != "" && qwenOurs(entry)
	hadOurs := slices.ContainsFunc(qwenEntries(settings), qwenOurs)

	list := []string{"modelProviders", "openai"}
	if err := settings.editError(settings.doc.UpdateArray(qwenOurs, nil, list...)); err != nil {
		return err
	}
	if hadOurs && len(qwenEntries(settings)) == 0 {
		if err := settings.unset(list...); err != nil {
			return err
		}
	}
	if !ourDefault {
		return nil
	}
	for _, path := range [][]string{{"model", "name"}, {"model", "baseUrl"}} {
		if err := settings.unset(path...); err != nil {
			return err
		}
	}
	authType, err := settings.text("security", "auth", "selectedType")
	if err != nil || authType != "openai" || len(qwenEntries(settings)) > 0 {
		return err
	}
	return settings.unset("security", "auth", "selectedType")
}

// qwenSetModels makes Ülgen's entries in modelProviders.openai the request's
// models. Entries are matched by id, not by position: one whose model is
// still offered stays where the user has it, byte for byte when nothing in
// it changes; one whose model is gone, or whose id an earlier entry of
// Ülgen's already has, is removed (the comments around it stay); a new model
// is appended. The user's own entries are not looked at.
//
// The editor replaces the i-th element it is told to drop by the i-th new
// value, so this takes two steps: first the entries that go, then the ones
// that stay, in the order the file has them, followed by the new ones.
func qwenSetModels(settings *jsoncFile, req Request) error {
	var order []Model // the models whose entry stays, in the file's order
	stays := map[string]bool{}
	for _, entry := range qwenEntries(settings) {
		if m, offered := req.model(qwenID(entry)); qwenOurs(entry) && offered && !stays[m.ID] {
			stays[m.ID] = true
			order = append(order, m)
		}
	}
	kept := map[string]bool{}
	goes := func(entry any) bool {
		if !qwenOurs(entry) {
			return false
		}
		id := qwenID(entry)
		if stays[id] && !kept[id] {
			kept[id] = true
			return false
		}
		return true
	}
	path := []string{"modelProviders", "openai"}
	if err := settings.editError(settings.doc.UpdateArray(goes, nil, path...)); err != nil {
		return err
	}
	for _, m := range req.Models {
		if !stays[m.ID] {
			order = append(order, m)
		}
	}
	entries := make([]any, len(order))
	for i, m := range order {
		entries[i] = qwenEntryFor(m, req.BaseURL)
	}
	return settings.editError(settings.doc.UpdateArray(qwenOurs, entries, path...))
}

// qwenChoice is Qwen Code's default model as Ülgen found it.
type qwenChoice struct {
	state defaultState
	name  string // the model it names; "" when it names none
	// vars is not empty when the default is not in the settings but in the
	// environment: the names of the variables that hold it.
	vars string
	// from is where the name stands, for a message: the settings file, or
	// the variable that names the model.
	from string
	// address is the address the default itself names: model.baseUrl in the
	// settings, OPENAI_BASE_URL in the environment. "" when it names none.
	address string
}

// qwenDefault says what Qwen Code's default model is to Ülgen.
//
// The settings have a default when they name a model (model.name) or a kind
// of sign-in (security.auth.selectedType). A model named there is judged
// when the sign-in is "openai" or not named at all: Qwen Code's "/model"
// writes the name alone for a user whose sign-in comes from the environment.
// Any other kind of sign-in makes the default the user's.
//
// Settings that name no model, and no other sign-in than "openai", leave
// the model to the environment: with OPENAI_BASE_URL or one of qwenModelVars
// set, the user has a default outside the file. It is the user's, and it is
// judged like one in the file for the one thing that matters there, the
// clash (qwenJudge). With none of them set, a kind of sign-in alone is still
// a choice of the user's, and settings with neither have no default.
func qwenDefault(settings *jsoncFile, env Env, req Request) (qwenChoice, error) {
	authType, err := settings.text("security", "auth", "selectedType")
	if err != nil {
		return qwenChoice{}, err
	}
	name, err := settings.text("model", "name")
	if err != nil {
		return qwenChoice{}, err
	}
	base, err := settings.text("model", "baseUrl")
	if err != nil {
		return qwenChoice{}, err
	}
	choice := qwenChoice{state: defaultForeign, name: name, from: settings.path, address: base}
	switch {
	case authType != "" && authType != "openai":
		return choice, nil
	case name != "":
		return qwenJudge(settings, req, choice)
	}
	if choice = qwenEnvDefault(env); choice.vars != "" {
		return qwenJudge(settings, req, choice)
	}
	if authType == "" {
		choice.state = defaultNone
	}
	return choice, nil
}

// qwenEnvDefault returns the default the environment holds: the model the
// first of qwenModelVars names, and the names of the variables that are set
// (OPENAI_BASE_URL among them). Without any of them vars is empty. The
// variable with the key is not looked at.
func qwenEnvDefault(env Env) qwenChoice {
	choice := qwenChoice{state: defaultForeign, address: env.getenv("OPENAI_BASE_URL")}
	var set []string
	if choice.address != "" {
		set = append(set, "OPENAI_BASE_URL")
	}
	for _, name := range qwenModelVars {
		if value := env.getenv(name); value != "" {
			set = append(set, name)
			if choice.name == "" {
				choice.name, choice.from = value, name
			}
		}
	}
	choice.vars = strings.Join(set, ", ")
	return choice
}

// qwenJudge says whether a default that names a model is EVREN's. It is
// when the entry Qwen Code starts with for the name is one of Ülgen's
// (qwenResolve): then it is a model EVREN offers, or one that is gone. A
// default from the environment is never EVREN's.
//
// A default whose name no list holds will be answered by the entry Ülgen is
// about to write for that model: Qwen Code finds a default by its name, and
// takes the entry's address before the one in the environment. When the
// address the default names is EVREN's, nothing is taken from anyone: a
// default in the settings was meant for EVREN and counts as EVREN's, and one
// in the environment goes to EVREN as before. Otherwise the default is the
// user's, and writing the entry would be a take-over nobody asked for (with
// the user's other key sent to EVREN when EVREN_API_KEY is not set): it is
// refused (*DefaultClashError). With --default the take-over was asked for.
func qwenJudge(settings *jsoncFile, req Request, choice qwenChoice) (qwenChoice, error) {
	inFile := choice.vars == ""
	base := "" // Qwen Code picks an entry by its address only for a default of the settings
	if inFile {
		base = choice.address
	}
	entry, held := qwenResolve(settings, choice.name, base)
	_, offered := req.model(choice.name)
	switch {
	case inFile && qwenOurs(entry) && offered:
		choice.state = defaultEvren
	case inFile && qwenOurs(entry):
		choice.state = defaultRetired
	case held || !offered:
	case choice.address != "" && sameURL(choice.address, req.BaseURL):
		if inFile {
			choice.state = defaultEvren
		}
	case !req.MakeDefault && inFile:
		return choice, &DefaultClashError{Path: choice.from, Model: choice.name}
	case !req.MakeDefault:
		return choice, &DefaultClashError{Var: choice.from, Model: choice.name}
	}
	return choice, nil
}

// qwenResolve returns the entry Qwen Code starts with when model.name is
// name and model.baseUrl is base, and whether any list holds an entry with
// that id. Qwen Code takes the entry with the id whose baseUrl is base, when
// base is not empty and there is such an entry, and the first entry with the
// id otherwise (packages/cli/src/utils/modelConfigUtils.ts). An empty
// model.baseUrl is what its own "/model" command writes.
//
// Qwen Code looks through every list of modelProviders that speaks the
// OpenAI protocol, in the file's order, and Ülgen only sees
// modelProviders.openai. So when another list that providerProtocol maps to
// "openai" has an entry with the id, the entry is nil and held is true: the
// id is taken, and Ülgen cannot say by which entry.
func qwenResolve(settings *jsoncFile, name, base string) (entry any, held bool) {
	protocols, _ := settings.get("providerProtocol")
	lists, _ := settings.get("modelProviders")
	for provider, protocol := range asObject(protocols) {
		entries, _ := asObject(lists)[provider].([]any)
		for _, other := range entries {
			if protocol == "openai" && provider != "openai" && qwenID(other) == name {
				return nil, true
			}
		}
	}
	for _, candidate := range qwenEntries(settings) {
		if qwenID(candidate) != name {
			continue
		}
		if m, _ := candidate.(map[string]any); base != "" && m["baseUrl"] == base {
			return candidate, true
		}
		if entry == nil {
			entry = candidate
		}
	}
	return entry, entry != nil
}

// qwenNoteStrangers adds a note for each entry that reads the EVREN key and
// is about to be removed although it does not look like one Ülgen wrote: its
// model is not one of EVREN's, and its address is neither the request's nor
// that of an entry of Ülgen's that stays. Most likely the user copied an
// entry for another service and left the variable's name as it was. The rule
// for "ours" still removes it; the note says so, and what to change.
func qwenNoteStrangers(plan *Plan, entries []any, req Request) {
	roots := []string{req.BaseURL}
	for _, entry := range entries {
		if _, offered := req.model(qwenID(entry)); qwenOurs(entry) && offered {
			roots = append(roots, qwenBase(entry))
		}
	}
	for _, entry := range entries {
		_, offered := req.model(qwenID(entry))
		base := qwenBase(entry)
		known := slices.ContainsFunc(roots, func(root string) bool { return sameURL(root, base) })
		if qwenOurs(entry) && !offered && !known {
			plan.note("setup.note.qwen_entry_removed", qwenID(entry), qwenShownAddress(base))
		}
	}
}

// qwenBase returns the baseUrl of an element of modelProviders.openai, or "".
func qwenBase(entry any) string {
	base, _ := asObject(entry)["baseUrl"].(string)
	return base
}

// qwenShownAddress returns an address from the user's file as a note may
// show it: without a user name and password, a query or a fragment, any of
// which can hold a secret. An address that does not parse is not shown.
func qwenShownAddress(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// asObject returns a decoded JSON value as an object; nil when it is none.
func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// qwenSetDefault applies the rule for the default model (changesDefault).
// The default is three keys that are written together:
// security.auth.selectedType, model.name and model.baseUrl.
//
// A default in the environment is the user's: it is not written over
// without --default, and a note says where it comes from.
//
// A default that is EVREN's and stays keeps pointing at Ülgen's entry: when
// its model.baseUrl names another address than the one the entries have now
// (setup ran with another --base-url before), it follows them. Left alone,
// Qwen Code would warn at every start and fall back to the first entry with
// that id, which can be the user's.
func qwenSetDefault(plan *Plan, settings *jsoncFile, req Request, choice qwenChoice) error {
	if choice.vars != "" && !req.MakeDefault {
		plan.note("setup.note.qwen_env_default", choice.vars, Qwen{}.ID())
		return nil
	}
	plan.defaultNotes(req, choice.state, choice.name, Qwen{}.Name(), Qwen{}.ID())
	if !req.changesDefault(choice.state) {
		base, err := settings.text("model", "baseUrl")
		if err != nil || choice.state != defaultEvren || base == "" || base == req.BaseURL {
			return err
		}
		return settings.set(req.BaseURL, "model", "baseUrl")
	}
	if err := settings.set("openai", "security", "auth", "selectedType"); err != nil {
		return err
	}
	if err := settings.set(req.Default, "model", "name"); err != nil {
		return err
	}
	return settings.set(req.BaseURL, "model", "baseUrl")
}

// qwenSetPrivacy switches usage statistics off when the user has not said
// anything about them: they go to a third party. A value that is there, of
// any kind, is the user's and stays.
func qwenSetPrivacy(plan *Plan, settings *jsoncFile) error {
	if _, has := settings.get("privacy", "usageStatisticsEnabled"); has {
		return nil
	}
	plan.note("setup.note.stats_off")
	return settings.set(false, "privacy", "usageStatisticsEnabled")
}

// qwenEnvFileNote adds a note when a .env file sets QWEN_HOME. Without the
// variable in its environment, Qwen Code reads <home>/.qwen/.env and then
// <home>/.env for it before it looks for its directory, and takes the first
// value it finds; it would then use another directory than the one Ülgen
// wrote to. The note names that first file. The variable in the environment
// wins over both files, so with QWEN_HOME set there is nothing to say. Only
// the name of the variable is looked for, and nothing of a file goes into the
// note: such a file can hold keys.
func qwenEnvFileNote(plan *Plan, env Env, dir string) {
	if env.getenv("QWEN_HOME") != "" {
		return
	}
	for _, path := range []string{filepath.Join(dir, ".env"), filepath.Join(filepath.Dir(dir), ".env")} {
		content, err := os.ReadFile(path)
		if err != nil {
			continue // no such file, or one that cannot be read: nothing is known
		}
		for _, line := range strings.Split(string(content), "\n") {
			if dotenvSets(line, "QWEN_HOME") {
				plan.note("setup.note.qwen_home_file", Path(path))
				return
			}
		}
	}
}

// dotenvSets reports whether a line of a .env file gives the variable name
// a value that is not empty: "NAME=value", "NAME: value" or either behind
// "export", with or without quotes around the value.
func dotenvSets(line, name string) bool {
	line = strings.TrimSpace(line)
	if rest, exported := strings.CutPrefix(line, "export"); exported && rest != strings.TrimLeft(rest, " \t") {
		line = strings.TrimSpace(rest)
	}
	rest, found := strings.CutPrefix(line, name)
	if !found {
		return false
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, ":") {
		return false
	}
	value, _, _ := strings.Cut(rest[1:], " #")
	return strings.Trim(strings.TrimSpace(value), "\"'`") != ""
}
