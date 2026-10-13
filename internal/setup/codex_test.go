package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the local date and time test starts a process in another zone

	"github.com/BurntSushi/toml"

	"github.com/mftnakrsu/ulgen/internal/i18n"
)

// codexProfileFresh is the whole profile file on a machine without one.
const codexProfileFresh = `# EVREN profile for Codex CLI, written by "ulgen setup codex". Use it with: codex --profile evren
#
# Ülgen rewrites this file when one of its own keys has to change. Your other
# keys are kept, with every value; comments are not, and neither is the layout
# (the order of the keys, the way strings and numbers are written, inline
# tables). The file as it was goes to Ülgen's backups.
# Ülgen sets: model, model_provider, web_search, model_context_window,
# [features] view_image, and in [model_providers.evren]: name, base_url,
# wire_api, env_key, env_key_instructions.

model = "deepseek-v4.1-flash"
model_provider = "evren"
web_search = "disabled"

[model_providers]
[model_providers.evren]
base_url = "https://evren-llmapi.ssyz.org.tr/v1"
env_key = "EVREN_API_KEY"
env_key_instructions = "Start Codex with \"ulgen run codex\", or put the key in your shell first: export EVREN_API_KEY=\"$(ulgen token)\""
name = "EVREN"
wire_api = "responses"
`

// codexAfterMCPAdd is config.toml exactly as Codex 0.162.1 leaves it after
// "codex mcp add docs -- echo hi" and "codex features enable shell_tool" on
// a machine that had none.
const codexAfterMCPAdd = "[mcp_servers.docs]\ncommand = \"echo\"\nargs = [\"hi\"]\n\n[features]\nshell_tool = true\n"

// codexValueTypes is a profile of the user's with every kind of TOML value.
const codexValueTypes = `hex = 0xDEADBEEF
big = 9223372036854775807
neg = -17
flt = 6.02e23
whole = 3.0
pinf = inf
ninf = -inf
notnum = nan
yes = true
utc = 2026-10-12T09:00:00Z
offset = 2026-10-12T09:00:00.123456789+05:30
multi = """
two
lines \"quoted\" \\ back"""
literal = 'C:\Users\x'
control = "bell\u0007 del\u007f tab\t nul\u0000"
mixed = [1, "two", 3.5, true, [4, 5], { k = "v" }]
nested = [[1, 2], ["a"], []]
empty = []
inline = { a = 1, b = { c = [1, 2] } }
"quoted key" = 1
"dotted.key" = 2

[table.sub]
x = 1

[[hooks]]
name = "one"

[[hooks]]
name = "two"
args = ["a", "b"]
`

// codexProfiles are existing profile files; every one of them must come
// through setup with nothing but Ülgen's keys changed.
var codexProfiles = map[string]string{
	"empty":                        "",
	"whitespace only":              "\n\n  \n",
	"a comment only":               "# mine\n",
	"a user key, no final newline": `model_reasoning_effort = "high"`,
	"ends in a comment":            "model_reasoning_effort = \"high\"\n# bye",
	"trailing blank lines":         "model_reasoning_effort = \"high\"\n\n\n\n",
	"byte-order mark":              "\xef\xbb\xbfmodel_reasoning_effort = \"high\"\n",
	"CRLF":                         "model_reasoning_effort = \"high\"\r\n\r\n[features]\r\nmulti_agent = false\r\n",
	"CRLF, no final newline":       "model_reasoning_effort = \"high\"\r\n[notice]\r\nhide = true",
	"ends in a multi-line string":  "notes = \"\"\"\nhello\n[not.a.table]\n\"\"\"",
	"dotted keys":                  "features.multi_agent = false\nmodel_providers.other.name = \"O\"\nmodel_providers.other.base_url = \"https://o.example/v1\"\n",
	"inline tables":                "features = { multi_agent = false }\nmodel_providers = { other = { name = \"O\", base_url = \"https://o.example/v1\" } }\n",
	"an array of tables last":      "[model_providers.other]\nname = \"O\"\n\n[[hooks]]\na = 1\n[[hooks]]\na = 2\n",
	"another model and provider":   "model = \"gpt-6\"\nmodel_provider = \"openai\"\nweb_search = \"live\"\n",
	"a model EVREN offers":         "model = \"qwen3-vl-30b\"\n",
	"the user's own limits":        "model = \"qwen3-vl-30b\"\nmodel_context_window = 100000\n\n[features]\nview_image = false\nmulti_agent = false\n",
	"an EVREN table with more":     "[model_providers.evren]\nname = \"mine\"\nbase_url = \"https://old.example/v1\"\nrequest_max_retries = 2\n\n[model_providers.evren.http_headers]\nx-team = \"a\"\n",
	"local dates and times":        "started = 2026-10-12T09:00:00\nday = 2026-10-12\nat = 09:00:00\nshort = 2026-10-12T09:00\nlist = [2026-10-12, 23:59:59.5]\n\n[t]\nwhen = 1979-05-27T07:32:00\n",
	"every value type":             codexValueTypes,
	"arrays of tables with tables": "[[hooks]]\nname = \"one\"\n[hooks.sub]\ny = 1\n[[hooks]]\nname = \"two\"\n[[hooks.items]]\nz = 1\n\n[[a.b]]\nx = 1\n[[a.b]]\nx = 2\n",
	"a table after its sub-table":  "[a.b]\nx = 1\n[a]\ny = 2\n",
	"empty tables":                 "[empty]\n\n[[none]]\n[[none]]\n\n[inline]\ne = {}\nlist = [{}]\n",
	"tables inside arrays":         "x = [ { a = 1 }, 2, [ { b = 3 } ] ]\ny = { a = [ { b = [ { c = 1 } ] } ] }\n",
	"numbers in every spelling":    "a = 1_000\nb = 0o755\nc = 0b1010\nd = -0\ne = +7\nf = -0.0\ng = +inf\nh = -nan\ni = 1e-7\nj = 5e+22\nk = 123456789012345680.0\n",
	"dates in every spelling":      "a = 2026-10-12 09:00:00Z\nb = 2026-10-12t09:00:00z\nc = 2026-10-12T09:00:00.5-03:30\nd = 0001-01-01T00:00:00Z\ne = 2026-10-12 09:00:00\nf = 2026-10-12T09:00:00.123456\ng = 00:00:00\nh = 9999-12-31\ni = 09:00\n",
	"strings in every spelling":    "a = \"\\b\\t\\n\\f\\r\\\"\\\\\\u00e9\\U0001F600\\u0001\\u001f\"\nb = '''\nfirst\n  second\\n\n'''\nc = \"\"\"\\\n   joined \\\n   lines\"\"\"\nd = \"\"\"a'''b\\\"\\\"\\\"c\"\"\"\n\"ключ\" = \"değer\"\n1234 = \"x\"\n\"a\\\"b\" = 1\n",
	"TOML 1.1":                     "x = {\n  a = 1,\n  b = 2,\n}\ns = \"\\e[0m \\x41\"\n",
	"a table named with a dot":     "[\"model_providers.evren\"]\nname = \"not the provider\"\n\n[features.sub]\nx = 1\n",
}

// codexAwkwardConfigs are config.toml files Ülgen must leave exactly as
// they are; setup succeeds for each.
var codexAwkwardConfigs = map[string]string{
	"empty":                                 "",
	"whitespace only":                       "\n\n  \n",
	"no final newline":                      "model = \"gpt-6\"\nmodel_provider = \"openai\"",
	"ends in a comment":                     "model = \"gpt-6\"\n# bye",
	"trailing blank lines":                  "model = \"gpt-6\"\n\n\n\n",
	"byte-order mark":                       "\xef\xbb\xbfmodel = \"gpt-6\"\n",
	"CRLF":                                  "model = \"gpt-6\"\r\n\r\n[notice]\r\nhide = true\r\n",
	"CRLF, no final newline":                "model = \"gpt-6\"\r\n[notice]\r\nhide = true",
	"ends in a multi-line string":           "instructions = \"\"\"\nhello\n[not.a.table]\n\"\"\"",
	"another legacy profile":                "model = \"gpt-6\"\n\n[profiles.work]\nmodel = \"x\"\n",
	"dotted-key providers":                  "model_providers.other.name = \"O\"\nmodel_providers.other.base_url = \"https://o.example/v1\"\n",
	"an inline provider under the header":   "[model_providers]\nother = { name = \"O\" }\n",
	"an array of tables last":               "[model_providers.other]\nname = \"O\"\n\n[[mcp]]\na = 1\n[[mcp]]\na = 2\n",
	"model_providers as an inline table":    "model_providers = { other = { name = \"O\", base_url = \"https://o.example/v1\" } }\n",
	"model_providers as a string":           "model_providers = \"x\"\n",
	"model_providers as an array of tables": "[[model_providers]]\nname = \"x\"\n",
	"as Codex leaves it after mcp add":      codexAfterMCPAdd,
	"MCP servers, a provider, trust":        "# my codex\nmodel = \"gpt-6\"\nmodel_provider = \"corp\"\n\n[model_providers.corp]\nname = \"Corp\"\nbase_url = \"https://llm.corp.example/v1\"\nenv_key = \"CORP_KEY\"\n\n[mcp_servers.docs]\ncommand = \"docs-mcp\"\nargs = [\"--stdio\"]\n\n[features]\nshell_tool = true\n\n[projects.\"/work\"]\ntrust_level = \"trusted\"\n",
	"the header text inside a string":       "x = \"\"\"\n[model_providers.evren]\nname = \"mine\"\n\"\"\"\n",
	"another spelling of the name":          "[model_providers.EVREN]\nname = \"mine\"\n",
}

func codexPaths(env Env) (config, profile string) {
	dir := filepath.Join(env.Home, ".codex")
	return filepath.Join(dir, "config.toml"), filepath.Join(dir, "evren.config.toml")
}

// codexDoc is the part of a profile Ülgen writes, decoded by its TOML names.
type codexDoc struct {
	Model         string         `toml:"model"`
	ModelProvider string         `toml:"model_provider"`
	WebSearch     string         `toml:"web_search"`
	ContextWindow *int64         `toml:"model_context_window"`
	Features      map[string]any `toml:"features"`
	Providers     map[string]struct {
		Name            string `toml:"name"`
		BaseURL         string `toml:"base_url"`
		WireAPI         string `toml:"wire_api"`
		EnvKey          string `toml:"env_key"`
		KeyInstructions string `toml:"env_key_instructions"`
	} `toml:"model_providers"`
}

func codexDecodeDoc(t *testing.T, text string) codexDoc {
	t.Helper()
	var doc codexDoc
	if _, err := toml.Decode(text, &doc); err != nil {
		t.Fatalf("not TOML: %v\n%s", err, text)
	}
	return doc
}

