package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mftnakrsu/ulgen/internal/i18n"
)

const qwenFresh = `{
  "$version": 4,
  "modelProviders": {
    "openai": [
      {
        "id": "deepseek-v4-flash",
        "name": "EVREN deepseek-v4-flash",
        "envKey": "EVREN_API_KEY",
        "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1",
        "generationConfig": {
          "contextWindowSize": 1048576,
          "modalities": {
            "image": false
          },
          "enableRequestMetadata": false
        }
      },
      {
        "id": "qwen3-vl-30b",
        "name": "EVREN qwen3-vl-30b",
        "envKey": "EVREN_API_KEY",
        "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1",
        "generationConfig": {
          "contextWindowSize": 262144,
          "modalities": {
            "image": true
          },
          "enableRequestMetadata": false
        }
      },
      {
        "id": "deepseek-v4.1-flash",
        "name": "EVREN deepseek-v4.1-flash",
        "envKey": "EVREN_API_KEY",
        "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1",
        "generationConfig": {
          "contextWindowSize": 1048576,
          "modalities": {
            "image": true
          },
          "enableRequestMetadata": false
        }
      }
    ]
  },
  "security": {
    "auth": {
      "selectedType": "openai"
    }
  },
  "model": {
    "name": "deepseek-v4.1-flash",
    "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1"
  },
  "privacy": {
    "usageStatisticsEnabled": false
  }
}
`

// qwenExisting is a settings file a user wrote: comments, another provider
// entry, a default model of their own, and usage statistics switched on.
const qwenExisting = `{
  // my settings
  "$version": 4,
  "modelProviders": {
    "openai": [
      // local model
      { "id": "llama", "envKey": "LOCAL_KEY", "baseUrl": "http://127.0.0.1:8080/v1" } // keep me
    ]
  },
  "security": { "auth": { "selectedType": "openai" } },
  "model": { "name": "llama" },
  "privacy": { "usageStatisticsEnabled": true },
  "ui": { "theme": "dark" } // dark, always
}
`

func qwenSettings(env Env) string { return filepath.Join(env.Home, ".qwen", "settings.json") }

// qwenOne is Ülgen's entry for a model as a user's editor would leave it on
// one line; context is the model's window.
func qwenOne(id, context, image string) string {
	return `{ "id": "` + id + `", "name": "EVREN ` + id + `", "envKey": "EVREN_API_KEY", "baseUrl": "` + testRoot +
		`", "generationConfig": { "contextWindowSize": ` + context + `, "modalities": { "image": ` + image + ` }, "enableRequestMetadata": false } }`
}

// qwenList returns the decoded entries of modelProviders.openai.
func qwenList(doc map[string]any) []any {
	list, _ := dig(doc, "modelProviders", "openai").([]any)
	return list
}

// qwenIDs returns the ids of the entries of modelProviders.openai, in order.
func qwenIDs(doc map[string]any) string {
	var ids []string
	for _, entry := range qwenList(doc) {
		id, _ := dig(entry, "id").(string)
		ids = append(ids, id)
	}
	return strings.Join(ids, " ")
}

// qwenDefaultOf returns the three keys of Qwen Code's default model.
func qwenDefaultOf(doc map[string]any) [3]any {
	return [3]any{dig(doc, "security", "auth", "selectedType"), dig(doc, "model", "name"), dig(doc, "model", "baseUrl")}
}

func TestQwenFreshMachine(t *testing.T) {
	env := testEnv(t, nil, nil)
	p, err := Qwen{}.Plan(env, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got := planned(t, p, qwenSettings(env)); got != qwenFresh {
		t.Errorf("settings.json:\n%s", got)
	}
	if len(p.Files) != 1 {
		t.Errorf("files = %v", paths(p))
	}
	if !hasNote(p, "setup.note.stats_off") || !hasNote(p, "setup.note.export") || len(p.Notes) != 2 {
		t.Errorf("notes = %v", p.Notes)
	}
	applyAndReplan(t, Qwen{}, env, testRequest(), p)
	if ok, err := (Qwen{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
}

func TestQwenKeepsTheUsersFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), qwenExisting)
	if ok, err := (Qwen{}).Configured(env); ok || err != nil {
		t.Errorf("Configured before setup = %v, %v", ok, err)
	}
	req := testRequest()
	req.Models = req.Models[2:] // one model keeps the expectation short
	p, err := Qwen{}.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  // my settings
  "$version": 4,
  "modelProviders": {
    "openai": [
      // local model
      { "id": "llama", "envKey": "LOCAL_KEY", "baseUrl": "http://127.0.0.1:8080/v1" }, // keep me
      {
        "id": "deepseek-v4.1-flash",
        "name": "EVREN deepseek-v4.1-flash",
        "envKey": "EVREN_API_KEY",
        "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1",
        "generationConfig": {
          "contextWindowSize": 1048576,
          "modalities": {
            "image": true
          },
          "enableRequestMetadata": false
        }
      }
    ]
  },
  "security": { "auth": { "selectedType": "openai" } },
  "model": { "name": "llama" },
  "privacy": { "usageStatisticsEnabled": true },
  "ui": { "theme": "dark" } // dark, always
}
`
	if got := planned(t, p, path); got != want {
		t.Errorf("settings.json:\n%s", got)
	}
	// The default is the user's own entry: it stays, and a note says how to
	// change that. The statistics were the user's choice.
	if args := noteArgs(t, p, "setup.note.default_kept"); len(args) != 2 || args[0] != "Qwen Code" || args[1] != "qwen" {
		t.Errorf("note arguments: %v", args)
	}
	if hasNote(p, "setup.note.stats_off") {
		t.Errorf("notes = %v", p.Notes)
	}
	applyAndReplan(t, Qwen{}, env, req, p)

	// A changed model list replaces Ülgen's entry and leaves the user's.
	req.Models = testRequest().Models[:1]
	req.Default = req.Models[0].ID
	if p, err = (Qwen{}).Plan(env, req); err != nil {
		t.Fatal(err)
	}
	got := planned(t, p, path)
	doc := decoded(t, got)
	if qwenIDs(doc) != "llama deepseek-v4-flash" || qwenDefaultOf(doc) != [3]any{"openai", "llama", nil} {
		t.Errorf("entries %q, default %v", qwenIDs(doc), qwenDefaultOf(doc))
	}
	if !strings.Contains(got, `{ "id": "llama", "envKey": "LOCAL_KEY", "baseUrl": "http://127.0.0.1:8080/v1" }, // keep me`) {
		t.Errorf("the user's entry is not as it was:\n%s", got)
	}
}

func TestQwenMakeDefault(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), qwenExisting)
	req := testRequest()
	req.MakeDefault = true
	p, err := Qwen{}.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := qwenDefaultOf(plannedDoc(t, p, path)); got != [3]any{"openai", "deepseek-v4.1-flash", testRoot} {
		t.Errorf("default = %v", got)
	}
	if hasNote(p, "setup.note.default_kept") {
		t.Errorf("notes = %v", p.Notes)
	}
	applyAndReplan(t, Qwen{}, env, req, p)
}

