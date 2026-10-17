package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
	evrenworkers "github.com/mftnakrsu/ulgen/plugins/claude-code/evren-workers"
)

// claudeSkill returns the EVREN workers skill compiled into this build, or
// nil. "ulgen setup claude" installs it into the user's Claude directory.
// Tests replace it.
var claudeSkill = evrenworkers.Skill

// modelIDPattern is what a model id from the API must look like before it is
// written into another program's configuration file: letters, digits and
// ". _ : / -", at most 100 characters.
var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,100}$`)

// failure ends a command: the message to print, already translated ("" when
// it was printed where it happened), the exit code, and whether the usage
// text follows the message.
type failure struct {
	msg   string
	code  int
	usage bool
}

// Error returns the message, so a failure can travel as an error.
func (f *failure) Error() string { return f.msg }

// usageMistake is the failure for a mistake on the command line.
func usageMistake(msg string) *failure {
	return &failure{msg: msg, code: exitUsage, usage: true}
}

// cancelled is the failure for a run that Ctrl+C ended.
func cancelled(tr *i18n.Translator) *failure {
	return &failure{msg: tr.T("error.cancelled"), code: exitInterrupted}
}

// report prints a failure on stderr and returns its exit code.
func (f *failure) report(stderr io.Writer, usage string) int {
	if f.msg != "" {
		fmt.Fprintln(stderr, "ulgen: "+f.msg)
	}
	if f.usage {
		io.WriteString(stderr, usage)
	}
	return f.code
}

// setupOptions is what the command line of "ulgen setup" asks for.
type setupOptions struct {
	names       []string // the tools, or the single word "all"
	model       string   // --model
	makeDefault bool     // --default
	remove      bool     // --remove
	dryRun      bool     // --dry-run
	baseURL     string   // --base-url
}

// setupFlags declares the options of "ulgen setup" on a flag set that fills
// opts.
func setupFlags(opts *setupOptions, tr *i18n.Translator) *flag.FlagSet {
	fs := flag.NewFlagSet("ulgen setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.model, "model", "", tr.T("flag.setup_model", provider.EVREN.DefaultModel))
	fs.BoolVar(&opts.makeDefault, "default", false, tr.T("flag.setup_default"))
	fs.BoolVar(&opts.dryRun, "dry-run", false, tr.T("flag.setup_dry_run"))
	fs.BoolVar(&opts.remove, "remove", false, tr.T("flag.setup_remove"))
	fs.StringVar(&opts.baseURL, "base-url", "", tr.T("flag.evren_base_url"))
	return fs
}

// parseSetupArgs reads the command line into opts and checks what can be
// checked without looking at the machine. It returns flag.ErrHelp for -h and
// a *failure for a mistake.
func parseSetupArgs(fs *flag.FlagSet, opts *setupOptions, args []string, tr *i18n.Translator) error {
	names, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	if err != nil {
		return usageMistake(translateFlagError(err, tr))
	}
	opts.names = names
	if checkBaseURL(opts.baseURL) != nil {
		return usageMistake(tr.T("error.base_url_unsafe"))
	}
	// A removal takes nothing from EVREN and writes no choice: the options
	// that say what to write have no meaning next to it.
	if opts.remove && (opts.model != "" || opts.makeDefault || opts.baseURL != "") {
		return usageMistake(tr.T("setup.remove_alone"))
	}
	// An option without a tool would be dropped without a word, and
	// "--model qwen" would take the tool's name for the model.
	if len(names) == 0 && fs.NFlag() > 0 {
		return usageMistake(tr.T("setup.options_need_tool", strings.Join(setup.IDs(), ", ")))
	}
	return nil
}

// parseInterleaved parses options that may stand before, between and after
// the positional words, which the flag package alone does not allow. A "--"
// ends the options once; a later "--" is a word like any other.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return words, nil
		}
		words = append(words, rest[0])
		args = rest[1:]
	}
}

// runSetup lists the tools or sets them up: "ulgen setup [tool... | all] [options]".
func runSetup(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	var opts setupOptions
	fs := setupFlags(&opts, tr)
	usage := tr.T("setup.usage", strings.Join(setup.IDs(), ", ")) + "\n" + optionRows(tr, fs)
	err := parseSetupArgs(fs, &opts, args, tr)
	if errors.Is(err, flag.ErrHelp) {
		io.WriteString(stdout, usage)
		return exitOK
	}
	var mistake *failure
	if errors.As(err, &mistake) {
		return mistake.report(stderr, usage)
	}
	env, err := checkedEnv(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeSetupError(err, env.Home, tr))
		return exitError
	}
	s := &setupRun{ctx: ctx, stdout: stdout, stderr: stderr, env: env, dryRun: opts.dryRun, remove: opts.remove, getenv: getenv, tr: tr}
	if len(opts.names) == 0 {
		return s.list()
	}
	if f := s.run(opts, getenv); f != nil {
		return f.report(stderr, usage)
	}
	return exitOK
}

// run sets the named tools up: which tools, the key and the models, where
// the backups go, then one tool after the other.
func (s *setupRun) run(opts setupOptions, getenv func(string) string) *failure {
	names := opts.names
	if s.remove && len(names) == 1 && names[0] == "all" {
		names = setup.IDs()
	}
	tools, f := s.selectTools(names)
	if f != nil {
		return f
	}
	// A removal needs neither the key nor EVREN's models. Its "all" is every
	// tool, installed or not: a tool's files can outlive it.
	if !s.remove {
		if f := s.request(opts, getenv); f != nil {
			return f
		}
	}
	backups, err := backupDir(getenv)
	if err != nil {
		msg, ok := describeEnvError(err, s.tr)
		if !ok {
			msg = s.tr.T("setup.err.backup_dir", oneLine(err.Error()))
		}
		return &failure{msg: msg, code: exitError}
	}
	s.backups = backups
	return s.setUpAll(tools)
}

// candidate is a tool to set up. For "all" the tool was already looked for:
// found holds the result, and skip says it is not installed.
type candidate struct {
	tool     setup.Tool
	detected bool
	found    setup.Detection
	skip     bool
}

// selectTools turns the words on the command line into the tools to set up.
// Named tools are set up whether they are installed or not. "all" means the
// supported tools that are installed, so they are looked for here, before
// the key is used for anything: with none installed nothing is sent.
func (s *setupRun) selectTools(names []string) ([]candidate, *failure) {
	if len(names) == 1 && names[0] == "all" {
		return s.installedTools()
	}
	var out []candidate
	seen := map[string]bool{}
	for _, name := range names {
		if name == "all" {
			return nil, usageMistake(s.tr.T("setup.all_alone"))
		}
		tool, ok := setup.Lookup(name)
		if !ok {
			return nil, usageMistake(s.tr.T("setup.unknown_tool", safeArg(name), strings.Join(setup.IDs(), ", ")))
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, candidate{tool: tool})
		}
	}
	return out, nil
}

// installedTools looks for every supported tool. The ones that are not
// installed stay in the list, marked to be skipped, so the report names
// them in their place.
func (s *setupRun) installedTools() ([]candidate, *failure) {
	var out []candidate
	installed := 0
	for _, tool := range setup.Tools() {
		if s.ctx.Err() != nil {
			return nil, cancelled(s.tr)
		}
		found := tool.Detect(s.ctx, s.env)
		if found.Installed {
			installed++
		}
		out = append(out, candidate{tool: tool, detected: true, found: found, skip: !found.Installed})
	}
	if s.ctx.Err() != nil {
		return nil, cancelled(s.tr)
	}
	if installed == 0 {
		return nil, &failure{msg: s.tr.T("setup.none_installed", strings.Join(setup.IDs(), ", ")), code: exitError}
	}
	return out, nil
}

// request resolves the key, asks EVREN for its models and puts together
// what every tool is set up with: s.req and s.key.
func (s *setupRun) request(opts setupOptions, getenv func(string) string) *failure {
	cred, code := resolveKey(s.stderr, getenv, s.tr)
	if code != exitOK {
		return &failure{code: code}
	}
	// With --base-url pointing at another host the tools are set up to send
	// the key there, so say it before anything is sent.
	if host := baseURLNotice(opts.baseURL); host != "" {
		fmt.Fprintln(s.stderr, s.tr.T("setup.base_url_note", host))
	}
	models, skipped, err := fetchModels(s.ctx, evren.New(cred.Secret, opts.baseURL, userAgent()))
	if err != nil {
		f := &failure{msg: describeEvrenError(err, s.tr, s.tr.T("status.key_invalid"), s.tr.T("status.terms_missing")), code: exitError}
		if errors.Is(err, context.Canceled) {
			f.code = exitInterrupted
		}
		return f
	}
	if skipped > 0 {
		fmt.Fprintln(s.stderr, "ulgen: "+plural(s.tr, "setup.models_skipped", skipped))
	}
	if len(models) == 0 {
		return &failure{msg: s.tr.T("setup.no_models"), code: exitError}
	}
	def, given, f := chooseDefault(models, opts.model, s.tr)
	if f != nil {
		return f
	}
	s.key = cred.Secret
	s.req = setup.Request{
		BaseURL: provider.EVREN.BaseURL, Models: models, Default: def, ModelGiven: given,
		MakeDefault: opts.makeDefault,
		ClaudeSkill: claudeSkill(),
	}
	if opts.baseURL != "" {
		s.req.BaseURL = opts.baseURL
	}
	return nil
}

// fetchModels asks EVREN for its models and returns the ones the tools can
// be given, with the number of models that were left out as unusable.
func fetchModels(ctx context.Context, client *evren.Client) ([]setup.Model, int, error) {
	listed, err := client.Models(ctx)
	if err != nil {
		return nil, 0, err
	}
	models, skipped := usableModels(listed)
	return models, skipped, nil
}

// usableModels keeps the chat models with tool support, in the API's order.
// What it keeps is written into other programs' configuration files, so a
// model is left out, and counted, when its id has unusual characters, when
// its context size is missing or not positive, or when its id was already
// seen (a configuration file with the same key twice means different things
// to different readers). A negative output limit counts as none.
func usableModels(listed []evren.Model) (models []setup.Model, skipped int) {
	seen := map[string]bool{}
	for _, m := range listed {
		if !evren.IsChat(m) || !m.Tools {
			continue
		}
		if !modelIDPattern.MatchString(m.ID) || m.ContextLength <= 0 || seen[m.ID] {
			skipped++
			continue
		}
		seen[m.ID] = true
		models = append(models, setup.Model{
			ID: m.ID, Context: m.ContextLength, MaxOutput: max(m.MaxOutput, 0), Vision: m.Vision,
			Effort: m.Effort, EffortValues: m.EffortValues,
		})
	}
	return models, skipped
}

// chooseDefault picks the model the tools select: the one --model names,
// which must be on the list, else Ülgen's own default when EVREN offers it,
// else the first model. given says that --model decided.
func chooseDefault(models []setup.Model, wanted string, tr *i18n.Translator) (id string, given bool, f *failure) {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	switch {
	case wanted != "" && !slices.Contains(ids, wanted):
		return "", false, usageMistake(tr.T("setup.unknown_model", safeArg(wanted), strings.Join(ids, ", ")))
	case wanted != "":
		return wanted, true, nil
	case slices.Contains(ids, provider.EVREN.DefaultModel):
		return provider.EVREN.DefaultModel, false, nil
	}
	return ids[0], false, nil
}

// plural picks the message for a count: key+".one" for exactly one and
// key+".other" for the rest. Languages that do not inflect after a number,
// like Turkish, hold the same sentence under both keys.
func plural(tr *i18n.Translator, key string, n int) string {
	if n == 1 {
		return tr.T(key+".one", n)
	}
	return tr.T(key+".other", n)
}