// codexDecode reads TOML into the library's general form.
func codexDecode(t *testing.T, text string) map[string]any {
	t.Helper()
	doc := map[string]any{}
	if _, err := toml.Decode(text, &doc); err != nil {
		t.Fatalf("not TOML: %v\n%s", err, text)
	}
	return doc
}

// codexCanon writes a decoded TOML value as text in which two values are
// equal exactly when they mean the same: keys are sorted, every value shows
// its type, a date or time shows whether it has an offset.
func codexCanon(v any) string {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("{")
		for _, k := range keys {
			fmt.Fprintf(&b, "%q: %s, ", k, codexCanon(v[k]))
		}
		return b.String() + "}"
	case []map[string]any:
		items := make([]any, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return codexCanon(items)
	case []any:
		var b strings.Builder
		b.WriteString("[")
		for _, item := range v {
			b.WriteString(codexCanon(item) + ", ")
		}
		return b.String() + "]"
	case time.Time:
		return "time(" + v.Location().String() + " " + v.Format(time.RFC3339Nano) + ")"
	case float64:
		return "float(" + strconv.FormatFloat(v, 'g', -1, 64) + ")"
	case string:
		return strconv.Quote(v)
	default:
		return fmt.Sprintf("%T(%v)", v, v)
	}
}

// codexWithoutUlgen returns a decoded profile without the keys Ülgen
// manages; a table left empty by that goes too.
func codexWithoutUlgen(doc map[string]any) map[string]any {
	for _, key := range []string{"model", "model_provider", "web_search", "model_context_window"} {
		delete(doc, key)
	}
	if features, ok := doc["features"].(map[string]any); ok {
		delete(features, "view_image")
		if len(features) == 0 {
			delete(doc, "features")
		}
	}
	if providers, ok := doc["model_providers"].(map[string]any); ok {
		if evren, ok := providers["evren"].(map[string]any); ok {
			for _, key := range []string{"name", "base_url", "wire_api", "env_key", "env_key_instructions"} {
				delete(evren, key)
			}
			if len(evren) == 0 {
				delete(providers, "evren")
			}
		}
		if len(providers) == 0 {
			delete(doc, "model_providers")
		}
	}
	return doc
}

// codexCheckUlgenKeys fails unless the profile selects the EVREN provider
// with the request's values.
func codexCheckUlgenKeys(t *testing.T, text string, req Request) {
	t.Helper()
	doc := codexDecodeDoc(t, text)
	e := doc.Providers["evren"]
	if _, offered := req.model(doc.Model); !offered || doc.ModelProvider != "evren" || doc.WebSearch != "disabled" ||
		e.Name != "EVREN" || e.BaseURL != req.BaseURL || e.WireAPI != "responses" || e.EnvKey != "EVREN_API_KEY" ||
		!strings.Contains(e.KeyInstructions, "ulgen run codex") {
		t.Errorf("Ülgen's keys are not all set: %+v", doc)
	}
}

// codexSetup plans and applies setup with the request and returns the plan.
func codexSetup(t *testing.T, env Env, req Request) Plan {
	t.Helper()
	p, err := Codex{}.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	applyAndReplan(t, Codex{}, env, req, p)
	return p
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCodexFreshMachine(t *testing.T) {
	env := testEnv(t, nil, nil)
	config, profile := codexPaths(env)
	if ok, err := (Codex{}).Configured(env); ok || err != nil {
		t.Errorf("Configured before setup = %v, %v", ok, err)
	}
	p, err := Codex{}.Plan(env, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 || p.Files[0].Path != profile || p.Files[0].Old != nil {
		t.Fatalf("the plan writes %v; it must create the profile and nothing else", paths(p))
	}
	if got := string(p.Files[0].New); got != codexProfileFresh {
		t.Errorf("evren.config.toml:\n%s", got)
	}
	if !hasNote(p, "setup.note.codex_profile") || !hasNote(p, "setup.note.export") || hasNote(p, "setup.note.codex_default") ||
		hasNote(p, "setup.note.codex_rewritten") || hasNote(p, "setup.note.codex_model_replaced") {
		t.Errorf("notes = %v", p.Notes)
	}
	applyAndReplan(t, Codex{}, env, testRequest(), p)
	if _, err := os.Stat(config); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config.toml was created: %v", err)
	}
	if ok, err := (Codex{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured after setup = %v, %v", ok, err)
	}
	codexCheckUlgenKeys(t, mustRead(t, profile), testRequest())
}

func TestCodexDefaultHasNoEffect(t *testing.T) {
	env := testEnv(t, nil, nil)
	config, _ := codexPaths(env)
	user := "model = \"gpt-6\"\nmodel_provider = \"openai\"\n"
	write(t, config, user)
	req := testRequest()
	req.MakeDefault = true
	p := codexSetup(t, env, req)
	if !hasNote(p, "setup.note.codex_default") || !hasNote(p, "setup.note.codex_profile") {
		t.Errorf("notes = %v", p.Notes)
	}
	if got := mustRead(t, config); got != user {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// config.toml is the user's: whatever is in it, the plan never holds it and
// its bytes and mode stay.
func TestCodexNeverWritesConfigToml(t *testing.T) {
	for name, content := range codexAwkwardConfigs {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			config, profile := codexPaths(env)
			write(t, config, content)
			if err := os.Chmod(config, 0o640); err != nil {
				t.Fatal(err)
			}
			p := codexSetup(t, env, testRequest())
			if len(p.Files) != 1 || p.Files[0].Path != profile {
				t.Errorf("the plan writes %v", paths(p))
			}
			if got := mustRead(t, config); got != content {
				t.Errorf("config.toml changed:\n%q", got)
			}
			if info, err := os.Stat(config); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o640) {
				t.Errorf("config.toml mode: %v, %v", info.Mode(), err)
			}
			entries, err := os.ReadDir(filepath.Dir(config))
			if err != nil || len(entries) != 2 {
				t.Errorf("the Codex directory holds %v, %v", entries, err)
			}
			if ok, err := (Codex{}).Configured(env); !ok || err != nil {
				t.Errorf("Configured = %v, %v", ok, err)
			}
		})
	}
}

// refused checks that neither Plan nor Configured accepts the machine and
// that Plan writes nothing; it returns Plan's error.
func codexRefused(t *testing.T, env Env) error {
	t.Helper()
	p, err := Codex{}.Plan(env, testRequest())
	if err == nil {
		t.Fatalf("Plan accepted it and writes %v", paths(p))
	}
	if len(p.Files) != 0 {
		t.Errorf("a refused plan holds %v", paths(p))
	}
	if ok, cerr := (Codex{}).Configured(env); ok || cerr == nil || cerr.Error() != err.Error() {
		t.Errorf("Configured = %v, %v; Plan said %v", ok, cerr, err)
	}
	return err
}

func TestCodexRefusesAProviderOfTheUsersOwn(t *testing.T) {
	for name, content := range map[string]string{
		"plain header":        "[model_providers.evren]\nname = \"mine\"\n",
		"quoted":              "[model_providers.\"evren\"]\nname = \"mine\"\n",
		"literal quoted":      "[model_providers.'evren']\nname = \"mine\"\n",
		"spaces in header":    "[ model_providers . evren ]\nname = \"mine\"\n",
		"dotted key":          "model_providers.evren.base_url = \"x\"\n",
		"inline table":        "model_providers = { evren = { name = \"mine\" } }\n",
		"inline under header": "[model_providers]\nevren = { name = \"mine\" }\n",
		"dotted under header": "[model_providers]\nevren.name = \"mine\"\n",
		"only a sub-table":    "[model_providers.evren.auth]\ncommand = \"x\"\n",
		"empty table":         "[model_providers.evren]\n",
		"evren is a string":   "[model_providers]\nevren = \"x\"\n",
		"evren is false":      "[model_providers]\nevren = false\n",
		"unicode-escaped key": "[model_providers.\"\\u0065vren\"]\nname = \"mine\"\n",
		// What the prototype of Ülgen wrote into config.toml, with a table
		// Codex added inside the markers.
		"the block of an earlier Ülgen": "model = \"gpt-6\"\n\n# >>> ulgen: EVREN provider (written by \"ulgen setup codex\"; changes inside this block are overwritten) >>>\n[model_providers.evren]\nname = \"EVREN\"\nbase_url = \"https://evren-llmapi.ssyz.org.tr/v1\"\nwire_api = \"responses\"\nenv_key = \"EVREN_API_KEY\"\n\n[mcp_servers.docs]\ncommand = \"echo\"\nargs = [\"hi\"]\n# <<< ulgen: EVREN provider <<<\n",
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			config, profile := codexPaths(env)
			write(t, config, content)
			var fe *ForeignEntryError
			if err := codexRefused(t, env); !errors.As(err, &fe) || fe.Path != config || fe.Entry != "model_providers.evren" {
				t.Errorf("err = %T %v", err, err)
			}
			if got := mustRead(t, config); got != content {
				t.Errorf("config.toml changed:\n%q", got)
			}
			if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the profile was written: %v", err)
			}
		})
	}
}

