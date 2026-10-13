package setup

import (
	"path/filepath"
	"reflect"
	"testing"
)

// The rule for a tool's default model, as a table: what the tool has, down
// the side, against what the user asked for, across. The table is written
// out here and not computed, so it is a second statement of the rule in
// Request.changesDefault; both adapters are held to it.

// defaultOptions are the four ways to ask: no option, --model, --default,
// and both.
var defaultOptions = []struct {
	name                    string
	modelGiven, makeDefault bool
}{{"plain", false, false}, {"--model", true, false}, {"--default", false, true}, {"both", true, true}}

// defaultRule says, for each kind of default a tool can have, with which of
// the four options the request's default is written.
var defaultRule = []struct {
	had    string
	writes [4]bool // plain, --model, --default, both
}{
	{"nothing", [4]bool{true, true, true, true}},
	{"another provider's", [4]bool{false, false, true, true}},
	{"EVREN's", [4]bool{false, true, true, true}},
	{"EVREN's, of a model that is gone", [4]bool{true, true, true, true}},
	{"an empty string", [4]bool{true, true, true, true}},
	{"null", [4]bool{true, true, true, true}},
}

// checkDefaultNotes fails unless the plan carries the notes the rule calls
// for: "keeps its own default" for another provider's default that stays,
// and "was replaced" for one whose model is gone when no model was asked
// for. replaced is the key of the second note.
func checkDefaultNotes(t *testing.T, name string, p Plan, had string, modelGiven, makeDefault bool, kept, replaced string) {
	t.Helper()
	if want := had == "another provider's" && !makeDefault; hasNote(p, kept) != want {
		t.Errorf("%s: the note %s is there: %v", name, kept, !want)
	}
	if want := had == "EVREN's, of a model that is gone" && !modelGiven; hasNote(p, replaced) != want {
		t.Errorf("%s: the note %s is there: %v", name, replaced, !want)
	}
}

func TestDefaultRuleQwen(t *testing.T) {
	const llama = `{ "id": "llama", "envKey": "LOCAL", "baseUrl": "http://127.0.0.1:1/v1" }`
	gone := `{ "id": "gone", "name": "EVREN gone", "envKey": "EVREN_API_KEY", "baseUrl": "` + testRoot + `" }`
	file := func(entries, selectedType, model string) string {
		return `{"modelProviders": {"openai": [` + entries + `]}, "security": {"auth": {"selectedType": ` + selectedType + `}}, "model": ` + model + `}`
	}
	starts := map[string]string{
		"nothing":                          `{"modelProviders": {"openai": [` + llama + `]}}`,
		"another provider's":               file(llama, `"openai"`, `{"name": "llama", "baseUrl": "http://127.0.0.1:1/v1"}`),
		"EVREN's":                          file(qwenOne("qwen3-vl-30b", "262144", "true"), `"openai"`, `{"name": "qwen3-vl-30b", "baseUrl": "`+testRoot+`"}`),
		"EVREN's, of a model that is gone": file(gone, `"openai"`, `{"name": "gone", "baseUrl": "`+testRoot+`"}`),
		"an empty string":                  file(llama, `""`, `{"name": ""}`),
		"null":                             file(llama, `null`, `{"name": null, "baseUrl": null}`),
	}
	for _, row := range defaultRule {
		for i, option := range defaultOptions {
			name := row.had + ", " + option.name
			env := testEnv(t, nil, nil)
			path := write(t, qwenSettings(env), starts[row.had])
			req := testRequest()
			req.Default, req.ModelGiven, req.MakeDefault = "deepseek-v4-flash", option.modelGiven, option.makeDefault
			p := setUp(t, Qwen{}, env, req)
			want := qwenDefaultOf(decoded(t, starts[row.had]))
			if row.writes[i] {
				want = [3]any{"openai", "deepseek-v4-flash", testRoot}
			}
			if got := qwenDefaultOf(decoded(t, content(t, path))); got != want {
				t.Errorf("%s: the default is %v, want %v", name, got, want)
			}
			checkDefaultNotes(t, name, p, row.had, option.modelGiven, option.makeDefault, "setup.note.default_kept", "setup.note.default_replaced")
		}
	}
}

func TestDefaultRuleOpencode(t *testing.T) {
	values := map[string]string{
		"nothing":                          "",
		"another provider's":               `"anthropic/claude"`,
		"EVREN's":                          `"evren/qwen3-vl-30b"`,
		"EVREN's, of a model that is gone": `"evren/gone"`,
		"an empty string":                  `""`,
		"null":                             `null`,
	}
	file := func(key, value, rest string) string {
		if value == "" {
			return `{` + rest + `}`
		}
		if rest != "" {
			rest = ", " + rest
		}
		return `{"` + key + `": ` + value + rest + `}`
	}
	value := func(text string) any { return dig(decoded(t, `{"v": `+text+`}`), "v") }
	for _, row := range defaultRule {
		for i, option := range defaultOptions {
			name := row.had + ", " + option.name
			req := opencodeRequest()
			req.Default, req.ModelGiven, req.MakeDefault = "deepseek-v4-flash", option.modelGiven, option.makeDefault

			// The default model. There is no small_model: it is written
			// together with the model, and only then.
			env := testEnv(t, nil, nil)
			src := file("model", values[row.had], "")
			path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), src)
			p := setUp(t, Opencode{}, env, req)
			doc := decoded(t, content(t, path))
			model, hasModel := doc["model"]
			small, hasSmall := doc["small_model"]
			switch {
			case row.writes[i]:
				if model != "evren/deepseek-v4-flash" || small != "evren/deepseek-v4-flash" {
					t.Errorf("%s: model %v, small_model %v", name, model, small)
				}
			case hasSmall || hasModel != (values[row.had] != "") || !reflect.DeepEqual(model, value(orNull(values[row.had]))):
				t.Errorf("%s: model %v (there: %v), small_model %v (there: %v)", name, model, hasModel, small, hasSmall)
			}
			checkDefaultNotes(t, name, p, row.had, option.modelGiven, option.makeDefault, "setup.note.default_kept", "setup.note.default_replaced")

			// The model for titles, next to a default model that is another
			// provider's and so is written only with --default. small_model
			// follows --default, is written with the model when there is
			// none, and is repaired when its model is gone, always.
			env = testEnv(t, nil, nil)
			src = file("small_model", values[row.had], `"model": "anthropic/claude"`)
			path = write(t, filepath.Join(opencodeHome(env), "opencode.json"), src)
			p = setUp(t, Opencode{}, env, req)
			doc = decoded(t, content(t, path))
			small, hasSmall = doc["small_model"]
			gone := row.had == "EVREN's, of a model that is gone"
			switch {
			case option.makeDefault || gone:
				if small != "evren/deepseek-v4-flash" {
					t.Errorf("%s: small_model %v", name, small)
				}
			case hasSmall != (values[row.had] != "") || !reflect.DeepEqual(small, value(orNull(values[row.had]))):
				t.Errorf("%s: small_model %v (there: %v) was %s", name, small, hasSmall, values[row.had])
			}
			if wantModel := map[bool]string{true: "evren/deepseek-v4-flash", false: "anthropic/claude"}[option.makeDefault]; doc["model"] != wantModel {
				t.Errorf("%s: model %v next to the small_model", name, doc["model"])
			}
			if hasNote(p, "setup.note.small_replaced") != gone {
				t.Errorf("%s: notes %v", name, p.Notes)
			}
		}
	}
}

// orNull returns the JSON text of a value, "null" for none.
func orNull(text string) string {
	if text == "" {
		return "null"
	}
	return text
}