// One rule says which entries are Ülgen's: the ones that read the EVREN
// key. An entry of the user's at EVREN's address with a variable of its own
// is not touched, and an entry with the EVREN key is Ülgen's at any address.
func TestQwenOneRuleForOurs(t *testing.T) {
	const mine = `{ "id": "auto", "name": "my router", "envKey": "MY_EVREN_KEY", "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1/", "generationConfig": { "samplingParams": { "temperature": 0.2 } } }`
	const src = `{
  "$version": 4,
  "modelProviders": {
    "openai": [
      ` + mine + `,
      { "id": "other", "envKey": "EVREN_API_KEY", "baseUrl": "https://proxy.example/v1" }
    ]
  },
  "security": { "auth": { "selectedType": "openai" } },
  "model": { "name": "auto", "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1/" }
}
`
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), src)
	// Configured and Plan go by the same rule: the second entry is Ülgen's.
	if ok, err := (Qwen{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
	req := testRequest()
	req.Models = req.Models[:1]
	req.Default = req.Models[0].ID
	p := setUp(t, Qwen{}, env, req)
	got := content(t, path)
	doc := decoded(t, got)
	if !strings.Contains(got, "      "+mine+",\n") {
		t.Errorf("the user's entry is not as it was:\n%s", got)
	}
	if qwenIDs(doc) != "auto deepseek-v4-flash" || dig(qwenList(doc)[1], "baseUrl") != testRoot {
		t.Errorf("entries: %v", qwenList(doc))
	}
	// The default names the user's entry: it is not EVREN's, and stays.
	if qwenDefaultOf(doc) != qwenDefaultOf(decoded(t, src)) || !hasNote(p, "setup.note.default_kept") {
		t.Errorf("default %v, notes %v", qwenDefaultOf(doc), p.Notes)
	}

	// Without an entry that reads the EVREN key, EVREN is not set up.
	env = testEnv(t, nil, nil)
	write(t, qwenSettings(env), `{"modelProviders": {"openai": [`+mine+`, "text", 5, null, {"envKey": 7}]}}`)
	if ok, err := (Qwen{}).Configured(env); ok || err != nil {
		t.Errorf("only the user's entry: Configured = %v, %v", ok, err)
	}
	// What is not an entry at all is the user's as well.
	doc = plannedDoc(t, setUp(t, Qwen{}, env, req), qwenSettings(env))
	if list := qwenList(doc); len(list) != 6 || list[1] != "text" || list[2] != float64(5) || list[3] != nil || dig(list[4], "envKey") != float64(7) {
		t.Errorf("entries: %v", list)
	}
}

// Setup with another --base-url leaves no entry of the old address behind,
// and a default that was EVREN's keeps pointing at Ülgen's entry.
func TestQwenEntriesOfAnEarlierRootAreReplaced(t *testing.T) {
	env := testEnv(t, nil, nil)
	req := testRequest()
	req.BaseURL = "http://127.0.0.1:4000/v1"
	setUp(t, Qwen{}, env, req)
	req.BaseURL = testRoot
	p := setUp(t, Qwen{}, env, req)
	doc := decoded(t, content(t, qwenSettings(env)))
	if qwenIDs(doc) != "deepseek-v4-flash qwen3-vl-30b deepseek-v4.1-flash" {
		t.Errorf("entries: %q", qwenIDs(doc))
	}
	for _, entry := range qwenList(doc) {
		if dig(entry, "baseUrl") != testRoot || dig(entry, "envKey") != KeyEnv {
			t.Errorf("a stale entry: %v", entry)
		}
	}
	if qwenDefaultOf(doc) != [3]any{"openai", "deepseek-v4.1-flash", testRoot} || hasNote(p, "setup.note.default_kept") {
		t.Errorf("default %v, notes %v", qwenDefaultOf(doc), p.Notes)
	}
	if ok, err := (Qwen{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
}

// Ülgen's entries are found by their id. The user moved them, put an entry
// of their own between them and wrote comments: order and comments stay, an
// entry that does not change keeps every byte, and a retired and a new model
// touch only their own entries.
func TestQwenEntriesAreMatchedByID(t *testing.T) {
	flash41 := "      " + qwenOne("deepseek-v4.1-flash", "1048576", "true") + ",\n"
	llama := "      { \"id\": \"llama\", \"envKey\": \"LOCAL\", \"baseUrl\": \"http://127.0.0.1:1/v1\" },\n"
	gone := "      { \"id\": \"gone\", \"name\": \"EVREN gone\", \"envKey\": \"EVREN_API_KEY\", \"baseUrl\": \"" + testRoot + "\" },\n"
	flash4 := "      " + qwenOne("deepseek-v4-flash", "1048576", "false")
	src := "{\n  \"modelProviders\": {\n    \"openai\": [\n" +
		"      // I want this one first\n" + flash41 + llama +
		"      // retired by EVREN\n" + gone +
		"      // the cheap one\n" + flash4 + "\n    ]\n  },\n  \"privacy\": { \"usageStatisticsEnabled\": true }\n}\n"
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), src)
	req := testRequest()
	setUp(t, Qwen{}, env, req)
	got := content(t, path)
	added, err := json.MarshalIndent(qwenEntryFor(req.Models[1], testRoot), "      ", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wantList := "      // I want this one first\n" + flash41 + llama +
		"      // retired by EVREN\n" +
		"      // the cheap one\n" + flash4 + ",\n      " + string(added) + "\n    ]\n"
	if !strings.Contains(got, wantList) {
		t.Errorf("the list:\n%s\nwant it to hold:\n%s", got, wantList)
	}
	if ids := qwenIDs(decoded(t, got)); ids != "deepseek-v4.1-flash llama deepseek-v4-flash qwen3-vl-30b" {
		t.Errorf("ids = %q", ids)
	}

	// An id that Ülgen has twice keeps its first entry; one whose content is
	// out of date is rewritten where it stands.
	env = testEnv(t, nil, nil)
	stale := "{ \"id\": \"qwen3-vl-30b\", \"envKey\": \"EVREN_API_KEY\", \"baseUrl\": \"http://old.example/v1\" }"
	path = write(t, qwenSettings(env), "{\n  \"modelProviders\": {\n    \"openai\": [\n      "+stale+", // first\n"+flash41+"      "+stale+" // again\n    ]\n  }\n}\n")
	setUp(t, Qwen{}, env, req)
	doc := decoded(t, content(t, path))
	if qwenIDs(doc) != "qwen3-vl-30b deepseek-v4.1-flash deepseek-v4-flash" || !reflect.DeepEqual(qwenList(doc)[0], qwenList(decoded(t, qwenFresh))[1]) {
		t.Errorf("entries: %v", qwenList(doc))
	}
	if got := content(t, path); !strings.Contains(got, flash41) || strings.Count(got, "// first") != 1 || strings.Count(got, "// again") != 1 {
		t.Errorf("an unchanged entry or a comment is gone:\n%s", got)
	}
}

// The default is EVREN's when Qwen Code would start with one of Ülgen's
// entries: the one whose address model.baseUrl names, else the first with
// the id. Qwen Code's own "/model" writes an empty model.baseUrl.
func TestQwenDefaultAsQwenCodeResolvesIt(t *testing.T) {
	ours := qwenOne("qwen3-vl-30b", "262144", "true")
	theirs := `{ "id": "qwen3-vl-30b", "envKey": "MY_KEY", "baseUrl": "https://mine.example/v1" }`
	for name, tc := range map[string]struct {
		entries, model string
		evren          bool
	}{
		"an empty baseUrl, as /model writes it": {ours, `{ "name": "qwen3-vl-30b", "baseUrl": "" }`, true},
		"no baseUrl":                            {ours, `{ "name": "qwen3-vl-30b" }`, true},
		"baseUrl null":                          {ours, `{ "name": "qwen3-vl-30b", "baseUrl": null }`, true},
		"the entry's baseUrl":                   {ours, `{ "name": "qwen3-vl-30b", "baseUrl": "` + testRoot + `" }`, true},
		"a baseUrl no entry has":                {ours, `{ "name": "qwen3-vl-30b", "baseUrl": "` + testRoot + `/" }`, true},
		"the user's entry comes first":          {theirs + ", " + ours, `{ "name": "qwen3-vl-30b", "baseUrl": "" }`, false},
		"ours first, baseUrl names theirs":      {ours + ", " + theirs, `{ "name": "qwen3-vl-30b", "baseUrl": "https://mine.example/v1" }`, false},
		"theirs first, baseUrl names ours":      {theirs + ", " + ours, `{ "name": "qwen3-vl-30b", "baseUrl": "` + testRoot + `" }`, true},
		"ours first, no baseUrl":                {ours + ", " + theirs, `{ "name": "qwen3-vl-30b" }`, true},
		"no entry with that id":                 {ours, `{ "name": "llama", "baseUrl": "` + testRoot + `" }`, false},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			src := "{\n  \"modelProviders\": { \"openai\": [" + tc.entries + "] },\n  \"security\": { \"auth\": { \"selectedType\": \"openai\" } },\n  \"model\": " + tc.model + "\n}\n"
			path := write(t, qwenSettings(env), src)
			before := decoded(t, src)
			// A plain run leaves the default alone and says "keeps its own"
			// only when it is not EVREN's.
			p, err := Qwen{}.Plan(env, testRequest())
			if err != nil {
				t.Fatal(err)
			}
			got := plannedDoc(t, p, path)
			if dig(got, "model", "name") != dig(before, "model", "name") || hasNote(p, "setup.note.default_kept") == tc.evren {
				t.Errorf("plain: default %v, notes %v", qwenDefaultOf(got), p.Notes)
			}
			// --model changes a default that is EVREN's, and no other.
			req := testRequest()
			req.Default, req.ModelGiven = "deepseek-v4-flash", true
			if p, err = (Qwen{}).Plan(env, req); err != nil {
				t.Fatal(err)
			}
			want := qwenDefaultOf(before)
			if tc.evren {
				want = [3]any{"openai", "deepseek-v4-flash", testRoot}
			}
			if got := qwenDefaultOf(plannedDoc(t, p, path)); got != want || hasNote(p, "setup.note.default_kept") == tc.evren {
				t.Errorf("--model: default %v, want %v; notes %v", got, want, p.Notes)
			}
			applyAndReplan(t, Qwen{}, env, req, p)
		})
	}
}