// What makes Codex 0.162.1 refuse "--profile evren" (checked with the real
// binary): a profile selected at the top level of config.toml, whatever its
// name, and a profiles.evren of any kind. The same key at the top of the
// profile file stops it too.
func TestCodexRefusesALegacyProfile(t *testing.T) {
	const evrenTable = "[profiles.evren]"
	for name, tc := range map[string]struct{ content, key string }{
		"evren selected":                    {"model = \"gpt-6\"\nprofile = \"evren\"\n", `profile = "evren"`},
		"another profile selected":          {"profile = \"work\"\n", `profile = "work"`},
		"another spelling selected":         {"profile = \"EVREN\"\n", `profile = "EVREN"`},
		"an empty name selected":            {"profile = \"\"\n", `profile = ""`},
		"a table":                           {"[profiles.evren]\nmodel = \"x\"\n", evrenTable},
		"an empty table":                    {"[profiles.evren]\n", evrenTable},
		"a quoted table":                    {"[profiles.\"evren\"]\nmodel = \"x\"\n", evrenTable},
		"a dotted key":                      {"profiles.evren.model = \"x\"\n", evrenTable},
		"an inline table":                   {"profiles = { evren = { model = \"x\" } }\n", evrenTable},
		"only a sub-table":                  {"[profiles.evren.features]\nview_image = false\n", evrenTable},
		"not a table":                       {"[profiles]\nevren = \"x\"\n", evrenTable},
		"next to another profile's table":   {"[profiles.work]\nmodel = \"y\"\n\n[profiles.evren]\nmodel = \"x\"\n", evrenTable},
		"selected and defined":              {"profile = \"evren\"\n\n[profiles.evren]\nmodel = \"x\"\n", `profile = "evren"`},
		"another selected, evren defined":   {"profile = \"work\"\n\n[profiles.work]\nmodel = \"y\"\n\n[profiles.evren]\nmodel = \"x\"\n", `profile = "work"`},
		"another selected and defined":      {"profile = \"work\"\n\n[profiles.work]\nmodel = \"y\"\n", `profile = "work"`},
		"a name with a quote and a newline": {"profile = \"a\\\"b\\nc\"\n", `profile = "a\"b\nc"`},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			config, profile := codexPaths(env)
			write(t, config, tc.content)
			var le *LegacyProfileError
			if err := codexRefused(t, env); !errors.As(err, &le) || le.Path != config || le.Key != tc.key {
				t.Errorf("err = %T %v (%+v)", err, err, le)
			}
			if got := mustRead(t, config); got != tc.content {
				t.Errorf("config.toml changed:\n%q", got)
			}
			if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the profile was written: %v", err)
			}
		})
	}

	t.Run("selected in the profile file", func(t *testing.T) {
		env := testEnv(t, nil, nil)
		_, profile := codexPaths(env)
		const content = "profile = \"work\"\nmodel_reasoning_effort = \"high\"\n"
		write(t, profile, content)
		var le *LegacyProfileError
		if err := codexRefused(t, env); !errors.As(err, &le) || le.Path != profile || le.Key != `profile = "work"` {
			t.Errorf("err = %T %v (%+v)", err, err, le)
		}
		if got := mustRead(t, profile); got != content {
			t.Errorf("the profile changed:\n%q", got)
		}
	})

	// What Codex loads is not refused: the tables of other profiles, and
	// any profiles table inside the profile file. A value of the wrong type
	// is not refused either: Codex fails on it with or without a profile
	// file and names it itself, like every other mistyped key of the file.
	for name, content := range map[string]string{
		"another profile's table":   "[profiles.work]\nmodel = \"x\"\n",
		"another spelling":          "[profiles.Evren]\nmodel = \"x\"\n\n[profiles.EVREN]\nmodel = \"y\"\n",
		"an empty profiles table":   "[profiles]\n",
		"the text in a string":      "x = \"\"\"\nprofile = \"evren\"\n[profiles.evren]\n\"\"\"\n",
		"profile is a number":       "profile = 5\n",
		"profiles is a string":      "profiles = \"x\"\n",
		"another profile, no table": "[profiles]\nwork = \"x\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			config, _ := codexPaths(env)
			write(t, config, content)
			codexSetup(t, env, testRequest())
			if got := mustRead(t, config); got != content {
				t.Errorf("config.toml changed:\n%q", got)
			}
		})
	}
	t.Run("profiles tables in the profile file", func(t *testing.T) {
		env := testEnv(t, nil, nil)
		_, profile := codexPaths(env)
		write(t, profile, "[profiles.evren]\nmodel = \"x\"\n\n[profiles.work]\nmodel = \"y\"\n")
		codexSetup(t, env, testRequest())
		kept, _ := codexDecode(t, mustRead(t, profile))["profiles"].(map[string]any)
		if len(kept) != 2 {
			t.Errorf("the user's tables are gone:\n%s", mustRead(t, profile))
		}
	})
}

func TestCodexMalformedFiles(t *testing.T) {
	// The error names the line of the file as it is on disk.
	const broken = "a = 1\n\n[notice]\nhide = true\n\n# five lines down\nb = \n"
	for _, which := range []string{"config.toml", "evren.config.toml"} {
		t.Run(which, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			config, profile := codexPaths(env)
			path := config
			if which == "evren.config.toml" {
				path = profile
			}
			write(t, path, broken)
			var pe *ParseError
			if err := codexRefused(t, env); !errors.As(err, &pe) || pe.Format != "TOML" || pe.Path != path || !strings.Contains(pe.Detail, "line 7") {
				t.Errorf("err = %T %v", err, err)
			}
			if got := mustRead(t, path); got != broken {
				t.Errorf("the file changed:\n%q", got)
			}
			entries, err := os.ReadDir(filepath.Dir(config))
			if err != nil || len(entries) != 1 {
				t.Errorf("the Codex directory holds %v, %v", entries, err)
			}
		})
	}
}

// One property over every existing profile: setup changes Ülgen's keys and
// nothing else, and a second run has nothing left to do.
func TestCodexProfileLayouts(t *testing.T) {
	for name, content := range codexProfiles {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, content)
			req := testRequest()
			p, err := Codex{}.Plan(env, req)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Files) != 1 || p.Files[0].Path != profile || string(p.Files[0].Old) != content {
				t.Fatalf("the plan writes %v", paths(p))
			}
			if !hasNote(p, "setup.note.codex_rewritten") {
				t.Errorf("no note says that the file is rewritten: %v", p.Notes)
			}
			applyAndReplan(t, Codex{}, env, req, p)
			written := mustRead(t, profile)
			codexCheckUlgenKeys(t, written, req)
			before := codexCanon(codexWithoutUlgen(codexDecode(t, content)))
			after := codexCanon(codexWithoutUlgen(codexDecode(t, written)))
			if before != after {
				t.Errorf("the user's keys changed\nbefore: %s\nafter:  %s\nfile:\n%s", before, after, written)
			}
			if ok, err := (Codex{}).Configured(env); !ok || err != nil {
				t.Errorf("Configured = %v, %v", ok, err)
			}
		})
	}
}

// The value types one by one: what comes out decodes to what went in, plus
// Ülgen's keys.
func TestCodexProfileKeepsEveryValueType(t *testing.T) {
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	write(t, profile, codexValueTypes)
	codexSetup(t, env, testRequest())
	want := codexDecode(t, codexValueTypes)
	want["model"] = "deepseek-v4.1-flash"
	want["model_provider"] = "evren"
	want["web_search"] = "disabled"
	want["model_providers"] = map[string]any{"evren": map[string]any{
		"name": "EVREN", "base_url": testRoot, "wire_api": "responses", "env_key": "EVREN_API_KEY",
		"env_key_instructions": `Start Codex with "ulgen run codex", or put the key in your shell first: export EVREN_API_KEY="$(ulgen token)"`,
	}}
	if got := codexCanon(codexDecode(t, mustRead(t, profile))); got != codexCanon(want) {
		t.Errorf("decoded profile\n got: %s\nwant: %s", got, codexCanon(want))
	}
}

// The TOML library reads the machine's zone when the program starts and
// writes a date or time that has no offset shifted by it. So this test also
// runs itself in a process that starts in a zone east of UTC; a machine set
// to UTC would not see the fault otherwise.
func TestCodexLocalDatesAndTimesSurviveTwoRuns(t *testing.T) {
	const marker = "ULGEN_TEST_CODEX_ZONE"
	if os.Getenv(marker) == "" && runtime.GOOS != "windows" { // Go on Windows does not read TZ
		cmd := exec.Command(os.Args[0], "-test.run", "^TestCodexLocalDatesAndTimesSurviveTwoRuns$")
		cmd.Env = append(os.Environ(), "TZ=Europe/Istanbul", marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("in a process started with TZ=Europe/Istanbul: %v\n%s", err, out)
		}
	}
	if os.Getenv(marker) != "" {
		if _, offset := time.Now().Zone(); offset == 0 {
			t.Fatal("this process was to start in Europe/Istanbul and is at UTC")
		}
	}
	const user = "started = 2026-10-12T09:00:00\nday = 2026-10-12\nat = 09:00:00\nfine = 23:59:59.123456789\nshort = 2026-10-12T09:00\nlist = [2026-10-12, 09:00:00, 2026-10-12T09:00:00]\n\n[t]\nwhen = 1979-05-27T07:32:00\n"
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	write(t, profile, user)
	req := testRequest()
	codexSetup(t, env, req)
	// A second run that has to write again: another API root.
	req.BaseURL = "http://127.0.0.1:4141/v1"
	codexSetup(t, env, req)
	written := mustRead(t, profile)
	for _, line := range []string{
		"started = 2026-10-12T09:00:00\n", "day = 2026-10-12\n", "at = 09:00:00\n", "fine = 23:59:59.123456789\n",
		"short = 2026-10-12T09:00:00\n", "list = [2026-10-12, 09:00:00, 2026-10-12T09:00:00]\n", "when = 1979-05-27T07:32:00\n",
		"base_url = \"http://127.0.0.1:4141/v1\"\n",
	} {
		if !strings.Contains(written, line) {
			t.Errorf("the profile lacks %q:\n%s", line, written)
		}
	}
	before := codexCanon(codexWithoutUlgen(codexDecode(t, user)))
	if after := codexCanon(codexWithoutUlgen(codexDecode(t, written))); after != before {
		t.Errorf("the user's values changed\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestCodexProfileWrongShapes(t *testing.T) {
	for name, tc := range map[string]struct{ content, key string }{
		"features is a string":             {"features = \"all\"\n", "features is a string"},
		"features is a boolean":            {"features = true\n", "features is a boolean"},
		"features is an array of tables":   {"[[features]]\nview_image = false\n", "features is an array"},
		"model is a number":                {"model = 5\n", "model is an integer"},
		"model is a table":                 {"[model]\nid = \"x\"\n", "model is a table"},
		"model_provider is a number":       {"model_provider = 5\n", "model_provider is an integer"},
		"model_providers is a string":      {"model_providers = \"x\"\n", "model_providers is a string"},
		"model_providers is an array":      {"[[model_providers]]\nname = \"x\"\n", "model_providers is an array"},
		"the EVREN provider is a string":   {"[model_providers]\nevren = \"x\"\n", "model_providers.evren is a string"},
		"the EVREN provider is false":      {"[model_providers]\nevren = false\n", "model_providers.evren is a boolean"},
		"the window is a string":           {"model_context_window = \"big\"\n", "model_context_window is a string"},
		"the window is a float":            {"model_context_window = 1.5\n", "model_context_window is a float"},
		"view_image is a string":           {"[features]\nview_image = \"no\"\n", "features.view_image is a string"},
		"a token command next to the key":  {"[model_providers.evren.auth]\ncommand = \"ulgen\"\nargs = [\"token\"]\n", "model_providers.evren.auth"},
		"a token command, inline spelling": {"model_providers = { evren = { auth = { command = \"x\" } } }\n", "model_providers.evren.auth"},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, tc.content)
			var se *ShapeError
			if err := codexRefused(t, env); !errors.As(err, &se) || se.Path != profile || !strings.Contains(se.Detail, tc.key) {
				t.Errorf("err = %T %v", err, err)
			}
			if got := mustRead(t, profile); got != tc.content {
				t.Errorf("the profile changed:\n%q", got)
			}
		})
	}
}

