package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup/jsonc"
)

const testRoot = "https://evren-llmapi.ssyz.org.tr/v1"

// testBin is where the test machine keeps its programs: an absolute path on
// the system the tests run on (/usr/bin, or C:\usr\bin on Windows), because
// a detected binary must be absolute.
func testBin(name string) string {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	return filepath.Join(root, "usr", "bin", name)
}

// testEnv is a machine with an empty home directory and nothing installed.
// vars are environment variables; bins maps a binary name (or path) to what
// "<binary> --version" prints.
func testEnv(t *testing.T, vars, bins map[string]string) Env {
	t.Helper()
	return Env{
		GOOS:   "linux",
		Home:   t.TempDir(),
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(name string) (string, error) {
			if _, ok := bins[name]; ok {
				if filepath.IsAbs(name) {
					return name, nil
				}
				return testBin(name), nil
			}
			return "", errors.New("not found")
		},
		Version: func(_ context.Context, binary string) (string, error) {
			for name, out := range bins {
				if binary == name || binary == testBin(name) {
					return out, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

// testRequest is a setup request with three of EVREN's models.
func testRequest() Request {
	return Request{
		BaseURL: testRoot,
		Models: []Model{
			{ID: "deepseek-v4-flash", Context: 1048576, MaxOutput: 16384},
			{ID: "qwen3-vl-30b", Context: 262144, MaxOutput: 16384, Vision: true},
			{ID: "deepseek-v4.1-flash", Context: 1048576, MaxOutput: 65536, Vision: true},
		},
		Default: "deepseek-v4.1-flash",
	}
}

// write creates a file under the test home and returns its path.
func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil { // whatever the umask is
		t.Fatal(err)
	}
	return path
}

// planned returns the new content the plan has for path, or fails.
func planned(t *testing.T, p Plan, path string) string {
	t.Helper()
	for _, f := range p.Files {
		if f.Path == path {
			return string(f.New)
		}
	}
	t.Fatalf("the plan does not change %s; it changes %v", path, paths(p))
	return ""
}

func paths(p Plan) []string {
	var out []string
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

func hasNote(p Plan, key string) bool {
	for _, n := range p.Notes {
		if n.Key == key {
			return true
		}
	}
	return false
}

// applyAndReplan applies the plan and plans again; a second plan must have
// nothing left to write.
func applyAndReplan(t *testing.T, tool Tool, env Env, req Request, p Plan) {
	t.Helper()
	// A directory of its own each time: a test may apply two plans to the
	// same file, and Apply never overwrites a backup.
	if err := Apply(p, tool.ID(), filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	again, err := tool.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 {
		t.Errorf("a second plan still changes %v:\n%s", paths(again), again.Files[0].New)
	}
}

// decoded returns what a JSON text with comments means to the tool that
// reads it. The tests of the adapters compare these values, not the text.
func decoded(t *testing.T, text string) map[string]any {
	t.Helper()
	doc, err := jsonc.Parse([]byte(text))
	if err != nil {
		t.Fatalf("the text does not parse: %v\n%s", err, text)
	}
	value, _ := doc.Get()
	object, _ := value.(map[string]any)
	return object
}

// dig returns the value at a path of object keys in a decoded value, or nil.
func dig(value any, path ...string) any {
	for _, key := range path {
		object, _ := value.(map[string]any)
		value = object[key]
	}
	return value
}

// plannedDoc returns what the plan's new content for path decodes to.
func plannedDoc(t *testing.T, p Plan, path string) map[string]any {
	t.Helper()
	return decoded(t, planned(t, p, path))
}

// setUp plans a tool's setup, applies it and checks that a second plan has
// nothing left to write. It returns the first plan.
func setUp(t *testing.T, tool Tool, env Env, req Request) Plan {
	t.Helper()
	p, err := tool.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	applyAndReplan(t, tool, env, req, p)
	return p
}

// noteArgs returns the arguments of the plan's note with the given key.
func noteArgs(t *testing.T, p Plan, key string) []any {
	t.Helper()
	for _, n := range p.Notes {
		if n.Key == key {
			return n.Args
		}
	}
	t.Fatalf("the plan has no note %s; its notes are %v", key, p.Notes)
	return nil
}

// unchanged fails the test when the file at path is no longer content.
func unchanged(t *testing.T, path, content string) {
	t.Helper()
	if got, err := os.ReadFile(path); err != nil || string(got) != content {
		t.Errorf("%s was changed (%v):\n%s", path, err, got)
	}
}

// A request no adapter can turn into a file is refused before any file is
// read: the file each adapter would read first does not parse here, and the
// error is still the one about the request.
func TestTheRequestIsChecked(t *testing.T) {
	for name, change := range map[string]func(*Request){
		"no models":                    func(r *Request) { r.Models = nil },
		"a repeated id":                func(r *Request) { r.Models = append(r.Models, r.Models[0]) },
		"a model without an id":        func(r *Request) { r.Models = append(r.Models, Model{Context: 1}) },
		"a default outside the models": func(r *Request) { r.Default = "nope" },
		"no default":                   func(r *Request) { r.Default = "" },
		"no API root":                  func(r *Request) { r.BaseURL = "" },
		"an API root that is not text": func(r *Request) { r.BaseURL = "https://x.example/\xff" },
		"a line break in the API root": func(r *Request) { r.BaseURL = "https://x.example/v1\n" },
	} {
		for _, tool := range Tools() {
			env := testEnv(t, nil, nil)
			// Only the adapters that read a file have one to break; Claude
			// Code reads nothing, so it is refused by the request alone.
			if broken := map[string]string{
				"qwen":     qwenSettings(env),
				"opencode": filepath.Join(opencodeHome(env), "opencode.json"),
				"codex":    filepath.Join(env.Home, ".codex", "config.toml"),
			}[tool.ID()]; broken != "" {
				write(t, broken, "{ not a file any parser takes")
			}
			req := testRequest()
			change(&req)
			p, err := tool.Plan(env, req)
			var re *RequestError
			if !errors.As(err, &re) || re.Problem == "" || len(p.Files) != 0 {
				t.Errorf("%s, %s: Plan = %v, %T %v", tool.ID(), name, paths(p), err, err)
			}
		}
	}
	if err := testRequest().check(); err != nil {
		t.Errorf("a good request: %v", err)
	}
	if got := (&RequestError{Problem: "it has no models"}).Error(); got != "the setup request is not usable: it has no models" {
		t.Errorf("RequestError: %q", got)
	}
}

// The adapters learn the name of the key's variable, never the key: nothing
// asks the environment for EVREN_API_KEY.
func TestNoAdapterAsksForTheKey(t *testing.T) {
	// Claude Code is "set up" only when it is installed and its skill is
	// there, so the machine here has the binary and every request carries the
	// skill text.
	env := testEnv(t, nil, map[string]string{"claude": "1.0.3\n"})
	asked := map[string]bool{}
	env.Getenv = func(name string) string {
		asked[name] = true
		if name == KeyEnv {
			t.Errorf("the environment was asked for %s", name)
		}
		return ""
	}
	req := testRequest()
	req.ClaudeSkill = claudeTestSkill()
	for _, tool := range Tools() {
		tool.Detect(context.Background(), env)
		if _, err := tool.Configured(env); err != nil {
			t.Errorf("%s: Configured: %v", tool.ID(), err)
		}
		setUp(t, tool, env, req)
		if ok, err := tool.Configured(env); !ok || err != nil {
			t.Errorf("%s: Configured after setup = %v, %v", tool.ID(), ok, err)
		}
	}
	for _, name := range []string{"QWEN_HOME", "OPENCODE_CONFIG_DIR", "XDG_CONFIG_HOME", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		if !asked[name] {
			t.Errorf("%s was never read; is the test still looking at the adapters?", name)
		}
	}
}

// A file that does not parse is reported with the place and the kind of
// problem and without its text: what does not parse can be a key whose
// quotes were forgotten, or one with a bad escape in it.
func TestNoTextOfAFileInAParseError(t *testing.T) {
	const secret = "sk-SENTINEL-4242"
	for name, src := range map[string]string{
		"without quotes": "{\n  \"env\": {\"EVREN_API_KEY\": " + secret + "}\n}\n",
		"a bad escape":   "{\n  \"env\": {\"EVREN_API_KEY\": \"sk-SENTINEL-\\q4242\"}\n}\n",
		"behind a value": "{\n  \"env\": {\"EVREN_API_KEY\": \"x\" " + secret + "}\n}\n",
	} {
		env := testEnv(t, nil, nil)
		settings := write(t, qwenSettings(env), src)
		config := write(t, filepath.Join(opencodeHome(env), "opencode.json"), src)
		lower := write(t, filepath.Join(opencodeHome(env), "config.json"), src)
		_, qwenPlan := Qwen{}.Plan(env, testRequest())
		_, qwenConfigured := Qwen{}.Configured(env)
		for call, err := range map[string]error{"Qwen.Plan": qwenPlan, "Qwen.Configured": qwenConfigured} {
			checkParseError(t, name+", "+call, err, settings)
		}
		// opencode reads config.json first; with it out of the way the file
		// Ülgen writes is the one that does not parse.
		for _, path := range []string{lower, config} {
			_, plan := Opencode{}.Plan(env, opencodeRequest())
			_, configured := Opencode{}.Configured(env)
			for call, err := range map[string]error{"Opencode.Plan": plan, "Opencode.Configured": configured} {
				checkParseError(t, name+", "+call, err, path)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		unchanged(t, settings, src)
	}
}

// checkParseError fails unless err is a *ParseError for path that names a
// line and a column and holds nothing of the sentinel the tests plant.
func checkParseError(t *testing.T, name string, err error, path string) {
	t.Helper()
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Path != path || pe.Format != "JSON" {
		t.Errorf("%s: %T %v", name, err, err)
		return
	}
	if !strings.Contains(pe.Detail, "line 2, column ") || strings.Contains(err.Error(), "SENTINEL") || strings.Contains(err.Error(), "4242") {
		t.Errorf("%s: %v", name, err)
	}
}

// Every note the Qwen Code and opencode adapters give has a message in both
// languages, and the message takes the note's arguments. The keys are also
// read from the adapters' source, so a note that no case here produces
// fails the test.
func TestEveryNoteIsTranslated(t *testing.T) {
	var notes []Note
	plan := func(tool Tool, env Env, req Request) {
		t.Helper()
		p, err := tool.Plan(env, req)
		if err != nil {
			t.Fatal(err)
		}
		notes = append(notes, p.Notes...)
	}
	// Qwen Code: a fresh machine; the user's own default and a trailing
	// comma; a default whose model is gone.
	plan(Qwen{}, testEnv(t, nil, nil), testRequest())
	env := testEnv(t, nil, nil)
	write(t, qwenSettings(env), "{\n  \"security\": { \"auth\": { \"selectedType\": \"anthropic\" } },\n}\n")
	write(t, filepath.Join(env.Home, ".qwen", ".env"), "QWEN_HOME=/srv/qwen\n")
	plan(Qwen{}, env, testRequest())
	env = testEnv(t, nil, nil)
	write(t, qwenSettings(env), `{"modelProviders": {"openai": [{"id": "gone", "envKey": "EVREN_API_KEY"}]}, "security": {"auth": {"selectedType": "openai"}}, "model": {"name": "gone"}}`)
	plan(Qwen{}, env, testRequest())
	// opencode: a copy beneath the file, defaults that are gone or kept,
	// and everything that defeats the setup.
	env = testEnv(t, map[string]string{"OPENCODE_CONFIG": filepath.Join(t.TempDir(), "extra.json")}, nil)
	write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{"provider": {"evren": `+opencodeStale+`}, "small_model": "evren/gone", "disabled_providers": ["evren"], "enabled_providers": ["other"], "providers": {"evren": {}}}`)
	write(t, filepath.Join(opencodeHome(env), "opencode.jsonc"), `{"model": "anthropic/claude"}`)
	plan(Opencode{}, env, opencodeRequest())
	// A default in the environment; files of ~/.opencode, and what is taken
	// out of an entry.
	env = testEnv(t, map[string]string{"OPENAI_BASE_URL": "https://api.other.example/v1"}, nil)
	plan(Qwen{}, env, testRequest())
	env = testEnv(t, nil, nil)
	write(t, filepath.Join(env.Home, ".opencode", "opencode.json"), `{"model": "evren/gone", "provider": {"evren": `+opencodeStale+`}}`)
	write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{"provider": {"evren": {"env": ["EVREN_API_KEY"], "options": {"apiKey": "invented-value"}, "models": {"gone": {}}}}}`)
	plan(Opencode{}, env, opencodeRequest())
	// A removal that leaves a copy of the entry in a file Ülgen does not write.
	removal, err := Opencode{}.Remove(env)
	if err != nil {
		t.Fatal(err)
	}
	notes = append(notes, removal.Notes...)
	ctx := context.Background()
	notes = append(notes, Qwen{}.Detect(ctx, testEnv(t, nil, nil)).Notes...)
	notes = append(notes, Qwen{}.Detect(ctx, testEnv(t, nil, map[string]string{"node": "v18.20.4\n"})).Notes...)
	notes = append(notes, Qwen{}.InstallHint(Env{}), Opencode{}.InstallHint(Env{}))

	seen := map[string]bool{}
	for _, n := range notes {
		seen[n.Key] = true
		english := i18n.New("en").T(n.Key, n.Args...)
		for _, lang := range i18n.Supported() {
			msg := i18n.New(lang).T(n.Key, n.Args...)
			if msg == n.Key || strings.Contains(msg, "%!") || (lang != "en" && msg == english) {
				t.Errorf("%s in %s with %v: %q", n.Key, lang, n.Args, msg)
			}
		}
	}
	keys := regexp.MustCompile(`"(setup\.(?:note|install)\.[a-z_.]+)"`)
	for _, file := range []string{"qwen.go", "opencode.go", "setup.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keys.FindAllStringSubmatch(string(source), -1) {
			if !seen[m[1]] {
				t.Errorf("%s gives the note %s, which no case of this test produces", file, m[1])
			}
		}
	}
}

func TestReadJSONC(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		content string
		blank   bool
	}{
		"empty":              {"", true},
		"whitespace":         {"  \n\t\r\n", true},
		"a line comment":     {"// mine\n", true},
		"a comment, no end":  {"// mine", true},
		"a block comment":    {"/* nothing yet */", true},
		"a byte-order mark":  {"\xef\xbb\xbf", true},
		"a mark and comment": {"\xef\xbb\xbf// mine\r\n", true},
		"an empty object":    {"{}", false},
		"an object":          {"{\"a\": 1}\n", false},
	} {
		path := write(t, filepath.Join(dir, name+".json"), tc.content)
		file, err := readJSONC(path)
		if err != nil || file.blank != tc.blank || string(file.old) != tc.content || file.old == nil || file.path != path {
			t.Errorf("%s: %+v, %v", name, file, err)
			continue
		}
		if got := string(file.doc.Bytes()); got != tc.content {
			t.Errorf("%s: the document reads %q", name, got)
		}
	}
	// No file: a new document, and no error.
	file, err := readJSONC(filepath.Join(dir, "none.json"))
	if err != nil || !file.blank || file.old != nil || string(file.doc.Bytes()) != "{}\n" {
		t.Errorf("no file: %+v, %v", file, err)
	}
	// A directory in the file's place.
	var ke *KindError
	if _, err := readJSONC(dir); !errors.As(err, &ke) || ke.Path != dir || ke.WantDir || ke.Var != "" {
		t.Errorf("a directory: %T %v", err, err)
	}
	// A file that does not parse, and a top-level value that is no object.
	for _, content := range []string{"{ \"a\": }", "[]", "5"} {
		path := write(t, filepath.Join(dir, "bad.json"), content)
		var pe *ParseError
		if _, err := readJSONC(path); !errors.As(err, &pe) || pe.Path != path || pe.Format != "JSON" {
			t.Errorf("%q: %T %v", content, err, err)
		}
	}
	// A file with a value that cannot be decoded as a whole is refused as a
	// *ShapeError: no adapter may plan on a document it cannot read.
	for name, content := range map[string]string{
		"a number too large":          `{"a": 1e400}`,
		"the same inside an array":    `{"modelProviders": {"openai": [{"envKey": "EVREN_API_KEY", "n": 1e400}]}}`,
		"values nested too deeply":    strings.Repeat(`{"a":`, 10001) + "1" + strings.Repeat("}", 10001),
		"the same in a nested object": `{"a": {"b": {"c": 1e400}}}`,
	} {
		path := write(t, filepath.Join(dir, name+".json"), content)
		var se *ShapeError
		if _, err := readJSONC(path); !errors.As(err, &se) || se.Path != path {
			t.Errorf("%s: %T %v", name, err, err)
		}
		if got := content2(t, path); got != content {
			t.Errorf("%s: the file changed:\n%q", name, got)
		}
	}
	// A path that cannot be looked at for another reason than "is not there".
	if runtime.GOOS != "windows" { // which reports a file in the path as "does not exist"
		inside := filepath.Join(write(t, filepath.Join(dir, "a-file"), "x"), "settings.json")
		if file, err := readJSONC(inside); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Errorf("a file as the parent: %+v, %v", file, err)
		}
	}
}

// content2 is the content of a file, read for the shape tests above.
func content2(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A6: an adapter never plans on a document it cannot read completely. A value
// that does not decode (a number too large here) is a *ShapeError from Plan
// and from Configured, and the file is left as it is.
func TestAdaptersRefuseAValueTheyCannotRead(t *testing.T) {
	for name, tc := range map[string]struct {
		tool    Tool
		relPath string
		content string
	}{
		"Qwen Code": {Qwen{}, filepath.Join(".qwen", "settings.json"),
			`{"modelProviders": {"openai": [{"id": "m", "envKey": "EVREN_API_KEY", "generationConfig": {"contextWindowSize": 1e400}}]}}`},
		"opencode": {Opencode{}, filepath.Join(".config", "opencode", "opencode.json"),
			`{"provider": {"evren": {"options": {"apiKey": "{env:EVREN_API_KEY}", "timeout": 1e400}}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			path := write(t, filepath.Join(env.Home, tc.relPath), tc.content)
			for _, call := range []struct {
				what string
				run  func() error
			}{
				{"Plan", func() error { _, err := tc.tool.Plan(env, testRequest()); return err }},
				{"Configured", func() error { _, err := tc.tool.Configured(env); return err }},
			} {
				var se *ShapeError
				if err := call.run(); !errors.As(err, &se) || se.Path != path {
					t.Errorf("%s: %T %v", call.what, err, err)
				}
			}
			if got := content2(t, path); got != tc.content {
				t.Errorf("the file was changed:\n%q", got)
			}
		})
	}
}

// Only what the editor says about the shape of the user's file is a
// *ShapeError, which tells the user to fix the file. The three messages are
// taken from the editor itself, so a change of its wording fails here.
func TestEditErrors(t *testing.T) {
	file := func(content string) *jsoncFile {
		f, err := readJSONC(write(t, filepath.Join(t.TempDir(), "f.json"), content))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	for name, tc := range map[string]struct {
		err    error
		detail string
	}{
		"a key on the way is no object": {file(`{"a": 1}`).set(1, "a", "b"), "a is not an object"},
		"a key that stands twice":       {file(`{"a": {}, "a": {}}`).set(1, "a", "b"), "a appears more than once"},
		"a list that is none":           {file(`{"a": {"l": {}}}`).editError(file(`{"a": {"l": {}}}`).doc.UpdateArray(nil, []any{1}, "a", "l")), "a.l is not a list"},
	} {
		var se *ShapeError
		if !errors.As(tc.err, &se) || se.Detail != tc.detail || !strings.HasSuffix(se.Path, "f.json") {
			t.Errorf("%s: %T %v", name, tc.err, tc.err)
		}
	}
	// What the editor cannot read in the user's file is the file's as well.
	// The file is built without readJSONC here: a file readJSONC accepts
	// decodes as a whole (see readJSONC), which would leave nothing to test.
	raw := func(content string) *jsoncFile {
		doc, err := jsonc.Parse([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		return &jsoncFile{path: "f.json", old: []byte(content), doc: doc}
	}
	big := raw(`{"l": [{"envKey": "EVREN_API_KEY", "n": 1e400}]}`)
	deep := raw(strings.Repeat(`{"a":`, 10001) + "1" + strings.Repeat("}", 10001))
	for name, tc := range map[string]struct {
		err    error
		detail string
	}{
		"a number that does not fit": {big.editError(big.doc.UpdateArray(qwenOurs, nil, "l")), "a number is too large to read"},
		"values nested too deeply":   {deep.set(1, "new"), "the values are nested too deeply"},
	} {
		var se *ShapeError
		if !errors.As(tc.err, &se) || se.Detail != tc.detail {
			t.Errorf("%s: %T %v", name, tc.err, tc.err)
		}
	}
	// A file with such a number is not an empty file.
	if big.blank {
		t.Error("a file whose value cannot be decoded counts as blank")
	}
	// A value that cannot be written is the caller's mistake, not the file's.
	twice := jsonc.Object{{Key: "k", Value: 1}, {Key: "k", Value: 2}}
	f := file("{}")
	var se *ShapeError
	if err := f.set(twice, "a"); err == nil || errors.As(err, &se) || !strings.HasPrefix(err.Error(), f.path+": ") {
		t.Errorf("a value with a key twice: %T %v", err, err)
	}
	if err := f.set(1); err == nil || errors.As(err, &se) {
		t.Errorf("no path: %T %v", err, err)
	}
	if err := f.set(1, "a"); err != nil || f.editError(nil) != nil {
		t.Errorf("a good edit: %v", err)
	}
}

func TestVersionToken(t *testing.T) {
	for in, want := range map[string]string{
		"0.25.1":                 "0.25.1",
		"codex-cli 0.130.0\n":    "0.130.0",
		"opencode v2.0.26 (abc)": "2.0.26",
		"v22.11.0":               "22.11.0",
		"no version here":        "",
		// Another program's output: escape sequences end the version, and a
		// word of any length is cut.
		"codex 1.2.3\x1b[2J\x1b]0;owned\x07\n": "1.2.3",
		"1.2.3-beta.1+build_7~x (linux)":       "1.2.3-beta.1+build_7~x",
		"9.9" + strings.Repeat("9", 300):       "9.9" + strings.Repeat("9", maxVersionLen-3),
		"\x1b[31m2.0\x1b[0m":                   "",
		"3.":                                   "",
	} {
		if got := versionToken(in); got != want {
			t.Errorf("versionToken(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]int{"v20.19.5": 20, "22.0.0": 22, "abc": -1, "": -1} {
		if got := major(in); got != want {
			t.Errorf("major(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestApplyCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "tool", "config.json"), "old\n")
	fresh := filepath.Join(dir, "tool", "sub", "new.json")
	backups := filepath.Join(dir, "backups", "20261012-010203")
	plan := Plan{Files: []FileChange{
		{Path: existing, Old: []byte("old\n"), New: []byte("new\n")},
		{Path: fresh, New: []byte("created\n")},
	}}
	if err := Apply(plan, "tool", backups); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		existing:                              "new\n",
		fresh:                                 "created\n",
		BackupPath(backups, "tool", existing): "old\n",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	if _, err := os.Stat(BackupPath(backups, "tool", fresh)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a new file got a backup: %v", err)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{existing: 0o644, fresh: 0o600, filepath.Dir(fresh): 0o700} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Errorf("%s mode = %v, %v; want %v", path, info.Mode().Perm(), err, want)
			}
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, "tool", ".ulgen-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestApplyWithoutBackupLeavesTheFile(t *testing.T) {
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "config.json"), "old\n")
	blocker := write(t, filepath.Join(dir, "blocker"), "a file where the backup directory should be")
	err := Apply(Plan{Files: []FileChange{{Path: existing, Old: []byte("old\n"), New: []byte("new\n")}}}, "tool", blocker)
	var we *WriteError
	if !errors.As(err, &we) || we.Path != existing || len(we.Done) != 0 {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "old\n" {
		t.Errorf("the file was changed without a backup: %q", got)
	}
}

func TestApplyNamesWhatWasWritten(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := write(t, filepath.Join(dir, "second.json"), "old\n")
	saved := rename
	rename = func(old, new string) error {
		if new == second {
			return errors.New("no rename today")
		}
		return saved(old, new)
	}
	t.Cleanup(func() { rename = saved })
	err := Apply(Plan{Files: []FileChange{{Path: first, New: []byte("1")}, {Path: second, Old: []byte("old\n"), New: []byte("2")}}}, "tool", filepath.Join(dir, "backups"))
	var we *WriteError
	if !errors.As(err, &we) || we.Path != second || len(we.Done) != 1 || we.Done[0] != first {
		t.Errorf("err = %v", err)
	}
}

func TestApplyEmptyPlan(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(Plan{}, "tool", filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an empty plan created the backup directory: %v", err)
	}
}

// applyOneFile applies a plan that rewrites one existing file.
func applyOneFile(t *testing.T, path, old, new, backups string) error {
	t.Helper()
	return Apply(Plan{Files: []FileChange{{Path: path, Old: []byte(old), New: []byte(new)}}}, "tool", backups)
}

func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyFollowsASymbolicLink(t *testing.T) {
	dir := t.TempDir()
	real := write(t, filepath.Join(dir, "dotfiles", "settings.json"), "old\n")
	link := filepath.Join(dir, "home", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	backups := filepath.Join(dir, "backups")
	if err := applyOneFile(t, link, "old\n", "new\n", backups); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
	if got := content(t, real); got != "new\n" {
		t.Errorf("the file behind the link = %q", got)
	}
	if got := content(t, BackupPath(backups, "tool", link)); got != "old\n" {
		t.Errorf("backup = %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*", ".ulgen-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}

	// A link that leads nowhere is refused, and stays a link.
	dangling := filepath.Join(dir, "home", "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "missing.json"), dangling); err != nil {
		t.Fatal(err)
	}
	err = Apply(Plan{Files: []FileChange{{Path: dangling, New: []byte("x")}}}, "tool", backups)
	var we *WriteError
	if !errors.As(err, &we) || we.Path != dangling {
		t.Fatalf("err = %v", err)
	}
	if info, err := os.Lstat(dangling); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the dangling link was replaced: %v, %v", info, err)
	}
}

func TestApplyNeverOverwritesABackup(t *testing.T) {
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "config.json"), "second\n")
	backups := filepath.Join(dir, "backups")
	earlier := write(t, BackupPath(backups, "tool", existing), "the original\n")
	err := applyOneFile(t, existing, "second\n", "third\n", backups)
	var we *WriteError
	if !errors.As(err, &we) || we.Path != existing {
		t.Fatalf("err = %v", err)
	}
	if got := content(t, earlier); got != "the original\n" {
		t.Errorf("the earlier backup was overwritten: %q", got)
	}
	if got := content(t, existing); got != "second\n" {
		t.Errorf("the file was written without a backup: %q", got)
	}

	// A backup that already holds the same content is fine.
	same := write(t, filepath.Join(dir, "other.json"), "same\n")
	write(t, BackupPath(backups, "tool", same), "same\n")
	if err := applyOneFile(t, same, "same\n", "new\n", backups); err != nil {
		t.Fatal(err)
	}
	if got := content(t, same); got != "new\n" {
		t.Errorf("file = %q", got)
	}
}

func TestApplyRefusesAFileThatChanged(t *testing.T) {
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	var changed *ChangedError

	// Edited after the plan was made.
	edited := write(t, filepath.Join(dir, "edited.json"), "the user's newer text\n")
	err := applyOneFile(t, edited, "what the plan read\n", "new\n", backups)
	if !errors.As(err, &changed) || changed.Path != edited {
		t.Fatalf("edited: err = %v", err)
	}
	if got := content(t, edited); got != "the user's newer text\n" {
		t.Errorf("an edited file was overwritten: %q", got)
	}

	// Planned as new, but there now.
	appeared := write(t, filepath.Join(dir, "appeared.json"), "someone else's\n")
	err = Apply(Plan{Files: []FileChange{{Path: appeared, New: []byte("new\n")}}}, "tool", backups)
	if !errors.As(err, &changed) {
		t.Fatalf("appeared: err = %v", err)
	}
	if got := content(t, appeared); got != "someone else's\n" {
		t.Errorf("a file that appeared was overwritten: %q", got)
	}

	// Planned as a rewrite, but gone now.
	gone := filepath.Join(dir, "gone.json")
	err = applyOneFile(t, gone, "old\n", "new\n", backups)
	if !errors.As(err, &changed) {
		t.Fatalf("gone: err = %v", err)
	}
	if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file that was removed was created again: %v", err)
	}
	if _, err := os.Stat(backups); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused write left a backup directory: %v", err)
	}

	// An empty existing file is a file, not a missing one.
	empty := write(t, filepath.Join(dir, "empty.json"), "")
	if err := applyOneFile(t, empty, "", "new\n", backups); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if got := content(t, BackupPath(backups, "tool", empty)); got != "" {
		t.Errorf("backup of an empty file = %q", got)
	}
}

func TestApplyRefusesAReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	locked := write(t, filepath.Join(dir, "locked.json"), "old\n")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) }) // so the directory can be removed on Windows
	err := applyOneFile(t, locked, "old\n", "new\n", filepath.Join(dir, "backups"))
	var readOnly *ReadOnlyError
	var we *WriteError
	if !errors.As(err, &readOnly) || readOnly.Path != locked || !errors.As(err, &we) {
		t.Fatalf("err = %v", err)
	}
	if got := content(t, locked); got != "old\n" {
		t.Errorf("a read-only file was rewritten: %q", got)
	}
}

func TestApplyKeepsTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no such modes")
	}
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o600, 0o640, 0o664} {
		path := write(t, filepath.Join(dir, mode.String()+".json"), "old\n")
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := applyOneFile(t, path, "old\n", "new\n", filepath.Join(dir, "backups")); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
			t.Errorf("mode after the write = %v, %v; want %v", info.Mode().Perm(), err, mode)
		}
		if info, err := os.Stat(BackupPath(filepath.Join(dir, "backups"), "tool", path)); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("backup mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
	}
}

func TestApplyRemovesItsTemporaryFileWhenTheLastStepFails(t *testing.T) {
	saved := rename
	rename = func(string, string) error { return errors.New("no rename today") }
	t.Cleanup(func() { rename = saved })
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "config.json"), "old\n")
	err := applyOneFile(t, existing, "old\n", "new\n", filepath.Join(dir, "backups"))
	var we *WriteError
	if !errors.As(err, &we) {
		t.Fatalf("err = %v", err)
	}
	if got := content(t, existing); got != "old\n" {
		t.Errorf("file = %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".ulgen-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// editAfterTemp makes the target hold other content in the instant after the
// temporary file is written and before the last look.
func editAfterTemp(t *testing.T, target, content string) {
	t.Helper()
	saved := afterTemp
	afterTemp = func(path string) {
		if path == target {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { afterTemp = saved })
}

// A2: a link that leads nowhere is a *LinkError inside a *WriteError.
func TestApplyRefusesADanglingLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing.json")
	linkPath := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	err := Apply(Plan{Files: []FileChange{{Path: linkPath, New: []byte("x")}}}, "tool", filepath.Join(dir, "backups"))
	var le *LinkError
	var we *WriteError
	if !errors.As(err, &le) || le.Path != linkPath || !errors.As(err, &we) || we.Path != linkPath {
		t.Fatalf("err = %T %v", err, err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the dangling link was replaced: %v, %v", info, err)
	}
}

// A2: a backup that is already there with other content is a
// *BackupExistsError inside a *WriteError, naming the file and the backup.
func TestApplyRefusesAForeignBackup(t *testing.T) {
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "config.json"), "second\n")
	backups := filepath.Join(dir, "backups")
	earlier := write(t, BackupPath(backups, "tool", existing), "the original\n")
	err := applyOneFile(t, existing, "second\n", "third\n", backups)
	var be *BackupExistsError
	var we *WriteError
	if !errors.As(err, &be) || be.Path != existing || be.Backup != earlier || !errors.As(err, &we) {
		t.Fatalf("err = %T %v", err, err)
	}
	// No backup was made in this run, so the failure names none as made: the
	// one that is in the way holds something else than the file.
	if we.Backup != "" {
		t.Errorf("WriteError.Backup = %q, want none", we.Backup)
	}
	if got := content(t, existing); got != "second\n" {
		t.Errorf("the file was changed: %q", got)
	}
}

// A1: an edit made after the temporary file is complete but before it is put
// in place is refused and kept, and the backup made before it stays.
func TestApplyRefusesAnEditBeforeTheRename(t *testing.T) {
	dir := t.TempDir()
	existing := write(t, filepath.Join(dir, "config.json"), "old\n")
	backups := filepath.Join(dir, "backups")
	editAfterTemp(t, existing, "the user's newer text\n")
	err := applyOneFile(t, existing, "old\n", "new\n", backups)
	var changed *ChangedError
	var we *WriteError
	if !errors.As(err, &changed) || changed.Path != existing || !errors.As(err, &we) {
		t.Fatalf("err = %T %v", err, err)
	}
	if got := content(t, existing); got != "the user's newer text\n" {
		t.Errorf("the edit was lost: %q", got)
	}
	if got := content(t, BackupPath(backups, "tool", existing)); got != "old\n" {
		t.Errorf("the backup made before the refusal is gone: %q", got)
	}
	if we.Backup != BackupPath(backups, "tool", existing) {
		t.Errorf("the error does not name the backup: %+v", we)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".ulgen-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A1: a file created after the temporary file is complete where none was
// planned is kept; the hard link does not overwrite it.
func TestApplyRefusesAFileThatAppearedBeforeTheLink(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.json")
	editAfterTemp(t, fresh, "someone else's\n")
	err := Apply(Plan{Files: []FileChange{{Path: fresh, New: []byte("ours\n")}}}, "tool", filepath.Join(dir, "backups"))
	var changed *ChangedError
	if !errors.As(err, &changed) || changed.Path != fresh {
		t.Fatalf("err = %T %v", err, err)
	}
	if got := content(t, fresh); got != "someone else's\n" {
		t.Errorf("a file that appeared was overwritten: %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".ulgen-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A4: Check runs every check Apply runs before it writes, and returns the
// first refusal as a *WriteError, writing nothing.
func TestCheckRefusesWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")

	// A file that changed since the plan.
	edited := write(t, filepath.Join(dir, "edited.json"), "the user's newer text\n")
	var changed *ChangedError
	if err := Check(Plan{Files: []FileChange{{Path: edited, Old: []byte("what the plan read\n"), New: []byte("new\n")}}}); !errors.As(err, &changed) || changed.Path != edited {
		t.Errorf("changed: %T %v", err, err)
	}
	// A file planned as new that is there now.
	appeared := write(t, filepath.Join(dir, "appeared.json"), "someone else's\n")
	if err := Check(Plan{Files: []FileChange{{Path: appeared, New: []byte("new\n")}}}); !errors.As(err, &changed) {
		t.Errorf("appeared: %T %v", err, err)
	}
	// A read-only file.
	locked := write(t, filepath.Join(dir, "locked.json"), "old\n")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })
	var readOnly *ReadOnlyError
	if err := Check(Plan{Files: []FileChange{{Path: locked, Old: []byte("old\n"), New: []byte("new\n")}}}); !errors.As(err, &readOnly) || readOnly.Path != locked {
		t.Errorf("read-only: %T %v", err, err)
	}
	// A link that leads nowhere.
	dangling := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "missing.json"), dangling); err == nil {
		var link *LinkError
		if err := Check(Plan{Files: []FileChange{{Path: dangling, New: []byte("x")}}}); !errors.As(err, &link) {
			t.Errorf("dangling: %T %v", err, err)
		}
	}
	// A plan with nothing wrong: no error, and nothing written.
	good := filepath.Join(dir, "good.json")
	if err := Check(Plan{Files: []FileChange{{Path: good, New: []byte("new\n")}}}); err != nil {
		t.Errorf("a good plan: %v", err)
	}
	if _, err := os.Stat(good); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Check wrote a file: %v", err)
	}
	if _, err := os.Stat(backups); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Check created a directory: %v", err)
	}
}

// A4: a refusal in the second file of a plan leaves the first file untouched,
// because Apply runs every check before it writes anything.
func TestApplyChecksEveryFileBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	locked := write(t, filepath.Join(dir, "locked.json"), "old\n")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })
	err := Apply(Plan{Files: []FileChange{
		{Path: first, New: []byte("1")},
		{Path: locked, Old: []byte("old\n"), New: []byte("2")},
	}}, "tool", filepath.Join(dir, "backups"))
	var we *WriteError
	var readOnly *ReadOnlyError
	if !errors.As(err, &we) || we.Path != locked || !errors.As(err, &readOnly) {
		t.Fatalf("err = %T %v", err, err)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the first file was written: %v", err)
	}
}

// A1: where the file system has no hard links, a new file still goes in
// place through the rename, after one more existence check.
func TestApplyNewFileFallsBackToRename(t *testing.T) {
	saved := link
	link = func(string, string) error { return errors.New("no hard links here") }
	t.Cleanup(func() { link = saved })
	dir := t.TempDir()
	fresh := filepath.Join(dir, "sub", "new.json")
	if err := Apply(Plan{Files: []FileChange{{Path: fresh, New: []byte("ours\n")}}}, "tool", filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	if got := content(t, fresh); got != "ours\n" {
		t.Errorf("file = %q", got)
	}

	// The same fallback refuses a file that appeared in the meantime.
	appeared := filepath.Join(dir, "appeared.json")
	editAfterTemp(t, appeared, "someone else's\n")
	err := Apply(Plan{Files: []FileChange{{Path: appeared, New: []byte("ours\n")}}}, "tool", filepath.Join(dir, "backups"))
	var changed *ChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("err = %T %v", err, err)
	}
	if got := content(t, appeared); got != "someone else's\n" {
		t.Errorf("a file that appeared was overwritten: %q", got)
	}
}