// Qwen Code looks through every provider list that speaks the OpenAI
// protocol. When another list has the default's id as well, Ülgen cannot
// say which entry Qwen Code takes, and leaves the default alone.
func TestQwenDefaultUnderACustomProvider(t *testing.T) {
	env := testEnv(t, nil, nil)
	src := `{
  "providerProtocol": { "corp": "openai", "claude": "anthropic" },
  "modelProviders": {
    "corp": [{ "id": "qwen3-vl-30b", "envKey": "CORP_KEY", "baseUrl": "https://llm.corp.example/v1" }],
    "claude": [{ "id": "deepseek-v4-flash" }],
    "openai": [` + qwenOne("qwen3-vl-30b", "262144", "true") + `, ` + qwenOne("deepseek-v4-flash", "1048576", "false") + `]
  },
  "security": { "auth": { "selectedType": "openai" } },
  "model": { "name": "qwen3-vl-30b" }
}
`
	path := write(t, qwenSettings(env), src)
	req := testRequest()
	req.Default, req.ModelGiven = "deepseek-v4.1-flash", true
	p := setUp(t, Qwen{}, env, req)
	doc := decoded(t, content(t, path))
	if qwenDefaultOf(doc) != [3]any{"openai", "qwen3-vl-30b", nil} || !hasNote(p, "setup.note.default_kept") {
		t.Errorf("default %v, notes %v", qwenDefaultOf(doc), p.Notes)
	}
	if !reflect.DeepEqual(dig(doc, "modelProviders", "corp"), dig(decoded(t, src), "modelProviders", "corp")) {
		t.Errorf("the other list changed: %v", dig(doc, "modelProviders", "corp"))
	}
	// A list of another protocol with the same id does not matter.
	write(t, path, strings.Replace(src, `"name": "qwen3-vl-30b" }`, `"name": "deepseek-v4-flash" }`, 1))
	setUp(t, Qwen{}, env, req)
	if got := qwenDefaultOf(decoded(t, content(t, path))); got != [3]any{"openai", "deepseek-v4.1-flash", testRoot} {
		t.Errorf("default = %v", got)
	}
}

// Qwen Code finds its default by name. A default of the user's that no
// entry answers to (the address and the key come from the environment) and
// that carries the name of a model EVREN offers would be answered by the
// entry Ülgen writes: a take-over nobody asked for, so setup refuses it. It
// makes no difference whether the settings name "openai" as the sign-in or
// none at all: Qwen Code's "/model" writes the name alone.
func TestQwenDefaultThatUlgensEntryWouldTakeOver(t *testing.T) {
	const llama = `{"id": "llama", "envKey": "LOCAL", "baseUrl": "http://127.0.0.1:1/v1"}`
	file := func(authType, model, entries string) string {
		return `{"security": {"auth": {"selectedType": ` + authType + `}}, "model": ` + model + `, "modelProviders": {"openai": [` + entries + `]}}`
	}
	for name, tc := range map[string]struct {
		src   string
		clash bool
	}{
		"no entry, a name EVREN offers":         {file(`"openai"`, `{"name": "deepseek-v4-flash"}`, llama), true},
		"the same with an empty baseUrl":        {file(`"openai"`, `{"name": "deepseek-v4-flash", "baseUrl": ""}`, ""), true},
		"the same with another address":         {file(`"openai"`, `{"name": "deepseek-v4-flash", "baseUrl": "https://api.other.example/v1"}`, llama), true},
		"the same without a kind of sign-in":    {file(`null`, `{"name": "deepseek-v4-flash"}`, llama), true},
		"what /model writes and nothing else":   {`{"model": {"name": "deepseek-v4-flash", "baseUrl": ""}}`, true},
		"a custom provider list holds the id":   {`{"providerProtocol": {"corp": "openai"}, "modelProviders": {"corp": [{"id": "deepseek-v4-flash"}]}, "security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash"}}`, false},
		"EVREN's address: it was meant for it":  {file(`"openai"`, `{"name": "deepseek-v4-flash", "baseUrl": "`+testRoot+`/"}`, llama), false},
		"a name EVREN does not offer":           {file(`"openai"`, `{"name": "gpt-6"}`, llama), false},
		"an entry of the user's with that name": {file(`"openai"`, `{"name": "deepseek-v4-flash"}`, `{"id": "deepseek-v4-flash", "envKey": "MY_KEY", "baseUrl": "https://api.other.example/v1"}`), false},
		"the user's entry, no kind of sign-in":  {file(`null`, `{"name": "deepseek-v4-flash"}`, `{"id": "deepseek-v4-flash", "envKey": "MY_KEY", "baseUrl": "https://api.other.example/v1"}`), false},
		"another kind of sign-in":               {file(`"anthropic"`, `{"name": "deepseek-v4-flash"}`, llama), false},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			path := write(t, qwenSettings(env), tc.src)
			before := qwenDefaultOf(decoded(t, tc.src))
			for _, modelGiven := range []bool{false, true} {
				req := testRequest()
				req.ModelGiven = modelGiven
				p, err := Qwen{}.Plan(env, req)
				var clash *DefaultClashError
				if !tc.clash {
					if err != nil {
						t.Fatalf("--model %v: %v", modelGiven, err)
					}
					continue
				}
				if !errors.As(err, &clash) || clash.Path != path || clash.Var != "" || clash.Model != "deepseek-v4-flash" || len(p.Files) != 0 {
					t.Errorf("--model %v: Plan = %v, %T %v", modelGiven, paths(p), err, err)
				}
			}
			if tc.clash {
				unchanged(t, path, tc.src)
			}
			// With --default the take-over is what was asked for.
			req := testRequest()
			req.MakeDefault = true
			setUp(t, Qwen{}, env, req)
			if got := qwenDefaultOf(decoded(t, content(t, path))); got != [3]any{"openai", "deepseek-v4.1-flash", testRoot} {
				t.Errorf("--default: the default is %v, was %v", got, before)
			}
		})
	}
	if got := (&DefaultClashError{Path: "p", Model: "m"}).Error(); !strings.Contains(got, "p names \"m\"") {
		t.Errorf("DefaultClashError: %q", got)
	}
	if got := (&DefaultClashError{Var: "V", Model: "m"}).Error(); !strings.HasPrefix(got, "V names \"m\"") {
		t.Errorf("DefaultClashError for a variable: %q", got)
	}
}