func TestCodexContextWindow(t *testing.T) {
	const absent = -1 << 40
	for name, tc := range map[string]struct {
		context int    // what the API says about the model
		had     string // the profile's line, or ""
		want    int64
	}{
		"small model":                             {262144, "", 262144},
		"large model":                             {1048576, "", absent},
		"exactly Codex's window":                  {272000, "", absent},
		"one below Codex's window":                {271999, "", 271999},
		"the API gives none":                      {0, "", absent},
		"the API gives a negative number":         {-1, "", absent},
		"the user's lower value stays":            {262144, "model_context_window = 100000\n", 100000},
		"a value above the model's is lowered":    {262144, "model_context_window = 270000\n", 262144},
		"zero is replaced":                        {262144, "model_context_window = 0\n", 262144},
		"a negative value is replaced":            {262144, "model_context_window = -5\n", 262144},
		"zero goes when the API gives none":       {0, "model_context_window = 0\n", absent},
		"a negative value goes for a large model": {1048576, "model_context_window = -1\n", absent},
		"the user's value stays, large model":     {1048576, "model_context_window = 100000\n", 100000},
		"the user's value stays, no API value":    {0, "model_context_window = 50000\n", 50000},
		"a value Codex caps anyway stays":         {1048576, "model_context_window = 900000\n", 900000},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			if tc.had != "" {
				write(t, profile, tc.had)
			}
			req := Request{BaseURL: testRoot, Default: "m", Models: []Model{{ID: "m", Context: tc.context, Vision: true}}}
			codexSetup(t, env, req)
			got := int64(absent)
			if w := codexDecodeDoc(t, mustRead(t, profile)).ContextWindow; w != nil {
				got = *w
			}
			if got != tc.want {
				t.Errorf("model_context_window = %d, want %d (%d means absent)", got, tc.want, int64(absent))
			}
		})
	}
}

func TestCodexViewImage(t *testing.T) {
	for name, tc := range map[string]struct {
		vision bool
		had    string // the profile's content, or ""
		want   any    // nil: the key is absent
	}{
		"no vision":                         {false, "", false},
		"vision":                            {true, "", nil},
		"vision, the user's false stays":    {true, "[features]\nview_image = false\n", false},
		"vision, the user's true stays":     {true, "[features]\nview_image = true\n", true},
		"no vision, true becomes false":     {false, "[features]\nview_image = true\n", false},
		"no vision, false stays":            {false, "[features]\nview_image = false\n", false},
		"no vision, other features stay":    {false, "[features]\nmulti_agent = false\n", false},
		"vision, other features stay alone": {true, "[features]\nmulti_agent = false\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			if tc.had != "" {
				write(t, profile, tc.had)
			}
			req := Request{BaseURL: testRoot, Default: "m", Models: []Model{{ID: "m", Context: 1048576, Vision: tc.vision}}}
			codexSetup(t, env, req)
			doc := codexDecodeDoc(t, mustRead(t, profile))
			if got, has := doc.Features["view_image"]; got != tc.want || has != (tc.want != nil) {
				t.Errorf("view_image = %v (present %v), want %v", got, has, tc.want)
			}
			if strings.Contains(tc.had, "multi_agent") && doc.Features["multi_agent"] != false {
				t.Errorf("the user's feature is gone: %v", doc.Features)
			}
		})
	}
}

func TestCodexModel(t *testing.T) {
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	model := func() string { return codexDecodeDoc(t, mustRead(t, profile)).Model }
	req := testRequest()

	// A model the user chose and EVREN offers stays; the window follows it.
	write(t, profile, "model = \"qwen3-vl-30b\"\n")
	p := codexSetup(t, env, req)
	if model() != "qwen3-vl-30b" || hasNote(p, "setup.note.codex_model_replaced") {
		t.Errorf("model = %q, notes %v", model(), p.Notes)
	}
	if w := codexDecodeDoc(t, mustRead(t, profile)).ContextWindow; w == nil || *w != 262144 {
		t.Errorf("model_context_window = %v", w)
	}

	// --model sets it, without a note about a replaced model.
	req.Default, req.ModelGiven = "deepseek-v4-flash", true
	p = codexSetup(t, env, req)
	if model() != "deepseek-v4-flash" || hasNote(p, "setup.note.codex_model_replaced") {
		t.Errorf("model = %q, notes %v", model(), p.Notes)
	}
	if v := codexDecodeDoc(t, mustRead(t, profile)).Features["view_image"]; v != false {
		t.Errorf("view_image = %v for a model without vision", v)
	}

	// A model EVREN does not offer cannot work: it is replaced by the
	// default, and a note names both.
	for _, had := range []string{"gpt-6", ""} {
		write(t, profile, "model = "+strconv.Quote(had)+"\nmodel_reasoning_effort = \"high\"\n")
		req = testRequest()
		p = codexSetup(t, env, req)
		var note *Note
		for i := range p.Notes {
			if p.Notes[i].Key == "setup.note.codex_model_replaced" {
				note = &p.Notes[i]
			}
		}
		if model() != "deepseek-v4.1-flash" || note == nil || len(note.Args) != 2 || note.Args[0] != had || note.Args[1] != "deepseek-v4.1-flash" {
			t.Errorf("had %q: model = %q, notes %v", had, model(), p.Notes)
		}
	}

	// A request whose default is not among its models is a caller's mistake.
	// It is an error where the default is needed: the profile names no
	// model that EVREN offers, or --model was given.
	req = testRequest()
	req.Default = "not-listed"
	if p, err := (Codex{}).Plan(testEnv(t, nil, nil), req); err == nil || len(p.Files) != 0 {
		t.Errorf("an unlisted default on a fresh machine: %v, %v", paths(p), err)
	}
	req.ModelGiven = true
	if p, err := (Codex{}).Plan(env, req); err == nil || len(p.Files) != 0 {
		t.Errorf("an unlisted --model: %v, %v", paths(p), err)
	}
}

// A profile that already says everything is left alone, byte for byte:
// the user's comments and key order survive.
func TestCodexNoChangeNoWrite(t *testing.T) {
	const instructions = `"Start Codex with \"ulgen run codex\", or put the key in your shell first: export EVREN_API_KEY=\"$(ulgen token)\""`
	for name, content := range map[string]string{
		"Ülgen's own file with a comment of the user's": strings.Replace(codexProfileFresh, "model = ", "# I like this one\nmodel = ", 1) + "\n# and a last word\n",
		"another key order, another spelling": "[model_providers.evren]\nwire_api = 'responses'\nenv_key_instructions = " + instructions +
			"\nname = \"EVREN\"\nenv_key = \"EVREN_API_KEY\" # the key's variable\nbase_url = \"https://evren-llmapi.ssyz.org.tr/v1\"\n\n[notice]\nhide = true\n",
		"inline, with keys of the user's": "web_search = \"disabled\"\nmodel_reasoning_effort = \"high\"\nmodel_provider = \"evren\"\nmodel = \"qwen3-vl-30b\"\nmodel_context_window = 100000\n" +
			"model_providers = { evren = { name = \"EVREN\", base_url = \"https://evren-llmapi.ssyz.org.tr/v1\", wire_api = \"responses\", env_key = \"EVREN_API_KEY\", request_max_retries = 2, env_key_instructions = " + instructions + " } }\n",
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			if strings.HasPrefix(content, "[model_providers.evren]") {
				content = "web_search = \"disabled\"\nmodel_provider = \"evren\"\nmodel = \"deepseek-v4.1-flash\"\n\n" + content
			}
			write(t, profile, content)
			p, err := Codex{}.Plan(env, testRequest())
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Files) != 0 || hasNote(p, "setup.note.codex_rewritten") {
				t.Errorf("the plan writes %v, notes %v", paths(p), p.Notes)
			}
			if !hasNote(p, "setup.note.codex_profile") || !hasNote(p, "setup.note.export") {
				t.Errorf("notes = %v", p.Notes)
			}
			if ok, err := (Codex{}).Configured(env); !ok || err != nil {
				t.Errorf("Configured = %v, %v", ok, err)
			}
		})
	}
}

func TestCodexRewriteKeepsKeysAndSaysSo(t *testing.T) {
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	write(t, profile, "# mine\nmodel = \"qwen3-vl-30b\"\nmodel_reasoning_effort = \"high\"\n\n[features]\nmulti_agent = false\n")
	p, err := Codex{}.Plan(env, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := strings.SplitAfter(codexProfileFresh, "env_key_instructions.\n\n")[0] + `model = "qwen3-vl-30b"
model_context_window = 262144
model_provider = "evren"
model_reasoning_effort = "high"
web_search = "disabled"

[features]
multi_agent = false

[model_providers]
[model_providers.evren]
base_url = "https://evren-llmapi.ssyz.org.tr/v1"
env_key = "EVREN_API_KEY"
env_key_instructions = "Start Codex with \"ulgen run codex\", or put the key in your shell first: export EVREN_API_KEY=\"$(ulgen token)\""
name = "EVREN"
wire_api = "responses"
`
	if got := planned(t, p, profile); got != want {
		t.Errorf("evren.config.toml:\n%s", got)
	}
	var note *Note
	for i := range p.Notes {
		if p.Notes[i].Key == "setup.note.codex_rewritten" {
			note = &p.Notes[i]
		}
	}
	// The file is named as a path, so the command shows it like every other.
	if note == nil || len(note.Args) != 1 || note.Args[0] != Path(profile) {
		t.Errorf("notes = %v", p.Notes)
	}
	applyAndReplan(t, Codex{}, env, testRequest(), p)

	// Another API root replaces Ülgen's value and nothing else.
	req := testRequest()
	req.BaseURL = "http://127.0.0.1:4141/v1"
	if p, err = (Codex{}).Plan(env, req); err != nil {
		t.Fatal(err)
	}
	if got, want := planned(t, p, profile), strings.Replace(want, testRoot, "http://127.0.0.1:4141/v1", 1); got != want {
		t.Errorf("evren.config.toml:\n%s", got)
	}
}

// B6: the note about the rewrite and the profile file's own header say that
// comments are not kept, and neither is the layout, while every value stays.
// The note for a replaced model says the model is not among the EVREN models
// Ülgen can set up. Both in English and Turkish.
func TestCodexRewriteAndModelNotesSayWhatIsNotKept(t *testing.T) {
	// What the note must name, per language: comments, layout, key order,
	// inline tables, and that every value stays.
	words := map[string][]string{
		"en": {"comment", "layout", "order of the keys", "inline tables", "every value"},
		"tr": {"yorum", "yerleşim", "anahtarların sırası", "satır içi tablolar", "her değeri"},
	}
	for _, lang := range i18n.Supported() {
		tr := i18n.New(lang)
		rewritten := tr.T("setup.note.codex_rewritten", "/home/x/.codex/evren.config.toml")
		if strings.Contains(rewritten, "%!") || !strings.Contains(rewritten, "/home/x/.codex/evren.config.toml") {
			t.Errorf("%s: note = %q", lang, rewritten)
		}
		lower := strings.ToLower(rewritten)
		for _, want := range words[lang] {
			if !strings.Contains(lower, want) {
				t.Errorf("%s: note does not name %q: %q", lang, want, rewritten)
			}
		}
		replaced := tr.T("setup.note.codex_model_replaced", "gpt-6", `"deepseek-v4.1-flash"`)
		if strings.Contains(replaced, "%!") || !strings.Contains(replaced, "gpt-6") || !strings.Contains(replaced, "deepseek-v4.1-flash") {
			t.Errorf("%s: note = %q", lang, replaced)
		}
		if strings.Contains(replaced, "does not offer") || strings.Contains(replaced, "sunmuyor") {
			t.Errorf("%s: note still says EVREN does not offer the model: %q", lang, replaced)
		}
		if !strings.Contains(strings.ToLower(replaced), "ulgen") {
			t.Errorf("%s: note does not name ulgen: %q", lang, replaced)
		}
	}
	// The header comment in the file itself carries the same two statements.
	header := codexProfileHeader
	for _, want := range []string{"comments are not", "layout", "every value", "inline"} {
		if !strings.Contains(header, want) {
			t.Errorf("the header does not say %q:\n%s", want, header)
		}
	}
}

func TestCodexBaseURL(t *testing.T) {
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	for name, root := range map[string]string{
		"newline":         "https://x.example/v1\n",
		"carriage return": "https://x.example/\rv1",
		"tab":             "https://x.example/\tv1",
		"DEL":             "https://x.example/v1\x7f",
		"NUL":             "https://x.example/\x00v1",
		"escape":          "https://x.example/\x1bv1",
		"C1 control":      "https://x.example/\u0085v1",
		"invalid UTF-8":   "https://x.example/\xff",
		"half a rune":     "https://x.example/\xc3",
	} {
		req := testRequest()
		req.BaseURL = root
		p, err := Codex{}.Plan(env, req)
		var re *RequestError
		if !errors.As(err, &re) || len(p.Files) != 0 {
			t.Errorf("%s: Plan = %v, %T %v", name, paths(p), err, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(profile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("something was written: %v", err)
	}
	// Whatever else a root holds is the encoder's to write.
	for _, root := range []string{`https://x.example/"v1"`, `https://x.example/\v1`, "https://örnek.example/v1", "https://x.example/\u2028v1", `https://x.example/\u0041`, "https://x.example/'''v1"} {
		req := testRequest()
		req.BaseURL = root
		p, err := Codex{}.Plan(env, req)
		if err != nil {
			t.Errorf("%q: %v", root, err)
			continue
		}
		if got := codexDecodeDoc(t, planned(t, p, profile)).Providers["evren"].BaseURL; got != root {
			t.Errorf("base_url read back as %q, want %q", got, root)
		}
	}
}

func TestCodexShellHintFollowsTheSystem(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":   `export EVREN_API_KEY="$(ulgen token)"`,
		"darwin":  `export EVREN_API_KEY="$(ulgen token)"`,
		"windows": `$env:EVREN_API_KEY = (ulgen token)`,
	} {
		env := testEnv(t, nil, nil)
		env.GOOS = goos
		_, profile := codexPaths(env)
		p := codexSetup(t, env, testRequest())
		got := codexDecodeDoc(t, mustRead(t, profile)).Providers["evren"].KeyInstructions
		if !strings.HasSuffix(got, ": "+want) || !strings.HasPrefix(got, `Start Codex with "ulgen run codex"`) {
			t.Errorf("%s: env_key_instructions = %q", goos, got)
		}
		// The note about the shell carries the same command as its argument.
		found := false
		for _, n := range p.Notes {
			if n.Key == "setup.note.export" {
				found = len(n.Args) == 1 && n.Args[0] == want
			}
		}
		if !found {
			t.Errorf("%s: no setup.note.export note with %q; notes %v", goos, want, p.Notes)
		}
	}
}

func TestCodexLaunch(t *testing.T) {
	added := func(args ...string) []string { return append([]string{"--profile", "evren"}, args...) }
	for _, tc := range []struct {
		args []string
		add  bool
	}{
		// Nothing selects a profile and Codex takes the option.
		{nil, true},
		{[]string{"fix the bug"}, true},
		{[]string{"features of this repository"}, true}, // one word of a prompt, not the subcommand
		{[]string{"exec", "fix the bug"}, true},
		{[]string{"e", "fix the bug"}, true},
		{[]string{"review"}, true},
		{[]string{"resume", "--last"}, true},
		{[]string{"queue", "x"}, true},
		{[]string{"archive", "x"}, true},
		{[]string{"delete", "x"}, true},
		{[]string{"unarchive", "x"}, true},
		{[]string{"fork"}, true},
		{[]string{"agents"}, true},
		{[]string{"mcp", "list"}, true},
		{[]string{"sandbox", "macos", "--", "ls"}, true},
		{[]string{"debug", "prompt-input", "hi"}, true},
		// B3: an option that takes a value between "debug" and its subcommand
		// must not hide the subcommand.
		{[]string{"debug", "-c", "a=1", "prompt-input", "hi"}, true},
		{[]string{"debug", "--config", "a=1", "prompt-input", "hi"}, true},
		{[]string{"debug", "--config=a=1", "prompt-input", "hi"}, true},
		{[]string{"debug", "-m", "glm-5.3", "prompt-input", "hi"}, true},
		{[]string{"-c", "a=1", "debug", "prompt-input", "hi"}, true},
		// An option with a value where the subcommand goes: not "prompt-input".
		{[]string{"debug", "-c", "prompt-input"}, false},
		{[]string{"-m", "glm-5.3", "exec", "hi"}, true},
		{[]string{"--search", "exec", "hi"}, true},
		{[]string{"-m", "login"}, true},                  // "login" is the model's name here
		{[]string{"-i", "a.png", "login"}, true},         // and an image here: -i takes every word after it
		{[]string{"--image", "a.png", "features"}, true}, // the same
		{[]string{"--some-new-option", "login"}, true},   // unknown to Ülgen: Codex itself says no if it must
		{[]string{"-x", "login"}, true},

		// The arguments select a profile.
		{[]string{"--profile", "work", "exec", "hi"}, false},
		{[]string{"-p", "work"}, false},
		{[]string{"--profile=work"}, false},
		{[]string{"-pwork", "exec", "hi"}, false},
		{[]string{"-p=work"}, false},
		{[]string{"exec", "-p", "work", "hi"}, false},
		{[]string{"exec", "--profile=work", "--", "-x"}, false},

		// After "--" nothing is an option.
		{[]string{"exec", "--", "-p"}, true},
		{[]string{"exec", "--", "--profile", "work"}, true},
		{[]string{"--", "--profile=work"}, true},
		{[]string{"--", "-pwork"}, true},
		{[]string{"--", "login"}, true}, // a prompt

		// Subcommands that refuse the option.
		{[]string{"features", "list"}, false},
		{[]string{"completion", "zsh"}, false},
		{[]string{"login", "status"}, false},
		{[]string{"logout"}, false},
		{[]string{"apply", "x"}, false},
		{[]string{"a", "x"}, false},
		{[]string{"plugin", "list"}, false},
		{[]string{"app-server"}, false},
		{[]string{"remote-control"}, false},
		{[]string{"app"}, false},
		{[]string{"update"}, false},
		{[]string{"doctor"}, false},
		{[]string{"migrate-rollouts"}, false},
		{[]string{"cloud"}, false},
		{[]string{"cloud-tasks"}, false},
		{[]string{"exec-server"}, false},
		{[]string{"execpolicy", "check"}, false},
		{[]string{"debug", "models"}, false},
		{[]string{"debug"}, false},
		{[]string{"-m", "glm-5.3", "features", "list"}, false},
		{[]string{"--model=glm-5.3", "features", "list"}, false},
		{[]string{"-mglm-5.3", "features", "list"}, false},
		{[]string{"-c", "model=\"x\"", "login", "status"}, false},
		{[]string{"--search", "--no-alt-screen", "login"}, false},
		{[]string{"-C", "/work", "--add-dir", "/data", "-a", "never", "-s", "read-only", "doctor"}, false},
		{[]string{"--enable", "x", "--disable", "y", "--remote", "ws://h:1", "--remote-auth-token-env", "T", "--local-provider", "ollama", "features"}, false},
		{[]string{"-i", "a.png", "-m", "glm-5.3", "login", "status"}, false}, // an option ends the images
		{[]string{"--image=a.png", "login", "status"}, false},
		{[]string{"-ia.png", "login", "status"}, false},
	} {
		want := tc.args
		if tc.add {
			want = added(tc.args...)
		}
		in := append([]string(nil), tc.args...)
		got := Codex{}.Launch(Env{}, in)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("Launch(%q) = %q, want %q", tc.args, got, want)
		}
		if strings.Join(in, "\x00") != strings.Join(tc.args, "\x00") {
			t.Errorf("Launch changed its argument: %q", in)
		}
	}
}

func TestCodexDir(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	file := write(t, filepath.Join(other, "a-file"), "x")
	missing := filepath.Join(other, "nope")
	envWith := func(home, codexHome string) Env {
		env := testEnv(t, map[string]string{"CODEX_HOME": codexHome}, nil)
		env.Home = home
		return env
	}
	// The real path of a directory that is there. A temporary directory may
	// itself lie under a link (/tmp is one on macOS), and codexDir returns the
	// path with every link resolved.
	realOf := func(dir string) string {
		t.Helper()
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Clean(real)
	}

	if dir, err := codexDir(envWith(home, "")); err != nil || dir != filepath.Join(home, ".codex") {
		t.Errorf("no CODEX_HOME: %q, %v", dir, err)
	}
	if dir, err := codexDir(envWith(home, other)); err != nil || dir != realOf(other) {
		t.Errorf("CODEX_HOME: %q, %v", dir, err)
	}
	if dir, err := codexDir(envWith("", other)); err != nil || dir != realOf(other) {
		t.Errorf("CODEX_HOME without a home directory: %q, %v", dir, err)
	}

	// A CODEX_HOME that is a symbolic link to a directory gives the real
	// directory: every file under it is reached by its real name, not through
	// the link. Skipped where a link cannot be made (no privilege on Windows).
	linkDir := t.TempDir()
	target := filepath.Join(linkDir, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Logf("a symbolic link cannot be made here, so this case is skipped: %v", err)
	} else {
		real, err := filepath.EvalSymlinks(target)
		if err != nil {
			t.Fatal(err)
		}
		if dir, err := codexDir(envWith(home, link)); err != nil || dir != filepath.Clean(real) {
			t.Errorf("CODEX_HOME is a symbolic link: %q, %v", dir, err)
		}
	}

	// Codex does not trim the value, so Ülgen does not either: the value is
	// used as it is, and the system says whether it names a directory. That
	// answer differs between systems for a value with a space at its end.
	var me *MissingDirError
	spaced := other + " "
	dir, err := codexDir(envWith(home, spaced))
	if runtime.GOOS == "windows" {
		// Windows drops a space at the end of a path component, so "dir "
		// names the directory "dir" that is there, for Codex as for every
		// program. The adapter does not second-guess that: what it returns is
		// the real path of that directory, which is the directory without the
		// space, so every file it then builds from it is one in it.
		real, rerr := filepath.EvalSymlinks(spaced)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if err != nil || dir != filepath.Clean(real) || strings.HasSuffix(dir, " ") {
			t.Errorf("CODEX_HOME with a space at the end: %q, %v", dir, err)
		}
	} else {
		// Everywhere else "dir " is another name. No such directory is there
		// yet, so it is missing; once it is, it is the one that is used.
		if !errors.As(err, &me) || me.Var != "CODEX_HOME" || me.Path != spaced {
			t.Errorf("CODEX_HOME with a space at the end: %q, %v", dir, err)
		}
		if err := os.Mkdir(spaced, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(spaced) })
		if dir, err := codexDir(envWith(home, spaced)); err != nil || dir != realOf(spaced) {
			t.Errorf("a directory whose name ends in a space: %q, %v", dir, err)
		}
	}

	// Missing: the typed error, and the directory is not created.
	env := envWith(home, missing)
	_, err = Codex{}.Plan(env, testRequest())
	if !errors.As(err, &me) || me.Var != "CODEX_HOME" || me.Path != missing {
		t.Errorf("a missing CODEX_HOME: %v", err)
	}
	if _, statErr := os.Stat(missing); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the directory was created: %v", statErr)
	}
	if ok, err := (Codex{}).Configured(env); ok || err != nil {
		t.Errorf("Configured with a missing CODEX_HOME = %v, %v", ok, err)
	}

	// A file is "not a directory", not "does not exist".
	var ke *KindError
	_, err = codexDir(envWith(home, file))
	if errors.As(err, &me) || !errors.As(err, &ke) || ke.Path != file || ke.Var != "CODEX_HOME" || !ke.WantDir {
		t.Errorf("CODEX_HOME is a file: %T %v", err, err)
	}
	if ok, err := (Codex{}).Configured(envWith(home, file)); ok || !errors.As(err, &ke) {
		t.Errorf("CODEX_HOME is a file: Configured = %v, %T %v", ok, err, err)
	}

	// A relative value is refused even when such a directory is there.
	t.Chdir(other)
	if err := os.Mkdir("rel", 0o700); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"rel", "./rel", ".", " "} {
		env := envWith(home, rel)
		_, err := codexDir(env)
		var re *RelativePathError
		if !errors.As(err, &re) || re.Var != "CODEX_HOME" || re.Value != rel {
			t.Errorf("CODEX_HOME=%q: %T %v", rel, err, err)
		}
		if p, err := (Codex{}).Plan(env, testRequest()); err == nil || len(p.Files) != 0 {
			t.Errorf("CODEX_HOME=%q: Plan = %v, %v", rel, paths(p), err)
		}
		if ok, err := (Codex{}).Configured(env); ok || err == nil {
			t.Errorf("CODEX_HOME=%q: Configured = %v, %v", rel, ok, err)
		}
	}

	// No home directory and no CODEX_HOME: an error, never a path under the
	// working directory.
	for _, noHome := range []string{"", "relative-home"} {
		env := envWith(noHome, "")
		var he *HomeError
		if dir, err := codexDir(env); !errors.As(err, &he) || he.Home != noHome {
			t.Errorf("home %q: codexDir = %q, %T %v", noHome, dir, err, err)
		}
		if p, err := (Codex{}).Plan(env, testRequest()); !errors.As(err, &he) || len(p.Files) != 0 {
			t.Errorf("home %q: Plan = %v, %v", noHome, paths(p), err)
		}
		if ok, err := (Codex{}).Configured(env); ok || !errors.As(err, &he) {
			t.Errorf("home %q: Configured = %v, %v", noHome, ok, err)
		}
	}
	if entries, err := os.ReadDir(other); err != nil || len(entries) != 2 { // a-file and rel
		t.Errorf("the working directory holds %v, %v", entries, err)
	}
}