// The refusal tells the user to add an entry of their own under
// modelProviders.openai. Whoever does that is not refused again: the default
// is theirs, by an entry Qwen Code finds before Ülgen's.
func TestQwenTheAdviceOfTheRefusalWorks(t *testing.T) {
	const mine = `{"id": "deepseek-v4-flash", "envKey": "MY_KEY", "baseUrl": "https://api.other.example/v1"}`
	for name, src := range map[string]string{
		"a default by its name":      `{"security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash"}}`,
		"without a kind of sign-in":  `{"model": {"name": "deepseek-v4-flash", "baseUrl": ""}}`,
		"an address no entry has":    `{"security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash", "baseUrl": "https://api.other.example/v1"}}`,
		"a custom list with the id":  `{"providerProtocol": {"corp": "openai"}, "modelProviders": {"corp": [{"id": "deepseek-v4-flash", "envKey": "CORP_KEY", "baseUrl": "https://llm.corp.example/v1"}]}, "security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash", "baseUrl": "https://llm.corp.example/v1"}}`,
		"the list is there, empty":   `{"modelProviders": {"openai": []}, "model": {"name": "deepseek-v4-flash"}}`,
		"only another model of ours": `{"modelProviders": {"openai": [` + qwenOne("qwen3-vl-30b", "262144", "true") + `]}, "security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			path := write(t, qwenSettings(env), src)
			_, err := Qwen{}.Plan(env, testRequest())
			var clash *DefaultClashError
			if refused := errors.As(err, &clash); refused == strings.Contains(src, `"corp"`) {
				t.Fatalf("before the advice: %T %v", err, err) // a custom list that holds the id is the user's default already
			}
			// The advice: an entry of one's own for the model, under
			// modelProviders.openai.
			doc := decoded(t, src)
			providers, _ := doc["modelProviders"].(map[string]any)
			if providers == nil {
				providers = map[string]any{}
			}
			list, _ := providers["openai"].([]any)
			providers["openai"] = append([]any{dig(decoded(t, `{"e": `+mine+`}`), "e")}, list...)
			doc["modelProviders"] = providers
			advised, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			write(t, path, string(advised))
			p := setUp(t, Qwen{}, env, testRequest())
			after := decoded(t, content(t, path))
			if qwenDefaultOf(after) != qwenDefaultOf(doc) || !hasNote(p, "setup.note.default_kept") {
				t.Errorf("the default is %v, was %v; notes %v", qwenDefaultOf(after), qwenDefaultOf(doc), p.Notes)
			}
			if first := qwenList(after)[0]; dig(first, "envKey") != "MY_KEY" || len(qwenList(after)) < 4 {
				t.Errorf("entries: %v", qwenList(after))
			}
		})
	}
}

// A default without a kind of sign-in next to it is judged like one of
// "openai": with Ülgen's entries in the file it is EVREN's, --model changes
// it, and one whose model is gone is repaired.
func TestQwenDefaultWithoutASignInType(t *testing.T) {
	ours := qwenOne("qwen3-vl-30b", "262144", "true") + ", " + qwenOne("deepseek-v4-flash", "1048576", "false") + ", " + qwenOne("deepseek-v4.1-flash", "1048576", "true")
	gone := `{ "id": "gone", "name": "EVREN gone", "envKey": "EVREN_API_KEY", "baseUrl": "` + testRoot + `" }`
	for name, tc := range map[string]struct {
		entries, model string
		plain, named   [3]any // the default after a plain run, and after --model deepseek-v4-flash
		kept, replaced bool   // the notes of the plain run
	}{
		"what /model writes for an EVREN model": {ours, `{"name": "qwen3-vl-30b", "baseUrl": ""}`,
			[3]any{nil, "qwen3-vl-30b", ""}, [3]any{"openai", "deepseek-v4-flash", testRoot}, false, false},
		"the name alone": {ours, `{"name": "qwen3-vl-30b"}`,
			[3]any{nil, "qwen3-vl-30b", nil}, [3]any{"openai", "deepseek-v4-flash", testRoot}, false, false},
		"a model that is gone": {gone + ", " + ours, `{"name": "gone", "baseUrl": ""}`,
			[3]any{"openai", "deepseek-v4-flash", testRoot}, [3]any{"openai", "deepseek-v4-flash", testRoot}, false, true},
		"the user's own entry": {`{"id": "llama", "envKey": "LOCAL", "baseUrl": "http://127.0.0.1:1/v1"}, ` + ours, `{"name": "llama"}`,
			[3]any{nil, "llama", nil}, [3]any{nil, "llama", nil}, true, false},
	} {
		for _, modelGiven := range []bool{false, true} {
			env := testEnv(t, nil, nil)
			path := write(t, qwenSettings(env), `{"modelProviders": {"openai": [`+tc.entries+`]}, "model": `+tc.model+`}`)
			req := testRequest()
			req.Default, req.ModelGiven = "deepseek-v4-flash", modelGiven
			p := setUp(t, Qwen{}, env, req)
			want := tc.plain
			if modelGiven {
				want = tc.named
			}
			if got := qwenDefaultOf(decoded(t, content(t, path))); got != want {
				t.Errorf("%s, --model %v: the default is %v, want %v", name, modelGiven, got, want)
			}
			if hasNote(p, "setup.note.default_kept") != tc.kept || hasNote(p, "setup.note.default_replaced") != (tc.replaced && !modelGiven) {
				t.Errorf("%s, --model %v: notes %v", name, modelGiven, p.Notes)
			}
		}
	}
}

// What the settings have to say for Qwen Code to have a default of its own,
// with nothing in the environment: a model, or a kind of sign-in. A kind of
// sign-in alone is a choice of the user's too, and stays.
func TestQwenWhatCountsAsADefault(t *testing.T) {
	for src, kept := range map[string]bool{
		`{}`: false,
		`{"security": {"auth": {}}, "model": {}}`:                                                       false,
		`{"security": {"auth": {"selectedType": ""}}, "model": {"name": null}}`:                         false,
		`{"security": {"auth": {"selectedType": "openai"}}}`:                                            true,
		`{"security": {"auth": {"selectedType": "qwen-oauth"}}}`:                                        true,
		`{"security": {"auth": {"selectedType": "anthropic"}}, "model": {"name": "deepseek-v4-flash"}}`: true,
		`{"model": {"name": "gpt-6"}}`:                                                                  true,
		// A default at EVREN's address whose entry is gone was EVREN's: the
		// entry comes back and the default is as it was, without a note.
		`{"security": {"auth": {"selectedType": "openai"}}, "model": {"name": "deepseek-v4-flash", "baseUrl": "` + testRoot + `"}}`: false,
	} {
		env := testEnv(t, nil, nil)
		path := write(t, qwenSettings(env), src)
		p := setUp(t, Qwen{}, env, testRequest())
		before, after := qwenDefaultOf(decoded(t, src)), qwenDefaultOf(decoded(t, content(t, path)))
		dangling := strings.Contains(src, testRoot)
		switch {
		case hasNote(p, "setup.note.default_kept") != kept:
			t.Errorf("%s: notes %v", src, p.Notes)
		case (kept || dangling) && after != before:
			t.Errorf("%s: the default is %v, was %v", src, after, before)
		case !kept && !dangling && after != [3]any{"openai", "deepseek-v4.1-flash", testRoot}:
			t.Errorf("%s: the default is %v", src, after)
		}
	}
}

// Settings without a default leave the choice to the environment. With
// OPENAI_BASE_URL or a model variable set, the user has a default there: it
// is not written over without --default, and a note says where it is. A
// model variable that names one of EVREN's models, with no entry to answer
// to, is the clash again. The variable with the key is never read.
func TestQwenDefaultFromTheEnvironment(t *testing.T) {
	const mine = `{"id": "deepseek-v4-flash", "envKey": "MY_KEY", "baseUrl": "https://api.other.example/v1"}`
	for name, tc := range map[string]struct {
		vars  map[string]string
		src   string
		note  string // the variables the note names; "" for no note
		clash string // the variable the refusal names; "" for none
	}{
		"an address":                           {map[string]string{"OPENAI_BASE_URL": "https://api.other.example/v1"}, `{}`, "OPENAI_BASE_URL", ""},
		"a model of the user's":                {map[string]string{"OPENAI_MODEL": "their-model"}, `{}`, "OPENAI_MODEL", ""},
		"Qwen Code's own name":                 {map[string]string{"QWEN_MODEL": "their-model"}, `{}`, "QWEN_MODEL", ""},
		"all three":                            {map[string]string{"OPENAI_BASE_URL": "https://x.example/v1", "OPENAI_MODEL": "a", "QWEN_MODEL": "b"}, `{}`, "OPENAI_BASE_URL, OPENAI_MODEL, QWEN_MODEL", ""},
		"next to a sign-in type":               {map[string]string{"OPENAI_MODEL": "their-model"}, `{"security": {"auth": {"selectedType": "openai"}}}`, "OPENAI_MODEL", ""},
		"a model EVREN offers":                 {map[string]string{"OPENAI_MODEL": "deepseek-v4-flash", "OPENAI_BASE_URL": "https://api.other.example/v1"}, `{}`, "", "OPENAI_MODEL"},
		"the second variable":                  {map[string]string{"QWEN_MODEL": "deepseek-v4-flash"}, `{"security": {"auth": {"selectedType": "openai"}}}`, "", "QWEN_MODEL"},
		"the first one decides":                {map[string]string{"OPENAI_MODEL": "their-model", "QWEN_MODEL": "deepseek-v4-flash"}, `{}`, "OPENAI_MODEL, QWEN_MODEL", ""},
		"offered, at EVREN's own address":      {map[string]string{"OPENAI_MODEL": "deepseek-v4-flash", "OPENAI_BASE_URL": testRoot + "/"}, `{}`, "OPENAI_BASE_URL, OPENAI_MODEL", ""},
		"offered, with an entry of the user's": {map[string]string{"OPENAI_MODEL": "deepseek-v4-flash"}, `{"modelProviders": {"openai": [` + mine + `]}}`, "OPENAI_MODEL", ""},
		"offered, with an entry of Ülgen's":    {map[string]string{"OPENAI_MODEL": "deepseek-v4-flash"}, `{"modelProviders": {"openai": [` + qwenOne("deepseek-v4-flash", "1048576", "false") + `]}}`, "OPENAI_MODEL", ""},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			env.Getenv = func(key string) string {
				if key == "OPENAI_API_KEY" || key == KeyEnv {
					t.Errorf("the environment was asked for %s", key)
				}
				return tc.vars[key]
			}
			path := write(t, qwenSettings(env), tc.src)
			before := qwenDefaultOf(decoded(t, tc.src))
			for _, modelGiven := range []bool{false, true} {
				req := testRequest()
				req.ModelGiven = modelGiven
				p, err := Qwen{}.Plan(env, req)
				var clash *DefaultClashError
				if tc.clash != "" {
					if !errors.As(err, &clash) || clash.Var != tc.clash || clash.Path != "" || clash.Model != "deepseek-v4-flash" || len(p.Files) != 0 {
						t.Errorf("--model %v: Plan = %v, %T %v", modelGiven, paths(p), err, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("--model %v: %v", modelGiven, err)
				}
				doc := plannedDoc(t, p, path)
				if args := noteArgs(t, p, "setup.note.qwen_env_default"); qwenDefaultOf(doc) != before || len(args) != 2 || args[0] != tc.note || args[1] != "qwen" || hasNote(p, "setup.note.default_kept") {
					t.Errorf("--model %v: the default is %v, notes %v", modelGiven, qwenDefaultOf(doc), p.Notes)
				}
				if !strings.Contains(qwenIDs(doc), "deepseek-v4.1-flash") {
					t.Errorf("--model %v: EVREN's models were not written: %q", modelGiven, qwenIDs(doc))
				}
			}
			if tc.clash != "" {
				unchanged(t, path, tc.src)
			}
			// --default is the user's word against the environment's.
			req := testRequest()
			req.MakeDefault = true
			p := setUp(t, Qwen{}, env, req)
			if got := qwenDefaultOf(decoded(t, content(t, path))); got != [3]any{"openai", "deepseek-v4.1-flash", testRoot} || hasNote(p, "setup.note.qwen_env_default") {
				t.Errorf("--default: the default is %v, notes %v", got, p.Notes)
			}
		})
	}

	// The key's variable alone is no default, and a default in the file
	// decides before the environment does.
	env := testEnv(t, map[string]string{"OPENAI_API_KEY": "invented-value"}, nil)
	if p := setUp(t, Qwen{}, env, testRequest()); hasNote(p, "setup.note.qwen_env_default") || qwenDefaultOf(decoded(t, content(t, qwenSettings(env)))) != [3]any{"openai", "deepseek-v4.1-flash", testRoot} {
		t.Errorf("only the key in the environment: notes %v", p.Notes)
	}
	env = testEnv(t, map[string]string{"OPENAI_MODEL": "deepseek-v4-flash", "OPENAI_BASE_URL": "https://api.other.example/v1"}, nil)
	write(t, qwenSettings(env), qwenExisting)
	if p := setUp(t, Qwen{}, env, testRequest()); hasNote(p, "setup.note.qwen_env_default") || !hasNote(p, "setup.note.default_kept") {
		t.Errorf("a default in the file: notes %v", p.Notes)
	}
}

// An entry that reads the EVREN key is Ülgen's by the rule, also when the
// user copied it for another service and forgot the variable's name. It is
// removed like every entry of a model EVREN does not have, but not without
// a word: a note names it, and what to change.
func TestQwenAnEntryOfAnotherServiceIsRemovedWithANote(t *testing.T) {
	flash := qwenOne("deepseek-v4-flash", "1048576", "false")
	entry := func(id, base string) string {
		return `{"id": "` + id + `", "envKey": "EVREN_API_KEY", "baseUrl": "` + base + `"}`
	}
	for name, tc := range map[string]struct {
		entries string
		noted   [][2]any // id and address of each note
	}{
		"another service":             {flash + ", " + entry("corp-model", "https://proxy.example/v1"), [][2]any{{"corp-model", "https://proxy.example/v1"}}},
		"with secrets in its address": {flash + ", " + entry("corp-model", "https://someone:SENTINEL@proxy.example/v1?key=SENTINEL#SENTINEL"), [][2]any{{"corp-model", "https://proxy.example/v1"}}},
		"two of them, one address":    {entry("a", "https://proxy.example/v1") + ", " + flash + ", " + entry("b", "https://proxy.example/v1/"), [][2]any{{"a", "https://proxy.example/v1"}, {"b", "https://proxy.example/v1/"}}},
		"without an address":          {flash + `, {"id": "corp-model", "envKey": "EVREN_API_KEY"}`, [][2]any{{"corp-model", ""}}},
		"a model EVREN retired":       {flash + ", " + entry("gone", testRoot), nil},
		"the same with a slash":       {flash + ", " + entry("gone", testRoot+"/"), nil},
		"retired, of an earlier root": {entry("deepseek-v4-flash", "http://127.0.0.1:4000/v1") + ", " + entry("gone", "http://127.0.0.1:4000/v1"), nil},
		"the user's own variable":     {flash + `, {"id": "corp-model", "envKey": "CORP_KEY", "baseUrl": "https://proxy.example/v1"}`, nil},
	} {
		env := testEnv(t, nil, nil)
		path := write(t, qwenSettings(env), `{"modelProviders": {"openai": [`+tc.entries+`]}}`)
		p := setUp(t, Qwen{}, env, testRequest())
		var noted [][2]any
		for _, n := range p.Notes {
			if n.Key == "setup.note.qwen_entry_removed" && len(n.Args) == 2 {
				noted = append(noted, [2]any{n.Args[0], n.Args[1]})
			}
			for _, arg := range n.Args {
				if text, _ := arg.(string); strings.Contains(text, "SENTINEL") || strings.Contains(text, "someone") {
					t.Errorf("%s: the note %s carries a secret: %v", name, n.Key, n.Args)
				}
			}
		}
		if !reflect.DeepEqual(noted, tc.noted) {
			t.Errorf("%s: noted %v, want %v", name, noted, tc.noted)
		}
		// Noted or not, what reads the EVREN key and is no model of EVREN's
		// is gone; the user's own entry is not.
		ids := qwenIDs(decoded(t, content(t, path)))
		if strings.Contains(name, "own variable") != strings.Contains(ids, "corp-model") || strings.Contains(ids, "gone") {
			t.Errorf("%s: entries %q", name, ids)
		}
	}

	// When that entry was the default, the default goes with it. The note
	// about it is true for both kinds of entry: it does not say that EVREN
	// stopped offering a model it never had.
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), `{"modelProviders": {"openai": [`+flash+`, `+entry("corp-model", "https://proxy.example/v1")+`]}, "security": {"auth": {"selectedType": "openai"}}, "model": {"name": "corp-model", "baseUrl": "https://proxy.example/v1"}}`)
	p := setUp(t, Qwen{}, env, testRequest())
	if got := qwenDefaultOf(decoded(t, content(t, path))); got != [3]any{"openai", "deepseek-v4.1-flash", testRoot} {
		t.Errorf("the default is %v", got)
	}
	if args := noteArgs(t, p, "setup.note.default_replaced"); len(args) != 2 || args[0] != "corp-model" || !hasNote(p, "setup.note.qwen_entry_removed") {
		t.Errorf("notes %v", p.Notes)
	}
	for _, lang := range i18n.Supported() {
		for _, key := range []string{"setup.note.default_replaced", "setup.note.small_replaced"} {
			if msg := i18n.New(lang).T(key, "corp-model", "deepseek-v4.1-flash"); strings.Contains(msg, "no longer") || strings.Contains(msg, "artık sunmuyor") || !strings.Contains(msg, "corp-model") {
				t.Errorf("%s in %s: %q", key, lang, msg)
			}
		}
	}
}

// A default that names a model EVREN no longer offers would start Qwen Code
// on a model that is not there: the request's default takes its place, and
// a note says which model went.
func TestQwenRetiredDefaultIsRepaired(t *testing.T) {
	env := testEnv(t, nil, nil)
	setUp(t, Qwen{}, env, testRequest()) // the default is deepseek-v4.1-flash
	req := testRequest()
	req.Models = req.Models[:2]
	req.Default = "deepseek-v4-flash"
	p := setUp(t, Qwen{}, env, req)
	doc := decoded(t, content(t, qwenSettings(env)))
	if qwenIDs(doc) != "deepseek-v4-flash qwen3-vl-30b" || qwenDefaultOf(doc) != [3]any{"openai", "deepseek-v4-flash", testRoot} {
		t.Errorf("entries %q, default %v", qwenIDs(doc), qwenDefaultOf(doc))
	}
	if args := noteArgs(t, p, "setup.note.default_replaced"); len(args) != 2 || args[0] != "deepseek-v4.1-flash" || args[1] != "deepseek-v4-flash" {
		t.Errorf("note arguments: %v", args)
	}
	if hasNote(p, "setup.note.default_kept") {
		t.Errorf("notes = %v", p.Notes)
	}
}

// A model whose context length the API does not give gets no
// contextWindowSize: Qwen Code would take a 0 as the window.
func TestQwenContextWindow(t *testing.T) {
	env := testEnv(t, nil, nil)
	req := Request{BaseURL: testRoot, Default: "known", Models: []Model{{ID: "known", Context: 4096}, {ID: "zero"}, {ID: "negative", Context: -1}}}
	p := setUp(t, Qwen{}, env, req)
	for _, entry := range qwenList(plannedDoc(t, p, qwenSettings(env))) {
		config, _ := dig(entry, "generationConfig").(map[string]any)
		size, has := config["contextWindowSize"]
		if id := dig(entry, "id"); has != (id == "known") || (has && size != float64(4096)) || config["modalities"] == nil {
			t.Errorf("%v: generationConfig %v", id, config)
		}
	}
}

// A file that is there and holds no value is a new file with comments: it
// gets "$version" like a file Ülgen creates, and what it held stays in
// front. An existing object, even an empty one, gets no "$version".
func TestQwenAFileWithoutAValue(t *testing.T) {
	for name, src := range map[string]string{
		"empty":             "",
		"whitespace":        "  \n\n",
		"a comment":         "// my qwen settings\n",
		"a comment, no end": "// my qwen settings",
		"a block comment":   "/* nothing yet */",
		"a byte-order mark": "\xef\xbb\xbf",
		"CRLF comments":     "\r\n// one\r\n/* two */\r\n",
	} {
		env := testEnv(t, nil, nil)
		path := write(t, qwenSettings(env), src)
		if ok, err := (Qwen{}).Configured(env); ok || err != nil {
			t.Errorf("%s: Configured = %v, %v", name, ok, err)
		}
		p := setUp(t, Qwen{}, env, testRequest())
		got := content(t, path)
		fresh := decoded(t, qwenFresh)
		if !strings.HasPrefix(got, src) || !reflect.DeepEqual(decoded(t, got), fresh) || len(p.Files) != 1 || p.Files[0].Old == nil {
			t.Errorf("%s: %q", name, got)
		}
	}
	for _, src := range []string{"{}", "{}\n", "{ /* mine */ }"} {
		env := testEnv(t, nil, nil)
		path := write(t, qwenSettings(env), src)
		setUp(t, Qwen{}, env, testRequest())
		doc := decoded(t, content(t, path))
		if _, has := doc["$version"]; has || qwenIDs(doc) == "" {
			t.Errorf("%q became %v", src, doc)
		}
	}
}

// Line breaks, the byte-order mark and tabs of the user's file are what
// Ülgen writes with.
func TestQwenKeepsTheStyleOfTheFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	src := "\xef\xbb\xbf{\r\n\t// mine\r\n\t\"ui\": { \"theme\": \"dark\" }, /* keep */\r\n\t\"modelProviders\": {\r\n\t\t\"openai\": [\r\n\t\t\t{ \"id\": \"llama\", \"envKey\": \"LOCAL\", \"baseUrl\": \"http://127.0.0.1:1/v1\" } // local\r\n\t\t]\r\n\t}\r\n}\r\n"
	path := write(t, qwenSettings(env), src)
	setUp(t, Qwen{}, env, testRequest())
	got := content(t, path)
	if !strings.HasPrefix(got, "\xef\xbb\xbf{\r\n\t// mine\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("mark or line breaks changed:\n%q", got)
	}
	if !strings.Contains(got, "\r\n\t\t\t{\r\n\t\t\t\t\"id\": \"deepseek-v4-flash\",\r\n") || strings.Contains(got, "  \"") {
		t.Errorf("the new entries are not indented with the file's tabs:\n%q", got)
	}
	for _, comment := range []string{"// mine", "/* keep */", "// local"} {
		if strings.Count(got, comment) != 1 {
			t.Errorf("the comment %q is there %d times", comment, strings.Count(got, comment))
		}
	}
	if doc := decoded(t, got); qwenIDs(doc) != "llama deepseek-v4-flash qwen3-vl-30b deepseek-v4.1-flash" || dig(doc, "ui", "theme") != "dark" {
		t.Errorf("decoded: %v", doc)
	}
}

// A settings file of Qwen Code's first format is left for Qwen Code to
// convert: in that conversion the old keys would win over what Ülgen wrote.
func TestQwenOlderFormat(t *testing.T) {
	for _, src := range []string{
		`{"theme": "Dracula", "selectedAuthType": "gemini", "usageStatisticsEnabled": true}`,
		`{"selectedAuthType": "openai"}`,
		`{"usageStatisticsEnabled": false}`,
		"{\n  // old\n  \"theme\": \"Dracula\"\n}\n",
	} {
		env := testEnv(t, nil, nil)
		path := write(t, qwenSettings(env), src)
		p, err := Qwen{}.Plan(env, testRequest())
		var se *ShapeError
		if !errors.As(err, &se) || se.Path != path || !strings.Contains(se.Detail, "older format") || !strings.Contains(se.Detail, "starting Qwen Code once converts it") || len(p.Files) != 0 {
			t.Errorf("%s: Plan = %v, %T %v", src, paths(p), err, err)
		}
		if ok, err := (Qwen{}).Configured(env); ok || !errors.As(err, &se) {
			t.Errorf("%s: Configured = %v, %v", src, ok, err)
		}
		unchanged(t, path, src)
	}
	// With "$version" the file is in a format Qwen Code names itself, and a
	// key of that name is just a key.
	env := testEnv(t, nil, nil)
	write(t, qwenSettings(env), `{"$version": 3, "theme": "Dracula"}`)
	setUp(t, Qwen{}, env, testRequest())
	if doc := decoded(t, content(t, qwenSettings(env))); doc["$version"] != float64(3) || doc["theme"] != "Dracula" {
		t.Errorf("decoded: %v", doc)
	}
}

// QWEN_HOME in ~/.qwen/.env moves Qwen Code to another directory than the
// one Ülgen wrote to. A note says so; nothing of the file goes into it.
func TestQwenHomeInTheEnvFile(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		note    bool
	}{
		"set":                 {"OTHER_KEY=sk-SENTINEL\nQWEN_HOME=/srv/qwen\n", true},
		"exported":            {"export QWEN_HOME=\"/srv/qwen\"\n", true},
		"with spaces":         {"  QWEN_HOME = '/srv/qwen'  # moved\r\n", true},
		"with a colon":        {"QWEN_HOME: /srv/qwen\n", true},
		"empty":               {"QWEN_HOME=\nQWEN_HOME=\"\"\n", false},
		"commented out":       {"# QWEN_HOME=/srv/qwen\n", false},
		"another variable":    {"QWEN_HOME_BACKUP=/srv/qwen\nMY_QWEN_HOME=/x\n", false},
		"only its name":       {"QWEN_HOME\n", false},
		"no such line at all": {"OTHER_KEY=sk-SENTINEL\n", false},
	} {
		env := testEnv(t, nil, nil)
		envFile := write(t, filepath.Join(env.Home, ".qwen", ".env"), tc.content)
		p := setUp(t, Qwen{}, env, testRequest())
		if hasNote(p, "setup.note.qwen_home_file") != tc.note {
			t.Errorf("%s: notes %v", name, p.Notes)
		}
		for _, n := range p.Notes {
			for _, arg := range n.Args {
				if s, _ := arg.(string); strings.Contains(s, "SENTINEL") || strings.Contains(s, "/srv/qwen") {
					t.Errorf("%s: the note %s carries text of the file: %v", name, n.Key, n.Args)
				}
			}
		}
		if tc.note {
			if args := noteArgs(t, p, "setup.note.qwen_home_file"); len(args) != 1 || args[0] != Path(envFile) {
				t.Errorf("%s: note arguments %v", name, args)
			}
		}
		unchanged(t, envFile, tc.content)
	}
	// The variable in the environment wins over the files: nothing to say.
	other := t.TempDir()
	env := testEnv(t, map[string]string{"QWEN_HOME": other}, nil)
	write(t, filepath.Join(env.Home, ".qwen", ".env"), "QWEN_HOME=/srv/qwen\n")
	write(t, filepath.Join(env.Home, ".env"), "QWEN_HOME=/srv/qwen\n")
	write(t, filepath.Join(other, ".env"), "QWEN_HOME=/srv/qwen\n")
	if p := setUp(t, Qwen{}, env, testRequest()); hasNote(p, "setup.note.qwen_home_file") {
		t.Errorf("with QWEN_HOME set: notes %v", p.Notes)
	}
}

// Without QWEN_HOME in the environment, Qwen Code reads ~/.qwen/.env and then
// ~/.env for it, and the first value it finds wins. The note names the file
// whose value Qwen Code takes.
func TestQwenHomeInTheHomeEnvFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	homeEnv := write(t, filepath.Join(env.Home, ".env"), "OTHER_KEY=sk-SENTINEL\nQWEN_HOME=/srv/qwen\n")
	p := setUp(t, Qwen{}, env, testRequest())
	if args := noteArgs(t, p, "setup.note.qwen_home_file"); len(args) != 1 || args[0] != Path(homeEnv) {
		t.Errorf("~/.env alone: note arguments %v", args)
	}
	qwenEnv := write(t, filepath.Join(env.Home, ".qwen", ".env"), "QWEN_HOME=/srv/other\n")
	p = setUp(t, Qwen{}, env, testRequest())
	if args := noteArgs(t, p, "setup.note.qwen_home_file"); len(args) != 1 || args[0] != Path(qwenEnv) {
		t.Errorf("both files: note arguments %v", args)
	}
	unchanged(t, homeEnv, "OTHER_KEY=sk-SENTINEL\nQWEN_HOME=/srv/qwen\n")
}

// A directory where a file belongs is reported as that, and nothing is
// planned.
func TestQwenWrongKindOfFile(t *testing.T) {
	kind := func(t *testing.T, env Env, path string, wantDir bool) {
		t.Helper()
		p, err := Qwen{}.Plan(env, testRequest())
		var ke *KindError
		if !errors.As(err, &ke) || ke.Path != path || ke.WantDir != wantDir || ke.Var != "" || len(p.Files) != 0 {
			t.Errorf("Plan = %v, %T %v", paths(p), err, err)
		}
	}
	env := testEnv(t, nil, nil)
	if err := os.MkdirAll(qwenSettings(env), 0o700); err != nil {
		t.Fatal(err)
	}
	kind(t, env, qwenSettings(env), false)
	var ke *KindError
	if ok, err := (Qwen{}).Configured(env); ok || !errors.As(err, &ke) || ke.Path != qwenSettings(env) {
		t.Errorf("Configured = %v, %T %v", ok, err, err)
	}

	for _, e := range []*KindError{{Path: "p"}, {Path: "p", WantDir: true}, {Path: "p", WantDir: true, Var: "V"}} {
		if !strings.Contains(e.Error(), "p") || !strings.Contains(e.Error(), "directory") {
			t.Errorf("KindError: %q", e.Error())
		}
	}
}

func TestQwenRefusesWhatItCannotRead(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), "{\n  \"model\": \n}\n")
	_, err := Qwen{}.Plan(env, testRequest())
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Path != path || pe.Format != "JSON" || pe.Detail != "line 3, column 1: invalid character '}' at start of value" {
		t.Errorf("err = %v", err)
	}
	if _, err := (Qwen{}).Configured(env); !errors.As(err, &pe) {
		t.Errorf("Configured err = %v", err)
	}

	// Keys that hold another kind of value than Qwen Code allows there.
	for src, detail := range map[string]string{
		`{"model": "gpt"}`:                                         "model is not an object",
		`{"modelProviders": {"openai": {"x":1}}}`:                  "modelProviders.openai is not a list",
		`{"modelProviders": null}`:                                 "modelProviders is not an object",
		`{"modelProviders": {"openai": null}}`:                     "modelProviders.openai is not a list",
		`{"modelProviders": {"openai": []}, "modelProviders": {}}`: "modelProviders appears more than once",
		`{"security": "x"}`:                                        "security is not an object",
		`{"security": {"auth": []}}`:                               "security.auth is not an object",
		`{"privacy": false}`:                                       "privacy is not an object",
		`{"model": {"name": 5}}`:                                   "model.name is not text",
		`{"model": {"name": "m", "baseUrl": ["x"]}}`:               "model.baseUrl is not text",
		`{"security": {"auth": {"selectedType": true}}}`:           "security.auth.selectedType is not text",
	} {
		write(t, path, src)
		p, err := Qwen{}.Plan(env, testRequest())
		var se *ShapeError
		if !errors.As(err, &se) || se.Path != path || se.Detail != detail || len(p.Files) != 0 {
			t.Errorf("%s: err = %v", src, err)
		}
		unchanged(t, path, src)
	}
}

// Usage statistics go to a third party: off when the user has said nothing,
// and whatever the user has said stays.
func TestQwenUsageStatistics(t *testing.T) {
	for src, want := range map[string]any{
		`{}`:              false,
		`{"privacy": {}}`: false,
		`{"privacy": {"usageStatisticsEnabled": true}}`:  true,
		`{"privacy": {"usageStatisticsEnabled": false}}`: false,
		`{"privacy": {"usageStatisticsEnabled": "yes"}}`: "yes",
		`{"privacy": {"usageStatisticsEnabled": null}}`:  nil,
	} {
		env := testEnv(t, nil, nil)
		write(t, qwenSettings(env), src)
		p := setUp(t, Qwen{}, env, testRequest())
		privacy, _ := dig(decoded(t, src), "privacy").(map[string]any)
		_, said := privacy["usageStatisticsEnabled"]
		got := dig(decoded(t, content(t, qwenSettings(env))), "privacy", "usageStatisticsEnabled")
		if got != want || hasNote(p, "setup.note.stats_off") == said {
			t.Errorf("%s: usageStatisticsEnabled %v, notes %v", src, got, p.Notes)
		}
	}
}

func TestQwenTrailingCommaNote(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), "{\n  \"ui\": {\"theme\": \"dark\"},\n}\n")
	p := setUp(t, Qwen{}, env, testRequest())
	if args := noteArgs(t, p, "setup.note.trailing_comma"); len(args) != 1 || args[0] != Path(path) {
		t.Errorf("note arguments: %v", args)
	}
	// The note is there for as long as the comma is.
	if p, err := (Qwen{}).Plan(env, testRequest()); err != nil || !hasNote(p, "setup.note.trailing_comma") || len(p.Files) != 0 {
		t.Errorf("second run: %v, notes %v", err, p.Notes)
	}
}

func TestQwenHome(t *testing.T) {
	base := t.TempDir()
	other := filepath.Join(base, "qw")
	env := testEnv(t, map[string]string{"QWEN_HOME": other}, nil)
	p, err := Qwen{}.Plan(env, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(other, "settings.json")}
	if !reflect.DeepEqual(paths(p), want) {
		t.Errorf("files = %v", paths(p))
	}

	env = testEnv(t, map[string]string{"QWEN_HOME": "~/qw"}, nil)
	if p, err = (Qwen{}).Plan(env, testRequest()); err != nil {
		t.Fatal(err)
	}
	planned(t, p, filepath.Join(env.Home, "qw", "settings.json"))
}

func TestQwenDetect(t *testing.T) {
	ctx := context.Background()
	d := Qwen{}.Detect(ctx, testEnv(t, nil, map[string]string{"qwen": "0.25.1\n"}))
	if !d.Installed || d.Version != "0.25.1" || d.Binary != testBin("qwen") || len(d.Notes) != 0 {
		t.Errorf("installed: %+v", d)
	}
	d = Qwen{}.Detect(ctx, testEnv(t, nil, nil))
	if d.Installed || len(d.Notes) != 1 || d.Notes[0].Key != "setup.note.node_missing" {
		t.Errorf("no node: %+v", d)
	}
	d = Qwen{}.Detect(ctx, testEnv(t, nil, map[string]string{"node": "v20.19.5\n"}))
	if d.Installed || len(d.Notes) != 1 || d.Notes[0].Key != "setup.note.node_old" || d.Notes[0].Args[0] != "v20.19.5" {
		t.Errorf("old node: %+v", d)
	}
	d = Qwen{}.Detect(ctx, testEnv(t, nil, map[string]string{"node": "v22.11.0\n"}))
	if d.Installed || len(d.Notes) != 0 {
		t.Errorf("new node: %+v", d)
	}
	if (Qwen{}).ID() != "qwen" || (Qwen{}).Name() != "Qwen Code" || (Qwen{}).InstallHint(Env{}).Key != "setup.install.qwen" {
		t.Error("ID, Name or InstallHint changed")
	}
}