func TestCodexHomeIsUsed(t *testing.T) {
	other := t.TempDir()
	// codexDir returns the real path of the directory, with every link
	// resolved, so the file paths are the real directory's.
	real, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}
	real = filepath.Clean(real)
	env := testEnv(t, map[string]string{"CODEX_HOME": other}, nil)
	p := codexSetup(t, env, testRequest())
	if len(p.Files) != 1 || p.Files[0].Path != filepath.Join(real, "evren.config.toml") {
		t.Errorf("the plan writes %v", paths(p))
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".codex")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("~/.codex was created: %v", err)
	}
	// A foreign provider there is seen there.
	write(t, filepath.Join(other, "config.toml"), "[model_providers.evren]\nname = \"mine\"\n")
	var fe *ForeignEntryError
	if _, err := (Codex{}).Plan(env, testRequest()); !errors.As(err, &fe) || fe.Path != filepath.Join(real, "config.toml") || fe.Entry != "model_providers.evren" {
		t.Errorf("err = %v", err)
	}
}

func TestCodexConfigured(t *testing.T) {
	const table = "\n[model_providers.evren]\nname = \"EVREN\"\nbase_url = \"https://evren-llmapi.ssyz.org.tr/v1\"\nenv_key = \"EVREN_API_KEY\"\n"
	for name, tc := range map[string]struct {
		profile string
		want    bool
	}{
		"complete":                       {"model_provider = \"evren\"\n" + table, true},
		"inline spelling":                {"model_provider = \"evren\"\nmodel_providers = { evren = { env_key = \"EVREN_API_KEY\" } }\n", true},
		"an empty file":                  {"", false},
		"no provider selected":           {table, false},
		"another provider selected":      {"model_provider = \"openai\"\n" + table, false},
		"no provider table":              {"model_provider = \"evren\"\n", false},
		"another provider's table":       {"model_provider = \"evren\"\n\n[model_providers.other]\nenv_key = \"EVREN_API_KEY\"\n", false},
		"the table reads another key":    {"model_provider = \"evren\"\n\n[model_providers.evren]\nname = \"EVREN\"\nenv_key = \"OTHER_KEY\"\n", false},
		"the table reads no key":         {"model_provider = \"evren\"\n\n[model_providers.evren]\nname = \"EVREN\"\n", false},
		"the key's name is not a string": {"model_provider = \"evren\"\n\n[model_providers.evren]\nenv_key = 5\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, tc.profile)
			if ok, err := (Codex{}).Configured(env); ok != tc.want || err != nil {
				t.Errorf("Configured = %v, %v", ok, err)
			}
		})
	}

	// The provider in config.toml alone, as the prototype wrote it, is not
	// "set up": the profile file is what counts.
	env := testEnv(t, nil, nil)
	config, profile := codexPaths(env)
	write(t, config, "[model_providers.other]\nname = \"O\"\n")
	if ok, err := (Codex{}).Configured(env); ok || err != nil {
		t.Errorf("without a profile file: Configured = %v, %v", ok, err)
	}

	// A profile path that is a directory is an error, not "not set up".
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	var se *ShapeError
	if ok, err := (Codex{}).Configured(env); ok || !errors.As(err, &se) || se.Path != profile || !strings.Contains(se.Detail, "directory") {
		t.Errorf("the profile is a directory: Configured = %v, %T %v", ok, err, err)
	}
	if p, err := (Codex{}).Plan(env, testRequest()); !errors.As(err, &se) || len(p.Files) != 0 {
		t.Errorf("the profile is a directory: Plan = %v, %v", paths(p), err)
	}

	// So is a profile that cannot be looked at for another reason than
	// "does not exist": a symbolic link that leads to itself, and a Codex
	// directory that is a file.
	if runtime.GOOS != "windows" { // which needs a privilege for links and reports the second as "does not exist"
		env = testEnv(t, nil, nil)
		config, profile = codexPaths(env)
		write(t, config, "model = \"gpt-6\"\n")
		if err := os.Symlink(profile, profile); err != nil {
			t.Fatal(err)
		}
		if ok, err := (Codex{}).Configured(env); ok || err == nil || errors.Is(err, os.ErrNotExist) {
			t.Errorf("the profile is a link to itself: Configured = %v, %v", ok, err)
		}
		if p, err := (Codex{}).Plan(env, testRequest()); err == nil || len(p.Files) != 0 {
			t.Errorf("the profile is a link to itself: Plan = %v, %v", paths(p), err)
		}

		env = testEnv(t, nil, nil)
		write(t, filepath.Join(env.Home, ".codex"), "not a directory")
		if ok, err := (Codex{}).Configured(env); ok || err == nil {
			t.Errorf("~/.codex is a file: Configured = %v, %v", ok, err)
		}
		if p, err := (Codex{}).Plan(env, testRequest()); err == nil || len(p.Files) != 0 {
			t.Errorf("~/.codex is a file: Plan = %v, %v", paths(p), err)
		}
	}
}

func TestCodexDetectAndNames(t *testing.T) {
	env := testEnv(t, nil, map[string]string{"codex": "codex-cli 0.162.1\n"})
	d := Codex{}.Detect(t.Context(), env)
	// The fixture's path is absolute on the system the test runs on (see
	// testBin); a path written with "/" here would not be one on Windows.
	if !d.Installed || d.Binary != testBin("codex") || d.Version != "0.162.1" {
		t.Errorf("Detect = %+v", d)
	}
	if d := (Codex{}).Detect(t.Context(), testEnv(t, nil, nil)); d.Installed {
		t.Errorf("Detect without the binary = %+v", d)
	}
	if (Codex{}).ID() != "codex" || (Codex{}).Name() != "Codex CLI" || (Codex{}).InstallHint(env).Key != "setup.install.codex" {
		t.Error("ID, Name or InstallHint changed")
	}
}

// The key itself is never in a request, so it cannot reach a file; the
// profile names the variable and nothing else.
func TestCodexWritesOnlyTheNameOfTheKey(t *testing.T) {
	env := testEnv(t, map[string]string{"EVREN_API_KEY": "sk-secret-value"}, nil)
	_, profile := codexPaths(env)
	p := codexSetup(t, env, testRequest())
	if got := mustRead(t, profile); strings.Contains(got, "sk-secret-value") || !strings.Contains(got, `env_key = "EVREN_API_KEY"`) {
		t.Errorf("evren.config.toml:\n%s", got)
	}
	if bytes.Contains([]byte(fmt.Sprint(p.Notes)), []byte("sk-secret-value")) {
		t.Errorf("notes = %v", p.Notes)
	}
}

// The check behind every write: text that would not decode to what was
// intended is an error that names the key, never a file.
func TestCodexEncodeRefusesAChangedMeaning(t *testing.T) {
	const path = "/somewhere/evren.config.toml"
	for name, tc := range map[string]struct {
		profile map[string]any
		detail  string
	}{
		// The encoder writes a duration as a string.
		"a value the encoder changes":   {map[string]any{"model": "m", "wait": 90 * time.Second}, "the value of wait cannot"},
		"the same inside a table":       {map[string]any{"t": map[string]any{"u": map[string]any{"wait": time.Second}}}, "the value of t.u.wait cannot"},
		"the same inside an array":      {map[string]any{"list": []any{int64(1), time.Second}}, "the value of list cannot"},
		"a value the encoder refuses":   {map[string]any{"list": []any{nil}}, "cannot be written as TOML"},
		"text the decoder cannot read":  {map[string]any{"s": "\xff"}, "cannot be written as TOML"},
		"a number of another Go type":   {map[string]any{"n": uint8(7)}, "the value of n cannot"},
		"a nil value the encoder skips": {map[string]any{"none": nil}, "the value of none cannot"},
	} {
		data, err := codexEncode(path, tc.profile)
		var se *ShapeError
		if !errors.As(err, &se) || se.Path != path || !strings.Contains(se.Detail, tc.detail) || data != nil {
			t.Errorf("%s: %q, %T %v", name, data, err, err)
		}
	}
	// What the decoder produces goes through, in any zone.
	profile := codexDecode(t, codexValueTypes+"\n[when]\nday = 2026-10-12\nat = 09:00:00\nboth = 2026-10-12T09:00:00\n")
	data, err := codexEncode(path, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := codexCanon(codexDecode(t, string(data))); got != codexCanon(profile) {
		t.Errorf("decoded\n got: %s\nwant: %s", got, codexCanon(profile))
	}
}

// Strings are the encoder's to write: a model id that holds TOML syntax
// stays one value.
func TestCodexModelIDIsEscaped(t *testing.T) {
	env := testEnv(t, nil, nil)
	_, profile := codexPaths(env)
	const id = "x\"\ninjected = true\n[features]\nview_image = \"no\" # '''"
	req := Request{BaseURL: testRoot, Default: id, Models: []Model{{ID: id, Context: 1048576, Vision: true}}}
	codexSetup(t, env, req)
	doc := codexDecode(t, mustRead(t, profile))
	if doc["model"] != id || len(doc) != 4 { // model, model_provider, web_search, model_providers
		t.Errorf("decoded profile: %s", codexCanon(doc))
	}
}

// Every note the adapter gives has a message in both languages, and the
// message takes the note's arguments.
func TestCodexNotesAreTranslated(t *testing.T) {
	var notes []Note
	collect := func(goos, profile string, makeDefault bool) {
		env := testEnv(t, nil, nil)
		env.GOOS = goos
		if profile != "" {
			_, path := codexPaths(env)
			write(t, path, profile)
		}
		req := testRequest()
		req.MakeDefault = makeDefault
		p, err := Codex{}.Plan(env, req)
		if err != nil {
			t.Fatal(err)
		}
		notes = append(notes, p.Notes...)
	}
	collect("linux", "", true)
	collect("windows", "model = \"gpt-6\"\nmodel_context_window = 64000\n", false)
	notes = append(notes, Codex{}.InstallHint(Env{}))
	seen := map[string]bool{}
	for _, n := range notes {
		seen[n.Key] = true
		for _, lang := range i18n.Supported() {
			msg := i18n.New(lang).T(n.Key, n.Args...)
			if msg == n.Key || strings.Contains(msg, "%!") || (lang != "en" && msg == i18n.New("en").T(n.Key, n.Args...)) {
				t.Errorf("%s in %s: %q", n.Key, lang, msg)
			}
		}
	}
	for _, key := range []string{
		"setup.note.export", "setup.note.codex_profile", "setup.note.codex_default",
		"setup.note.codex_rewritten", "setup.note.codex_model_replaced", "setup.note.codex_followed_model", "setup.install.codex",
	} {
		if !seen[key] {
			t.Errorf("no plan gave the note %s", key)
		}
	}
}

// A restriction belongs to a model. When setup changes the profile's model
// (--model, or the old one is not EVREN's), the window and the image switch
// are set for the new model whatever the file held, and a note names each
// value that was dropped or raised by that. When the model stays, a lower
// window and a false of the user's stay too.
func TestCodexLimitsFollowAModelChange(t *testing.T) {
	models := []Model{
		{ID: "small-blind", Context: 100000},
		{ID: "tiny-blind", Context: 50000},
		{ID: "mid-seeing", Context: 200000, Vision: true},
		{ID: "large-seeing", Context: 1048576, Vision: true},
		{ID: "large-blind", Context: 1048576},
	}
	const (
		none      = int64(-1) // model_context_window is absent
		window    = "model_context_window"
		viewImage = "[features] view_image"
	)
	for name, tc := range map[string]struct {
		had      string   // the profile before
		model    string   // --model; "" when it is not given (the default is large-seeing)
		want     string   // the model afterwards
		window   int64    // model_context_window afterwards
		image    any      // view_image afterwards; nil when absent
		followed []string // the keys the notes name
	}{
		// The model changes.
		"to a larger model: the window is raised, images are allowed again": {
			"model = \"small-blind\"\nmodel_context_window = 100000\n\n[features]\nview_image = false\n", "mid-seeing", "mid-seeing", 200000, nil, []string{window, viewImage}},
		"to a model that needs no window: the key goes": {
			"model = \"small-blind\"\nmodel_context_window = 100000\n\n[features]\nview_image = false\n", "large-blind", "large-blind", none, false, []string{window}},
		"to a smaller model: lowered, which needs no note": {
			"model = \"mid-seeing\"\nmodel_context_window = 200000\n", "small-blind", "small-blind", 100000, false, nil},
		"the user's lower window does not outlive the model": {
			"model = \"mid-seeing\"\nmodel_context_window = 150000\n", "large-seeing", "large-seeing", none, nil, []string{window}},
		"the user's lower window is raised to the new model's": {
			"model = \"small-blind\"\nmodel_context_window = 80000\n", "mid-seeing", "mid-seeing", 200000, nil, []string{window}},
		"lower than the new model's as well: still the new model's": {
			"model = \"mid-seeing\"\nmodel_context_window = 20000\n", "tiny-blind", "tiny-blind", 50000, false, []string{window}},
		"above the new model's: lowered, no note": {
			"model = \"mid-seeing\"\nmodel_context_window = 150000\n", "small-blind", "small-blind", 100000, false, nil},
		"a model EVREN does not offer gives way to the default": {
			"model = \"gpt-6\"\nmodel_context_window = 64000\n\n[features]\nview_image = false\nmulti_agent = false\n", "", "large-seeing", none, nil, []string{window, viewImage}},
		"a true is dropped like a false": {
			"model = \"mid-seeing\"\n\n[features]\nview_image = true\n", "large-seeing", "large-seeing", none, nil, []string{viewImage}},
		"to a model without vision: nothing is dropped": {
			"model = \"mid-seeing\"\n\n[features]\nview_image = false\n", "large-blind", "large-blind", none, false, nil},
		"a window of zero is not worth a note": {
			"model = \"mid-seeing\"\nmodel_context_window = 0\n", "large-seeing", "large-seeing", none, nil, nil},
		"nothing held, nothing to say": {
			"model = \"small-blind\"\n", "mid-seeing", "mid-seeing", 200000, nil, nil},

		// The model stays: the user's values stand.
		"--model names the model the profile has": {
			"model = \"mid-seeing\"\nmodel_context_window = 150000\n\n[features]\nview_image = false\n", "mid-seeing", "mid-seeing", 150000, false, nil},
		"no --model and a model EVREN offers": {
			"model = \"mid-seeing\"\nmodel_context_window = 150000\n\n[features]\nview_image = false\n", "", "mid-seeing", 150000, false, nil},
		"no model was named before, so none changes": {
			"model_context_window = 150000\n\n[features]\nview_image = false\n", "mid-seeing", "mid-seeing", 150000, false, nil},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, tc.had)
			req := Request{BaseURL: testRoot, Models: models, Default: "large-seeing"}
			if tc.model != "" {
				req.Default, req.ModelGiven = tc.model, true
			}
			p := codexSetup(t, env, req)
			doc := codexDecodeDoc(t, mustRead(t, profile))
			got := none
			if doc.ContextWindow != nil {
				got = *doc.ContextWindow
			}
			image, hasImage := doc.Features["view_image"]
			if doc.Model != tc.want || got != tc.window || image != tc.image || hasImage != (tc.image != nil) {
				t.Errorf("model %q, window %d, view_image %v (present %v)\n%s", doc.Model, got, image, hasImage, mustRead(t, profile))
			}
			var followed []string
			for _, n := range p.Notes {
				if n.Key == "setup.note.codex_followed_model" {
					if len(n.Args) != 1 {
						t.Fatalf("note arguments: %v", n.Args)
					}
					followed = append(followed, fmt.Sprint(n.Args[0]))
				}
			}
			if strings.Join(followed, "|") != strings.Join(tc.followed, "|") {
				t.Errorf("notes name %q, want %q", followed, tc.followed)
			}
			if strings.Contains(tc.had, "multi_agent") && doc.Features["multi_agent"] != false {
				t.Errorf("the user's feature is gone: %v", doc.Features)
			}
			if !strings.Contains(tc.had, "multi_agent") && tc.image == nil && doc.Features != nil {
				t.Errorf("an empty [features] table is left: %v", doc.Features)
			}
			if hasNote(p, "setup.note.codex_model_replaced") != (tc.model == "" && doc.Model != "mid-seeing") {
				t.Errorf("notes = %v", p.Notes)
			}
		})
	}
}

// B1: tomlDiff reports a difference even when it is under the empty key, and
// the caller goes by differs, not by the path.
func TestTomlDiffEmptyKey(t *testing.T) {
	for name, tc := range map[string]struct {
		a, b  map[string]any
		path  string
		diffs bool
	}{
		"the empty key alone":            {map[string]any{"": int64(1)}, map[string]any{"": int64(2)}, "", true},
		"the empty key and another":      {map[string]any{"": int64(1), "a": int64(1)}, map[string]any{"": int64(2), "a": int64(2)}, "", true},
		"only another key":               {map[string]any{"": int64(1), "a": int64(1)}, map[string]any{"": int64(1), "a": int64(2)}, "a", true},
		"nothing differs, empty key too": {map[string]any{"": int64(1)}, map[string]any{"": int64(1)}, "", false},
		"a nested empty key":             {map[string]any{"t": map[string]any{"": int64(1)}}, map[string]any{"t": map[string]any{"": int64(2)}}, "t.", true},
	} {
		path, differs := tomlDiff(tc.a, tc.b)
		if path != tc.path || differs != tc.diffs {
			t.Errorf("%s: tomlDiff = (%q, %v), want (%q, %v)", name, path, differs, tc.path, tc.diffs)
		}
	}
}

// B4: the comparison of the encoded profile with the intended one, and the
// "nothing to change" comparison, are strict about types: an integer is not
// a float of the same value, and a local date is not a local date-time at
// midnight.
func TestTomlComparisonIsStrictAboutTypes(t *testing.T) {
	dec := func(src string) map[string]any {
		m := map[string]any{}
		if _, err := toml.Decode(src, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	for name, tc := range map[string]struct{ a, b string }{
		"an integer and a float":           {"n = 1\n", "n = 1.0\n"},
		"a date and a date-time":           {"n = 2026-10-12\n", "n = 2026-10-12T00:00:00\n"},
		"a date-time and an offset one":    {"n = 2026-10-12T00:00:00\n", "n = 2026-10-12T00:00:00Z\n"},
		"a string and its number":          {"n = \"1\"\n", "n = 1\n"},
		"an array with an int and a float": {"n = [1]\n", "n = [1.0]\n"},
	} {
		a, b := dec(tc.a), dec(tc.b)
		if _, differs := tomlDiff(a, b); !differs {
			t.Errorf("%s: tomlDiff found no difference", name)
		}
		if tomlSame(a["n"], b["n"]) {
			t.Errorf("%s: tomlSame said the same", name)
		}
	}
}

// B4: a profile whose EVREN provider names another wire API or another key
// variable is corrected by Plan.
func TestCodexCorrectsTheProviderKeys(t *testing.T) {
	for name, content := range map[string]string{
		"wire_api chat":   "[model_providers.evren]\nwire_api = \"chat\"\n",
		"another env_key": "[model_providers.evren]\nenv_key = \"OTHER_KEY\"\n",
		"both wrong":      "model = \"qwen3-vl-30b\"\n\n[model_providers.evren]\nwire_api = \"chat\"\nenv_key = \"OTHER_KEY\"\nbase_url = \"https://old.example/v1\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, content)
			codexSetup(t, env, testRequest())
			got := codexDecodeDoc(t, mustRead(t, profile)).Providers["evren"]
			if got.WireAPI != "responses" || got.EnvKey != "EVREN_API_KEY" || got.BaseURL != testRoot {
				t.Errorf("the provider was not corrected: %+v", got)
			}
			if ok, err := (Codex{}).Configured(env); !ok || err != nil {
				t.Errorf("Configured = %v, %v", ok, err)
			}
		})
	}
}

// B2: a profile that holds an empty key anywhere is refused with a
// *ShapeError whose detail names the place; rewriting it could drop array
// items. Plan and Configured refuse alike, and the file is not touched.
func TestCodexRefusesAnEmptyKey(t *testing.T) {
	for name, tc := range map[string]struct{ content, place string }{
		"at the top level":            {"\"\" = 1\n", `""`},
		"an array with an empty key":  {"x = [1, {\"\" = 2}, 3]\n", `x.""`},
		"inside a table":              {"[t]\n\"\" = [1, {a = 2}, \"three\"]\n", `t.""`},
		"an inline table in an array": {"y = [{a = 1}, {\"\" = 2}]\n", `y.""`},
		"a nested empty key":          {"[a.b]\n\"\" = true\n", `a.b.""`},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			_, profile := codexPaths(env)
			write(t, profile, tc.content)
			var se *ShapeError
			err := codexRefused(t, env)
			if !errors.As(err, &se) || se.Path != profile || !strings.Contains(se.Detail, tc.place) {
				t.Errorf("err = %T %v (want place %q)", err, err, tc.place)
			}
			if got := mustRead(t, profile); got != tc.content {
				t.Errorf("the profile changed:\n%q", got)
			}
		})
	}
}

// B5: a profile file that is a symbolic link to the user's config.toml is
// refused: Ülgen writes the profile, so it would write config.toml.
func TestCodexRefusesAProfileLinkToConfig(t *testing.T) {
	env := testEnv(t, nil, nil)
	config, profile := codexPaths(env)
	write(t, config, "model = \"gpt-6\"\n")
	if err := os.Symlink(config, profile); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	var se *ShapeError
	err := codexRefused(t, env)
	if !errors.As(err, &se) || se.Path != profile || !strings.Contains(se.Detail, "config.toml") {
		t.Errorf("err = %T %v", err, err)
	}
	if info, err := os.Lstat(profile); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
	if got := mustRead(t, config); got != "model = \"gpt-6\"\n" {
		t.Errorf("config.toml changed:\n%q", got)
	}
}

// B7: the detail of the *ParseError keeps the line and a fixed description
// and never any text of the file, so a secret in an unparsable file is not
// echoed. Plan and Configured refuse alike, and the file is untouched.
func TestCodexParseErrorHidesTheFileText(t *testing.T) {
	const secret = "SENTINELvalue123"
	for name, tc := range map[string]struct{ content, want string }{
		"an unquoted secret":          {"api_key = " + secret + "\n", "expected value but found"},
		"a bad escape in a string":    {"api_key = \"x\\q" + secret + "\"\n", "invalid escape in string"},
		"a missing closing quote":     {"api_key = \"x" + secret + "\n", "strings cannot contain newlines"},
		"a missing closing quote eof": {"api_key = \"" + secret + "\n[table]\n", "strings cannot contain newlines"},
		"an empty value":              {"api_key = \n", "expected value but found"},
	} {
		for _, which := range []string{"config.toml", "evren.config.toml"} {
			t.Run(name+"/"+which, func(t *testing.T) {
				env := testEnv(t, nil, nil)
				config, profile := codexPaths(env)
				path := config
				if which == "evren.config.toml" {
					path = profile
				}
				write(t, path, tc.content)
				for _, call := range []struct {
					what string
					run  func() error
				}{
					{"Plan", func() error { _, err := Codex{}.Plan(env, testRequest()); return err }},
					{"Configured", func() error { _, err := Codex{}.Configured(env); return err }},
				} {
					var pe *ParseError
					err := call.run()
					if !errors.As(err, &pe) || pe.Format != "TOML" || pe.Path != path {
						t.Fatalf("%s: %T %v", call.what, err, err)
					}
					if strings.Contains(pe.Detail, secret) {
						t.Errorf("%s: the detail quotes the file: %q", call.what, pe.Detail)
					}
					if !strings.Contains(pe.Detail, tc.want) || !strings.Contains(pe.Detail, "line ") {
						t.Errorf("%s: detail = %q, want it to name %q and a line", call.what, pe.Detail, tc.want)
					}
				}
				if got := mustRead(t, path); got != tc.content {
					t.Errorf("the file changed:\n%q", got)
				}
			})
		}
	}
}

// The detail of a parse error never starts with ":" or a space: an error
// without a position (not the library's own) gives the description alone.
func TestCodexParseDetailHasNoLeadingSeparator(t *testing.T) {
	for _, err := range []error{
		errors.New("something else"),
		fmt.Errorf("wrapped: %w", errors.New("x")),
	} {
		got := codexParseDetail(err)
		if got != "the text does not parse" {
			t.Errorf("codexParseDetail(%v) = %q", err, got)
		}
	}
	for _, content := range []string{"api_key = \n", "a = [\n", "[a\n", "= 1\n", "a = 1\nb = 'x\n", "\xff = 1\n"} {
		env := testEnv(t, nil, nil)
		config, _ := codexPaths(env)
		write(t, config, content)
		_, err := Codex{}.Plan(env, testRequest())
		var pe *ParseError
		if !errors.As(err, &pe) {
			continue
		}
		if strings.HasPrefix(pe.Detail, ":") || strings.HasPrefix(pe.Detail, " ") || pe.Detail == "" {
			t.Errorf("%q: detail = %q", content, pe.Detail)
		}
	}
}

// The two refusals about config.toml have a message in both languages that
// shows the file and, for a legacy profile, what was found in it.
// The two refusals about config.toml have a message in both languages that
// shows the file and, for a legacy profile, what was found in it.
func TestCodexRefusalsAreTranslated(t *testing.T) {
	const path, key = "/home/x/.codex/config.toml", `profile = "work"`
	for _, lang := range i18n.Supported() {
		tr := i18n.New(lang)
		legacy := tr.T("setup.err.legacy_profile", path, key)
		foreign := tr.T("setup.err.foreign", path, "model_providers.evren")
		for _, msg := range []string{legacy, foreign} {
			if !strings.Contains(msg, path) || strings.Contains(msg, "%!") || !strings.Contains(msg, "ulgen setup") {
				t.Errorf("%s: %q", lang, msg)
			}
		}
		if !strings.Contains(legacy, key) || !strings.Contains(legacy, ".config.toml") {
			t.Errorf("%s: %q", lang, legacy)
		}
		if !strings.Contains(foreign, "model_providers.evren") {
			t.Errorf("%s: %q", lang, foreign)
		}
	}
	// The errors' own text, for a log, says the same.
	if got := (&LegacyProfileError{Path: path, Key: key}).Error(); !strings.Contains(got, path) || !strings.Contains(got, key) {
		t.Errorf("LegacyProfileError: %q", got)
	}
	if got := (&ForeignEntryError{Path: path, Entry: "model_providers.evren"}).Error(); !strings.Contains(got, path) || !strings.Contains(got, "model_providers.evren") ||
		strings.Contains(got, "did not write") {
		t.Errorf("ForeignEntryError: %q", got)
	}
}
