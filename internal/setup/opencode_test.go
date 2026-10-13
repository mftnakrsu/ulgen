package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const opencodeFresh = `{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "evren": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "EVREN",
      "env": [
        "EVREN_API_KEY"
      ],
      "options": {
        "baseURL": "https://evren-llmapi.ssyz.org.tr/v1",
        "apiKey": "{env:EVREN_API_KEY}"
      },
      "models": {
        "qwen3-vl-30b": {
          "name": "qwen3-vl-30b",
          "tool_call": true,
          "limit": {
            "context": 262144,
            "output": 8192
          },
          "modalities": {
            "input": [
              "text",
              "image"
            ],
            "output": [
              "text"
            ]
          }
        },
        "deepseek-v4-flash": {
          "name": "deepseek-v4-flash",
          "tool_call": true,
          "limit": {
            "context": 1048576,
            "output": 16384
          },
          "modalities": {
            "input": [
              "text"
            ],
            "output": [
              "text"
            ]
          }
        }
      }
    }
  },
  "model": "evren/qwen3-vl-30b",
  "small_model": "evren/deepseek-v4-flash"
}
`

// opencodeOurs is an entry of Ülgen's as a user's editor would leave it on
// one line: it carries the reference to the key and nothing else of value.
const opencodeStale = `{ "name": "an older entry", "env": ["EVREN_API_KEY"], "options": { "apiKey": "{env:EVREN_API_KEY}" } }`

func opencodeRequest() Request {
	return Request{
		BaseURL: testRoot,
		Models: []Model{
			{ID: "qwen3-vl-30b", Context: 262144, Vision: true}, // no output limit from the API
			{ID: "deepseek-v4-flash", Context: 1048576, MaxOutput: 16384},
		},
		Default: "qwen3-vl-30b",
	}
}

// opencodeHome is opencode's global configuration directory on the test
// machine.
func opencodeHome(env Env) string { return filepath.Join(env.Home, ".config", "opencode") }

// opencodeEntry is the decoded provider.evren a fresh setup writes.
func opencodeEntry(t *testing.T) any {
	t.Helper()
	return dig(decoded(t, opencodeFresh), "provider", "evren")
}

func TestOpencodeFreshMachine(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := filepath.Join(opencodeHome(env), "opencode.json")
	if ok, err := (Opencode{}).Configured(env); ok || err != nil {
		t.Errorf("Configured before setup = %v, %v", ok, err)
	}
	p, err := Opencode{}.Plan(env, opencodeRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got := planned(t, p, path); got != opencodeFresh {
		t.Errorf("opencode.json:\n%s", got)
	}
	if len(p.Files) != 1 || len(p.Notes) != 1 || !hasNote(p, "setup.note.export") {
		t.Errorf("files = %v, notes = %v", paths(p), p.Notes)
	}
	applyAndReplan(t, Opencode{}, env, opencodeRequest(), p)
	if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
}

func TestOpencodeKeepsTheUsersFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, filepath.Join(opencodeHome(env), "opencode.jsonc"), `{
  // my opencode
  "model": "anthropic/claude-sonnet-5-5", // daily driver
  "provider": {
    "local": { "npm": "@ai-sdk/openai-compatible", "options": { "baseURL": "http://127.0.0.1:8080/v1" } },
  },
}
`)
	req := opencodeRequest()
	req.Models = req.Models[1:]
	req.Default = "deepseek-v4-flash"
	p, err := Opencode{}.Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  // my opencode
  "model": "anthropic/claude-sonnet-5-5", // daily driver
  "provider": {
    "local": { "npm": "@ai-sdk/openai-compatible", "options": { "baseURL": "http://127.0.0.1:8080/v1" } },
    "evren": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "EVREN",
      "env": [
        "EVREN_API_KEY"
      ],
      "options": {
        "baseURL": "https://evren-llmapi.ssyz.org.tr/v1",
        "apiKey": "{env:EVREN_API_KEY}"
      },
      "models": {
        "deepseek-v4-flash": {
          "name": "deepseek-v4-flash",
          "tool_call": true,
          "limit": {
            "context": 1048576,
            "output": 16384
          },
          "modalities": {
            "input": [
              "text"
            ],
            "output": [
              "text"
            ]
          }
        }
      }
    },
  },
}
`
	if got := planned(t, p, path); got != want {
		t.Errorf("opencode.jsonc:\n%s", got)
	}
	if args := noteArgs(t, p, "setup.note.default_kept"); len(p.Files) != 1 || len(args) != 2 || args[0] != "opencode" || args[1] != "opencode" {
		t.Errorf("files = %v, notes = %v", paths(p), p.Notes)
	}
	applyAndReplan(t, Opencode{}, env, req, p)

	// --default takes the default over, both of its keys.
	req.MakeDefault = true
	p = setUp(t, Opencode{}, env, req)
	got := content(t, path)
	if doc := decoded(t, got); doc["model"] != "evren/deepseek-v4-flash" || doc["small_model"] != "evren/deepseek-v4-flash" || hasNote(p, "setup.note.default_kept") {
		t.Errorf("model %v, small_model %v, notes %v", doc["model"], doc["small_model"], p.Notes)
	}
	if !strings.Contains(got, `"model": "evren/deepseek-v4-flash", // daily driver`) {
		t.Errorf("the comment next to the model is gone:\n%s", got)
	}
}

// opencode reads several files of the user and takes each key from the last
// one that has it. A default in a file beneath the one Ülgen writes is the
// tool's default all the same, and Ülgen does not take it over.
func TestOpencodeDefaultInAnotherFile(t *testing.T) {
	const theirs = `{"model": "anthropic/claude-sonnet-5-5", "small_model": "anthropic/claude-haiku-5"}`
	for name, tc := range map[string]struct {
		files  map[string]string // name in the global directory, or "~/" + path under the home directory
		custom map[string]string // files in $OPENCODE_CONFIG_DIR; nil when it is not set
		writes string            // the file Ülgen writes
	}{
		"opencode.json beneath opencode.jsonc": {
			files: map[string]string{"opencode.json": theirs, "opencode.jsonc": `{ "theme": "tokyonight" }`}, writes: "opencode.jsonc"},
		"only config.json, the older name": {
			files: map[string]string{"config.json": theirs}, writes: "opencode.json"},
		"config.json beneath opencode.json": {
			files: map[string]string{"config.json": theirs, "opencode.json": `{ "theme": "tokyonight" }`}, writes: "opencode.json"},
		"the global directory beneath OPENCODE_CONFIG_DIR": {
			files: map[string]string{"opencode.json": theirs}, custom: map[string]string{}, writes: "opencode.json"},
		"the global opencode.jsonc beneath OPENCODE_CONFIG_DIR": {
			files: map[string]string{"opencode.jsonc": theirs}, custom: map[string]string{"opencode.jsonc": "// mine\n{}"}, writes: "opencode.jsonc"},
		"~/.opencode beneath OPENCODE_CONFIG_DIR": {
			files: map[string]string{"~/.opencode/opencode.json": theirs}, custom: map[string]string{}, writes: "opencode.json"},
	} {
		t.Run(name, func(t *testing.T) {
			var vars map[string]string
			custom := t.TempDir()
			if tc.custom != nil {
				vars = map[string]string{"OPENCODE_CONFIG_DIR": custom}
			}
			env := testEnv(t, vars, nil)
			dir := opencodeHome(env)
			for file, text := range tc.files {
				if rest, underHome := strings.CutPrefix(file, "~/"); underHome {
					write(t, filepath.Join(env.Home, filepath.FromSlash(rest)), text)
				} else {
					write(t, filepath.Join(dir, file), text)
				}
			}
			if tc.custom != nil {
				dir = custom
				for file, text := range tc.custom {
					write(t, filepath.Join(dir, file), text)
				}
			}
			target := filepath.Join(dir, tc.writes)
			p := setUp(t, Opencode{}, env, opencodeRequest())
			doc := decoded(t, content(t, target))
			if _, has := doc["model"]; has || doc["small_model"] != nil || !hasNote(p, "setup.note.default_kept") {
				t.Errorf("model %v, small_model %v, notes %v", doc["model"], doc["small_model"], p.Notes)
			}
			if !reflect.DeepEqual(dig(doc, "provider", "evren"), opencodeEntry(t)) || !reflect.DeepEqual(paths(p), []string{target}) {
				t.Errorf("provider.evren = %v, files %v", dig(doc, "provider", "evren"), paths(p))
			}
			// With --default the file Ülgen writes gets the default; the other
			// file stays the user's.
			req := opencodeRequest()
			req.MakeDefault = true
			setUp(t, Opencode{}, env, req)
			if doc := decoded(t, content(t, target)); doc["model"] != "evren/qwen3-vl-30b" || doc["small_model"] != "evren/deepseek-v4-flash" {
				t.Errorf("--default: model %v, small_model %v", doc["model"], doc["small_model"])
			}
		})
	}
}

// The value of the last file that has the key counts, whatever a file
// beneath it says.
func TestOpencodeTheLastFileDecides(t *testing.T) {
	for name, tc := range map[string]struct {
		lower, upper string
		evren        bool
	}{
		"EVREN's over another": {`{"model": "anthropic/claude"}`, `{"model": "evren/qwen3-vl-30b"}`, true},
		"another over EVREN's": {`{"model": "evren/qwen3-vl-30b"}`, `{"model": "anthropic/claude"}`, false},
		"EVREN's beneath":      {`{"model": "evren/qwen3-vl-30b"}`, `{"theme": "x"}`, true},
		"an empty one on top":  {`{"model": "anthropic/claude"}`, `{"model": ""}`, false},
	} {
		env := testEnv(t, nil, nil)
		lower := write(t, filepath.Join(opencodeHome(env), "opencode.json"), tc.lower)
		upper := write(t, filepath.Join(opencodeHome(env), "opencode.jsonc"), tc.upper)
		req := opencodeRequest()
		req.Default, req.ModelGiven = "deepseek-v4-flash", true
		p := setUp(t, Opencode{}, env, req)
		unchanged(t, lower, tc.lower)
		doc := decoded(t, content(t, upper))
		switch name {
		case "an empty one on top": // nothing is a default there: the request's is written
			if doc["model"] != "evren/deepseek-v4-flash" || hasNote(p, "setup.note.default_kept") {
				t.Errorf("%s: model %v, notes %v", name, doc["model"], p.Notes)
			}
		default:
			want := decoded(t, tc.upper)["model"]
			if tc.evren {
				want = "evren/deepseek-v4-flash"
			}
			if doc["model"] != want || hasNote(p, "setup.note.default_kept") == tc.evren {
				t.Errorf("%s: model %v, want %v; notes %v", name, doc["model"], want, p.Notes)
			}
		}
	}
}

// EVREN is set up when any of the files opencode merges holds Ülgen's
// entry. When the user starts an opencode.jsonc next to the opencode.json
// Ülgen wrote, a new run writes the entry there and says that the copy
// beneath it is no longer kept up to date.
func TestOpencodeACopyBeneathTheFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	setUp(t, Opencode{}, env, opencodeRequest())
	lower := filepath.Join(opencodeHome(env), "opencode.json")
	before := content(t, lower)
	upper := write(t, filepath.Join(opencodeHome(env), "opencode.jsonc"), "{\n  // mine\n  \"theme\": \"tokyonight\"\n}\n")
	if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
	req := opencodeRequest()
	req.Models = req.Models[:1]
	p := setUp(t, Opencode{}, env, req)
	unchanged(t, lower, before)
	doc := decoded(t, content(t, upper))
	models, _ := dig(doc, "provider", "evren", "models").(map[string]any)
	// The default was Ülgen's, in the lower file, and still is: it is not
	// written again. small_model there names a model that is gone, so it is
	// repaired where Ülgen writes.
	if _, has := doc["model"]; has || len(models) != 1 || doc["small_model"] != "evren/qwen3-vl-30b" || doc["theme"] != "tokyonight" {
		t.Errorf("opencode.jsonc: %v", doc)
	}
	if args := noteArgs(t, p, "setup.note.opencode_copy"); len(args) != 2 || args[0] != Path(lower) || args[1] != Path(upper) {
		t.Errorf("note arguments: %v", args)
	}
	if args := noteArgs(t, p, "setup.note.small_replaced"); len(args) != 2 || args[0] != "deepseek-v4-flash" || args[1] != "qwen3-vl-30b" {
		t.Errorf("note arguments: %v", args)
	}
	if hasNote(p, "setup.note.default_kept") || hasNote(p, "setup.note.default_replaced") {
		t.Errorf("notes = %v", p.Notes)
	}
	// The note is there for as long as the copy is.
	if p, err := (Opencode{}).Plan(env, req); err != nil || !hasNote(p, "setup.note.opencode_copy") || len(p.Files) != 0 {
		t.Errorf("second run: %v, notes %v, files %v", err, p.Notes, paths(p))
	}
}

// A provider.evren without the reference to the key is not Ülgen's: it is
// the user's own provider that happens to have the name. Ülgen refuses, in
// whichever of the files it stands, and changes nothing.
func TestOpencodeRefusesAProviderOfTheUsersOwn(t *testing.T) {
	const corp = `{
  "provider": {
    // my company's gateway
    "evren": {
      "npm": "@ai-sdk/anthropic",
      "name": "Evren Corp gateway",
      "options": {
        "baseURL": "https://llm.evren-corp.example/v1",
        "apiKey": "{file:~/.secrets/evren-corp}",
        "headers": { "X-Team": "platform" }, // required
        "timeout": 600000
      },
      "models": { "claude-sonnet-5-5": { "name": "Sonnet" } }
    }
  },
  "model": "evren/claude-sonnet-5-5"
}
`
	for name, tc := range map[string]struct {
		file, src string
		shape     string // the detail of a *ShapeError; "" for a *ForeignEntryError
	}{
		"the user's own provider":       {"opencode.json", corp, ""},
		"an empty entry":                {"opencode.json", `{"provider": {"evren": {}}}`, ""},
		"another variable":              {"opencode.jsonc", `{"provider": {"evren": {"env": ["OTHER_KEY"], "options": {"apiKey": "{env:OTHER_KEY}"}}}}`, ""},
		"the key itself":                {"opencode.json", `{"provider": {"evren": {"options": {"apiKey": "invented-key-value"}}}}`, ""},
		"env is not a list":             {"opencode.json", `{"provider": {"evren": {"env": "EVREN_API_KEY"}}}`, ""},
		"in a file beneath":             {"config.json", corp, ""},
		"a string":                      {"opencode.json", `{"provider": {"evren": "x"}}`, "provider.evren is not an object"},
		"null":                          {"opencode.json", `{"provider": {"evren": null}}`, "provider.evren is not an object"},
		"a list":                        {"opencode.jsonc", `{"provider": {"evren": []}}`, "provider.evren is not an object"},
		"a string in a file beneath":    {"config.json", `{"provider": {"evren": "x"}}`, "provider.evren is not an object"},
		"provider is a string":          {"opencode.json", `{"provider": "none"}`, "provider is not an object"},
		"provider is null":              {"opencode.json", `{"provider": null}`, "provider is not an object"},
		"provider is a list beneath":    {"config.json", `{"provider": []}`, "provider is not an object"},
		"provider stands twice":         {"opencode.json", `{"provider": {}, "provider": {}}`, "provider appears more than once"},
		"the entry stands twice, as it": {"opencode.json", `{"provider": {"evren": ` + opencodeStale + `, "evren": ` + opencodeStale + `}}`, "provider.evren appears more than once"},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			path := write(t, filepath.Join(opencodeHome(env), tc.file), tc.src)
			p, err := Opencode{}.Plan(env, opencodeRequest())
			configured, cerr := Opencode{}.Configured(env)
			var fe *ForeignEntryError
			var se *ShapeError
			switch {
			case len(p.Files) != 0:
				t.Errorf("the plan writes %v", paths(p))
			case tc.shape != "":
				if !errors.As(err, &se) || se.Path != path || se.Detail != tc.shape {
					t.Errorf("Plan: %T %v", err, err)
				}
			case !errors.As(err, &fe) || fe.Path != path || fe.Entry != "provider.evren":
				t.Errorf("Plan: %T %v", err, err)
			case !errors.As(cerr, &fe) || fe.Path != path:
				t.Errorf("Configured: %T %v", cerr, cerr)
			}
			// A key that stands twice is found by the editor, when Plan
			// writes; opencode itself reads the last one, and so does
			// Configured.
			if twice := strings.HasSuffix(tc.shape, "appears more than once"); configured && !(twice && strings.Contains(tc.src, "EVREN_API_KEY")) {
				t.Error("Configured is true")
			}
			unchanged(t, path, tc.src)
			if entries, _ := os.ReadDir(opencodeHome(env)); len(entries) != 1 {
				t.Errorf("the directory holds %d files", len(entries))
			}
		})
	}
	if got := (&ForeignEntryError{Path: "p", Entry: "provider.evren"}).Error(); got != "p defines provider.evren, an entry that is not ulgen's" {
		t.Errorf("ForeignEntryError: %q", got)
	}
}

// Inside its own entry Ülgen sets its keys and nothing else: what the user
// added next to them stays, comments included, and a key that already says
// what it should keeps its bytes.
func TestOpencodeKeepsWhatTheUserAddedToTheEntry(t *testing.T) {
	env := testEnv(t, nil, nil)
	src := `{
  "provider": {
    "evren": {
      "name": "EVREN (work)", // my label
      "env": ["EVREN_API_KEY", "EVREN_ALT_KEY"],
      "options": {
        "apiKey": "{env:EVREN_API_KEY}",
        "timeout": 600000, // slow link
        "headers": { "X-Team": "platform" },
        "baseURL": "https://old.example/v1"
      },
      "whitelist": ["qwen3-vl-30b"]
    }
  }
}
`
	path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), src)
	if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured = %v, %v", ok, err)
	}
	setUp(t, Opencode{}, env, opencodeRequest())
	got := content(t, path)
	entry, _ := dig(decoded(t, got), "provider", "evren").(map[string]any)
	fresh, _ := opencodeEntry(t).(map[string]any)
	want := map[string]any{
		"npm": fresh["npm"], "name": "EVREN", "models": fresh["models"],
		"env":       []any{"EVREN_API_KEY", "EVREN_ALT_KEY"},
		"whitelist": []any{"qwen3-vl-30b"},
		"options": map[string]any{
			"apiKey": "{env:EVREN_API_KEY}", "baseURL": testRoot,
			"timeout": float64(600000), "headers": map[string]any{"X-Team": "platform"},
		},
	}
	if !reflect.DeepEqual(entry, want) {
		t.Errorf("provider.evren = %v\nwant %v", entry, want)
	}
	for _, kept := range []string{
		"\"env\": [\"EVREN_API_KEY\", \"EVREN_ALT_KEY\"],\n", "\"apiKey\": \"{env:EVREN_API_KEY}\",\n",
		"\"timeout\": 600000, // slow link\n", "\"headers\": { \"X-Team\": \"platform\" },\n", "// my label\n",
	} {
		if !strings.Contains(got, kept) {
			t.Errorf("%q is not in the file as it was:\n%s", kept, got)
		}
	}

	// An entry that is Ülgen's by its env list alone gets the key reference.
	env = testEnv(t, nil, nil)
	path = write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{"provider": {"evren": {"env": ["EVREN_API_KEY"], "options": {"apiKey": "invented-key-value"}}}}`)
	p := setUp(t, Opencode{}, env, opencodeRequest())
	if got := content(t, path); strings.Contains(got, "invented-key-value") || dig(decoded(t, got), "provider", "evren", "options", "apiKey") != "{env:EVREN_API_KEY}" || !hasNote(p, "setup.note.opencode_key_replaced") {
		t.Errorf("notes %v, opencode.json:\n%s", p.Notes, got)
	}

	// Keys in another order, or models in another order than the API's, are
	// the same entry: nothing is written.
	env = testEnv(t, nil, nil)
	write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{
  "provider": { "evren": { "models": { "deepseek-v4-flash": { "modalities": { "output": ["text"], "input": ["text"] }, "limit": { "output": 16384, "context": 1048576 }, "tool_call": true, "name": "deepseek-v4-flash" }, "qwen3-vl-30b": { "name": "qwen3-vl-30b", "tool_call": true, "limit": { "context": 262144, "output": 8192 }, "modalities": { "input": ["text", "image"], "output": ["text"] } } }, "options": { "apiKey": "{env:EVREN_API_KEY}", "baseURL": "https://evren-llmapi.ssyz.org.tr/v1" }, "name": "EVREN", "npm": "@ai-sdk/openai-compatible" } },
  "model": "evren/deepseek-v4-flash", "small_model": "evren/deepseek-v4-flash"
}
`)
	if p, err := (Opencode{}).Plan(env, opencodeRequest()); err != nil || len(p.Files) != 0 {
		t.Errorf("another order: %v, files %v", err, paths(p))
	}
}

// A small_model of EVREN's that names a model EVREN no longer offers is
// repaired like the default model, whatever the default is.
func TestOpencodeRetiredDefaultsAreRepaired(t *testing.T) {
	for name, tc := range map[string]struct {
		src                 string
		model, small        any
		replaced, smallGone string // the model each note names; "" for no note
	}{
		"both gone":                 {`{"model": "evren/gone-model", "small_model": "evren/gone-small"}`, "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", "gone-model", "gone-small"},
		"the small one gone":        {`{"model": "evren/qwen3-vl-30b", "small_model": "evren/gone-small"}`, "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", "", "gone-small"},
		"next to another":           {`{"model": "anthropic/claude", "small_model": "evren/gone-small"}`, "anthropic/claude", "evren/deepseek-v4-flash", "", "gone-small"},
		"the default gone":          {`{"model": "evren/gone-model", "small_model": "anthropic/haiku"}`, "evren/qwen3-vl-30b", "anthropic/haiku", "gone-model", ""},
		"gone, with a variant":      {`{"model": "evren/gone-model#high"}`, "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", "gone-model", ""},
		"offered, with variant":     {`{"model": "evren/qwen3-vl-30b#high", "small_model": "evren/qwen3-vl-30b"}`, "evren/qwen3-vl-30b#high", "evren/qwen3-vl-30b", "", ""},
		"gone, the object form":     {`{"model": {"providerID": "evren", "model": "gone-model"}}`, "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", "gone-model", ""},
		"another's object form":     {`{"model": {"providerID": "anthropic", "model": "gone-model"}}`, map[string]any{"providerID": "anthropic", "model": "gone-model"}, nil, "", ""},
		"an object without a model": {`{"model": {"providerID": "evren"}}`, map[string]any{"providerID": "evren"}, nil, "", ""},
	} {
		env := testEnv(t, nil, nil)
		path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), tc.src)
		p := setUp(t, Opencode{}, env, opencodeRequest())
		doc := decoded(t, content(t, path))
		if !reflect.DeepEqual(doc["model"], tc.model) || !reflect.DeepEqual(doc["small_model"], tc.small) {
			t.Errorf("%s: model %v, small_model %v", name, doc["model"], doc["small_model"])
		}
		for key, was := range map[string]string{"setup.note.default_replaced": tc.replaced, "setup.note.small_replaced": tc.smallGone} {
			if hasNote(p, key) != (was != "") {
				t.Errorf("%s: %s: notes %v", name, key, p.Notes)
			} else if was != "" && noteArgs(t, p, key)[0] != was {
				t.Errorf("%s: %s names %v", name, key, noteArgs(t, p, key))
			}
		}
	}
	// A default of another kind than opencode allows is not guessed at.
	for _, src := range []string{`{"model": 5}`, `{"small_model": ["evren/x"]}`, `{"model": true}`} {
		env := testEnv(t, nil, nil)
		path := write(t, filepath.Join(opencodeHome(env), "config.json"), src)
		_, err := Opencode{}.Plan(env, opencodeRequest())
		var se *ShapeError
		if !errors.As(err, &se) || se.Path != path || !strings.HasSuffix(se.Detail, "is neither text nor an object") {
			t.Errorf("%s: %T %v", src, err, err)
		}
	}
}

// A model whose context length is not known gets no limit: opencode wants
// both limits or neither, and a 0 would be taken as the limit.
func TestOpencodeLimits(t *testing.T) {
	env := testEnv(t, nil, nil)
	req := Request{BaseURL: testRoot, Default: "known", Models: []Model{
		{ID: "known", Context: 4096, MaxOutput: 512}, {ID: "no-output", Context: 4096}, {ID: "negative-output", Context: 4096, MaxOutput: -5},
		{ID: "zero", MaxOutput: 512}, {ID: "negative", Context: -1},
	}}
	p := setUp(t, Opencode{}, env, req)
	models, _ := dig(plannedDoc(t, p, filepath.Join(opencodeHome(env), "opencode.json")), "provider", "evren", "models").(map[string]any)
	for id, want := range map[string]any{
		"known":           map[string]any{"context": float64(4096), "output": float64(512)},
		"no-output":       map[string]any{"context": float64(4096), "output": float64(8192)},
		"negative-output": map[string]any{"context": float64(4096), "output": float64(8192)},
		"zero":            nil,
		"negative":        nil,
	} {
		if got := dig(models[id], "limit"); !reflect.DeepEqual(got, want) || dig(models[id], "tool_call") != true {
			t.Errorf("%s: limit %v, want %v", id, got, want)
		}
	}
}

// A file that is there and holds no value is a new file with comments: it
// gets "$schema", and what it held stays in front. An existing object gets
// none.
func TestOpencodeAFileWithoutAValue(t *testing.T) {
	for name, src := range map[string]string{
		"empty":             "",
		"whitespace":        " \n",
		"a comment":         "// my opencode\n",
		"a block comment":   "/* nothing yet */",
		"a byte-order mark": "\xef\xbb\xbf",
	} {
		for _, file := range []string{"opencode.json", "opencode.jsonc"} {
			env := testEnv(t, nil, nil)
			path := write(t, filepath.Join(opencodeHome(env), file), src)
			if ok, err := (Opencode{}).Configured(env); ok || err != nil {
				t.Errorf("%s: Configured = %v, %v", name, ok, err)
			}
			setUp(t, Opencode{}, env, opencodeRequest())
			if got := content(t, path); !strings.HasPrefix(got, src) || !reflect.DeepEqual(decoded(t, got), decoded(t, opencodeFresh)) {
				t.Errorf("%s, %s: %q", name, file, got)
			}
		}
	}
	for _, src := range []string{"{}", "{ }\n", "{\n  // mine\n}\n"} {
		env := testEnv(t, nil, nil)
		path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), src)
		setUp(t, Opencode{}, env, opencodeRequest())
		doc := decoded(t, content(t, path))
		if _, has := doc["$schema"]; has || doc["model"] != "evren/qwen3-vl-30b" {
			t.Errorf("%q became %v", src, doc)
		}
	}
}

// Line breaks, the byte-order mark, tabs and trailing commas of the user's
// file are what Ülgen writes with.
func TestOpencodeKeepsTheStyleOfTheFile(t *testing.T) {
	env := testEnv(t, nil, nil)
	src := "\xef\xbb\xbf{\r\n\t// tabs\r\n\t\"theme\": \"x\",\r\n\t\"provider\": {\r\n\t\t\"local\": { \"npm\": \"x\" }, // mine\r\n\t},\r\n}\r\n"
	path := write(t, filepath.Join(opencodeHome(env), "opencode.jsonc"), src)
	setUp(t, Opencode{}, env, opencodeRequest())
	got := content(t, path)
	if !strings.HasPrefix(got, "\xef\xbb\xbf{\r\n\t// tabs\r\n\t\"theme\": \"x\",\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("mark or line breaks changed:\n%q", got)
	}
	if !strings.Contains(got, "\r\n\t\t\"evren\": {\r\n\t\t\t\"npm\": \"@ai-sdk/openai-compatible\",\r\n") || !strings.HasSuffix(got, ",\r\n}\r\n") {
		t.Errorf("the entry is not written in the file's style:\n%q", got)
	}
	if strings.Count(got, "// tabs") != 1 || strings.Count(got, "// mine") != 1 {
		t.Errorf("a comment is gone or doubled:\n%q", got)
	}
	doc := decoded(t, got)
	if doc["theme"] != "x" || dig(doc, "provider", "local", "npm") != "x" || !reflect.DeepEqual(dig(doc, "provider", "evren"), opencodeEntry(t)) {
		t.Errorf("decoded: %v", doc)
	}
}

// Settings that keep EVREN from working although it is set up get a note,
// with the file they stand in; the setup is made all the same.
func TestOpencodeNotesWhatDefeatsTheSetup(t *testing.T) {
	for name, tc := range map[string]struct {
		file, src string
		note      string // "" for none
	}{
		"evren is disabled":              {"opencode.json", `{"disabled_providers": ["anthropic", "evren"]}`, "setup.note.opencode_disabled"},
		"disabled in a file beneath":     {"config.json", `{"disabled_providers": ["evren"]}`, "setup.note.opencode_disabled"},
		"others are disabled":            {"opencode.json", `{"disabled_providers": ["anthropic"]}`, ""},
		"only others are enabled":        {"opencode.json", `{"enabled_providers": ["anthropic"], "model": "anthropic/claude"}`, "setup.note.opencode_not_enabled"},
		"evren is among the enabled":     {"opencode.json", `{"enabled_providers": ["anthropic", "evren"]}`, ""},
		"an empty list of enabled ones":  {"opencode.json", `{"enabled_providers": []}`, "setup.note.opencode_not_enabled"},
		"no list of enabled ones (null)": {"opencode.json", `{"enabled_providers": null}`, ""},
		"a provider in the native form":  {"opencode.jsonc", `{"providers": {"evren": {"name": "native", "settings": {"baseURL": "https://x.example"}}}}`, "setup.note.opencode_native"},
		"another in the native form":     {"opencode.jsonc", `{"providers": {"other": {}}}`, ""},
		"lists of another kind":          {"opencode.json", `{"disabled_providers": "evren", "enabled_providers": {"evren": false}}`, ""},
	} {
		env := testEnv(t, nil, nil)
		path := write(t, filepath.Join(opencodeHome(env), tc.file), tc.src)
		p := setUp(t, Opencode{}, env, opencodeRequest())
		for _, key := range []string{"setup.note.opencode_disabled", "setup.note.opencode_not_enabled", "setup.note.opencode_native"} {
			if hasNote(p, key) != (key == tc.note) {
				t.Errorf("%s: %s: notes %v", name, key, p.Notes)
			}
		}
		if tc.note != "" {
			if args := noteArgs(t, p, tc.note); len(args) != 1 || args[0] != Path(path) {
				t.Errorf("%s: note arguments %v", name, args)
			}
		}
		if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
			t.Errorf("%s: Configured = %v, %v", name, ok, err)
		}
	}
	// OPENCODE_CONFIG names one more file, which Ülgen does not read.
	env := testEnv(t, map[string]string{"OPENCODE_CONFIG": filepath.Join(t.TempDir(), "extra.json")}, nil)
	if p := setUp(t, Opencode{}, env, opencodeRequest()); !hasNote(p, "setup.note.opencode_config_var") {
		t.Errorf("OPENCODE_CONFIG: notes %v", p.Notes)
	}
	if p := setUp(t, Opencode{}, testEnv(t, nil, nil), opencodeRequest()); hasNote(p, "setup.note.opencode_config_var") {
		t.Errorf("no OPENCODE_CONFIG: notes %v", p.Notes)
	}
}

func TestOpencodeModelOptionChangesAnEvrenDefault(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, filepath.Join(opencodeHome(env), "opencode.json"),
		"{\n  \"model\": {\"providerID\": \"evren\", \"model\": \"deepseek-v4-flash\"},\n  \"small_model\": \"evren/qwen3-vl-30b\"\n}\n")
	req := opencodeRequest()
	p := setUp(t, Opencode{}, env, req)
	doc := decoded(t, content(t, path))
	if !reflect.DeepEqual(doc["model"], map[string]any{"providerID": "evren", "model": "deepseek-v4-flash"}) || hasNote(p, "setup.note.default_kept") {
		t.Errorf("without --model: model %v, notes %v", doc["model"], p.Notes)
	}
	req.ModelGiven = true
	setUp(t, Opencode{}, env, req)
	// --model names the default model; the model for titles is the user's.
	if doc := decoded(t, content(t, path)); doc["model"] != "evren/qwen3-vl-30b" || doc["small_model"] != "evren/qwen3-vl-30b" {
		t.Errorf("with --model: model %v, small_model %v", doc["model"], doc["small_model"])
	}
}

func TestOpencodeDirectories(t *testing.T) {
	custom := t.TempDir()
	env := testEnv(t, map[string]string{"OPENCODE_CONFIG_DIR": custom, "XDG_CONFIG_HOME": t.TempDir()}, nil)
	p, err := Opencode{}.Plan(env, opencodeRequest())
	if err != nil {
		t.Fatal(err)
	}
	planned(t, p, filepath.Join(custom, "opencode.json"))

	xdg := t.TempDir()
	env = testEnv(t, map[string]string{"XDG_CONFIG_HOME": xdg}, nil)
	if p, err = (Opencode{}).Plan(env, opencodeRequest()); err != nil {
		t.Fatal(err)
	}
	planned(t, p, filepath.Join(xdg, "opencode", "opencode.json"))

	// The files opencode merges, lowest first, and the two names of the file
	// Ülgen writes. ~/.opencode lies over the global directory, and beneath
	// OPENCODE_CONFIG_DIR when that is set.
	global := filepath.Join(xdg, "opencode")
	in := func(dir string, names ...string) []string {
		var out []string
		for _, name := range names {
			out = append(out, filepath.Join(dir, name))
		}
		return out
	}
	check := func(name string, env Env, dir string, want []string) {
		t.Helper()
		layout, err := opencodePaths(env)
		if err != nil || !reflect.DeepEqual(layout.paths, want) {
			t.Errorf("%s: paths = %v, %v", name, layout.paths, err)
		}
		if layout.json != filepath.Join(dir, "opencode.json") || layout.jsonc != filepath.Join(dir, "opencode.jsonc") || layout.home != filepath.Join(env.Home, ".opencode") {
			t.Errorf("%s: layout = %+v", name, layout)
		}
	}
	home := filepath.Join(env.Home, ".opencode")
	check("no OPENCODE_CONFIG_DIR", env, global,
		append(in(global, "config.json", "opencode.json", "opencode.jsonc"), in(home, "opencode.json", "opencode.jsonc")...))

	env = testEnv(t, map[string]string{"XDG_CONFIG_HOME": xdg, "OPENCODE_CONFIG_DIR": custom}, nil)
	home = filepath.Join(env.Home, ".opencode")
	want := append(in(global, "config.json", "opencode.json", "opencode.jsonc"), in(home, "opencode.json", "opencode.jsonc")...)
	check("with OPENCODE_CONFIG_DIR", env, custom, append(want, in(custom, "opencode.json", "opencode.jsonc")...))

	// A directory that is named twice counts where opencode reads it last.
	env = testEnv(t, map[string]string{"XDG_CONFIG_HOME": xdg, "OPENCODE_CONFIG_DIR": global + string(filepath.Separator)}, nil)
	home = filepath.Join(env.Home, ".opencode")
	want = append(in(global, "config.json"), in(home, "opencode.json", "opencode.jsonc")...)
	check("the global directory named twice", env, global, append(want, in(global, "opencode.json", "opencode.jsonc")...))

	env = testEnv(t, map[string]string{"XDG_CONFIG_HOME": xdg}, nil)
	home = filepath.Join(env.Home, ".opencode")
	env.Getenv = func(key string) string {
		return map[string]string{"XDG_CONFIG_HOME": xdg, "OPENCODE_CONFIG_DIR": home}[key]
	}
	check("OPENCODE_CONFIG_DIR is ~/.opencode", env, home,
		append(in(global, "config.json", "opencode.json", "opencode.jsonc"), in(home, "opencode.json", "opencode.jsonc")...))
}

// ~/.opencode/opencode.json and .jsonc are files of the user that both lines
// of opencode read. Without OPENCODE_CONFIG_DIR they lie over the file Ülgen
// writes: what they say decides, Ülgen does not write a key they set, and
// nothing of the user's in them is taken for Ülgen's.
func TestOpencodeFilesOfTheHomeDirectory(t *testing.T) {
	type note struct {
		key  string
		args []any // without the path of the file, which comes first
	}
	for name, tc := range map[string]struct {
		src    string
		option string // "", "--default" or "--model" (deepseek-v4-flash)
		model  any    // "model" in the file Ülgen writes afterwards; nil when absent
		small  any    // "small_model" there
		notes  []note // the notes that name the file in ~/.opencode
		kept   bool   // the default_kept note
	}{
		"a default of the user's": {`{"model": "anthropic/claude", "small_model": "anthropic/haiku"}`, "", nil, nil, nil, true},
		"the same with --default": {`{"model": "anthropic/claude", "small_model": "anthropic/haiku"}`, "--default", nil, nil,
			[]note{{"setup.note.opencode_set_above", []any{"model", "evren/qwen3-vl-30b"}}, {"setup.note.opencode_set_above", []any{"small_model", "evren/deepseek-v4-flash"}}}, false},
		"only the model, with --default": {`{"model": "anthropic/claude"}`, "--default", nil, "evren/deepseek-v4-flash",
			[]note{{"setup.note.opencode_set_above", []any{"model", "evren/qwen3-vl-30b"}}}, false},
		"EVREN's default": {`{"model": "evren/qwen3-vl-30b"}`, "", nil, nil, nil, false},
		"EVREN's default and --model": {`{"model": "evren/qwen3-vl-30b"}`, "--model", nil, "evren/deepseek-v4-flash",
			[]note{{"setup.note.opencode_set_above", []any{"model", "evren/deepseek-v4-flash"}}}, false},
		"a model that is gone": {`{"model": "evren/gone"}`, "", nil, "evren/deepseek-v4-flash",
			[]note{{"setup.note.opencode_set_above", []any{"model", "evren/qwen3-vl-30b"}}}, false},
		"an empty model": {`{"model": ""}`, "", nil, "evren/deepseek-v4-flash",
			[]note{{"setup.note.opencode_set_above", []any{"model", "evren/qwen3-vl-30b"}}}, false},
		"a small model that is gone": {`{"small_model": "evren/gone"}`, "", "evren/qwen3-vl-30b", nil,
			[]note{{"setup.note.opencode_set_above", []any{"small_model", "evren/deepseek-v4-flash"}}}, false},
		"only a small model of the user's": {`{"small_model": "anthropic/haiku"}`, "", "evren/qwen3-vl-30b", nil, nil, false},
		"evren is disabled":                {`{"disabled_providers": ["evren"]}`, "", "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", []note{{"setup.note.opencode_disabled", nil}}, false},
		"an entry of Ülgen's":              {`{"provider": {"evren": ` + opencodeStale + `}}`, "", "evren/qwen3-vl-30b", "evren/deepseek-v4-flash", []note{{"setup.note.opencode_home_copy", nil}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			env := testEnv(t, nil, nil)
			above := write(t, filepath.Join(env.Home, ".opencode", "opencode.json"), tc.src)
			target := filepath.Join(opencodeHome(env), "opencode.json")
			if ok, err := (Opencode{}).Configured(env); ok || err != nil {
				t.Errorf("Configured before setup = %v, %v", ok, err)
			}
			req := opencodeRequest()
			switch tc.option {
			case "--default":
				req.MakeDefault = true
			case "--model":
				req.Default, req.ModelGiven = "deepseek-v4-flash", true
			}
			p := setUp(t, Opencode{}, env, req)
			unchanged(t, above, tc.src)
			doc := decoded(t, content(t, target))
			if !reflect.DeepEqual(doc["model"], tc.model) || !reflect.DeepEqual(doc["small_model"], tc.small) || !reflect.DeepEqual(dig(doc, "provider", "evren"), dig(decoded(t, planned(t, p, target)), "provider", "evren")) {
				t.Errorf("the file Ülgen writes: model %v, small_model %v", doc["model"], doc["small_model"])
			}
			var got []note
			for _, n := range p.Notes {
				if len(n.Args) > 0 && n.Args[0] == Path(above) {
					got = append(got, note{n.Key, n.Args[1:]})
				}
			}
			if len(got) != len(tc.notes) {
				t.Fatalf("notes about the file: %v, want %v", got, tc.notes)
			}
			for i, want := range tc.notes {
				if got[i].key != want.key || len(got[i].args) != len(want.args) || !reflect.DeepEqual(append([]any{}, got[i].args...), append([]any{}, want.args...)) {
					t.Errorf("note %d: %v, want %v", i, got[i], want)
				}
			}
			if hasNote(p, "setup.note.default_kept") != tc.kept || hasNote(p, "setup.note.default_replaced") || hasNote(p, "setup.note.small_replaced") {
				t.Errorf("notes %v", p.Notes)
			}
			if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
				t.Errorf("Configured after setup = %v, %v", ok, err)
			}
		})
	}

	// What refuses in any file refuses there: a provider.evren of the
	// user's own, a wrong shape, a file that does not parse.
	for name, src := range map[string]string{
		"the user's own provider": `{"provider": {"evren": {"options": {"baseURL": "https://llm.corp.example/v1"}}}}`,
		"a provider of no shape":  `{"provider": {"evren": "x"}}`,
		"a file that is broken":   `{"model": `,
		"a model of no kind":      `{"model": 5}`,
	} {
		for _, file := range []string{"opencode.json", "opencode.jsonc"} {
			env := testEnv(t, nil, nil)
			above := write(t, filepath.Join(env.Home, ".opencode", file), src)
			p, err := Opencode{}.Plan(env, opencodeRequest())
			var fe *ForeignEntryError
			var se *ShapeError
			var pe *ParseError
			switch {
			case len(p.Files) != 0:
				t.Errorf("%s: the plan writes %v", name, paths(p))
			case errors.As(err, &fe) && fe.Path == above, errors.As(err, &se) && se.Path == above, errors.As(err, &pe) && pe.Path == above:
			default:
				t.Errorf("%s in %s: %T %v", name, file, err, err)
			}
			if ok, err := (Opencode{}).Configured(env); ok || (err == nil) != (name == "a model of no kind") {
				t.Errorf("%s in %s: Configured = %v, %v", name, file, ok, err)
			}
		}
	}

	// Of the two files there the .jsonc one is read last.
	env := testEnv(t, nil, nil)
	write(t, filepath.Join(env.Home, ".opencode", "opencode.json"), `{"model": "anthropic/claude"}`)
	write(t, filepath.Join(env.Home, ".opencode", "opencode.jsonc"), `{"model": "evren/qwen3-vl-30b"}`)
	if p := setUp(t, Opencode{}, env, opencodeRequest()); hasNote(p, "setup.note.default_kept") || hasNote(p, "setup.note.opencode_set_above") {
		t.Errorf("EVREN's default in opencode.jsonc over another in opencode.json: notes %v", p.Notes)
	}

	// With OPENCODE_CONFIG_DIR set, ~/.opencode lies beneath the file Ülgen
	// writes: its default counts until Ülgen's file has one, and --default
	// is written. An entry of Ülgen's there is still a copy "ulgen run"
	// takes for a project file.
	custom := t.TempDir()
	env = testEnv(t, map[string]string{"OPENCODE_CONFIG_DIR": custom}, nil)
	beneath := write(t, filepath.Join(env.Home, ".opencode", "opencode.jsonc"), `{"model": "anthropic/claude", "provider": {"evren": `+opencodeStale+`}}`)
	if ok, err := (Opencode{}).Configured(env); ok || err != nil {
		t.Errorf("an entry only in ~/.opencode: Configured = %v, %v", ok, err)
	}
	p := setUp(t, Opencode{}, env, opencodeRequest())
	if args := noteArgs(t, p, "setup.note.opencode_home_copy"); len(args) != 1 || args[0] != Path(beneath) || !hasNote(p, "setup.note.default_kept") || hasNote(p, "setup.note.opencode_copy") {
		t.Errorf("notes %v", p.Notes)
	}
	req := opencodeRequest()
	req.MakeDefault = true
	p = setUp(t, Opencode{}, env, req)
	if doc := decoded(t, content(t, filepath.Join(custom, "opencode.json"))); doc["model"] != "evren/qwen3-vl-30b" || hasNote(p, "setup.note.opencode_set_above") {
		t.Errorf("--default beneath OPENCODE_CONFIG_DIR: model %v, notes %v", doc["model"], p.Notes)
	}
}

// EVREN is set up when opencode.json or opencode.jsonc of the directory
// Ülgen writes in holds its entry: the two files both lines of opencode
// read. An entry anywhere else is a copy, not a setup.
func TestOpencodeConfiguredCountsTheDirectoryUlgenWritesIn(t *testing.T) {
	entry := `{"provider": {"evren": ` + opencodeStale + `}}`
	custom := t.TempDir()
	for name, tc := range map[string]struct {
		file   string // under the home directory, with "/"; or "custom/" + name
		custom bool   // OPENCODE_CONFIG_DIR is set
		want   bool
	}{
		"opencode.json":                          {".config/opencode/opencode.json", false, true},
		"opencode.jsonc":                         {".config/opencode/opencode.jsonc", false, true},
		"config.json, which only 1.x reads":      {".config/opencode/config.json", false, false},
		"~/.opencode":                            {".opencode/opencode.json", false, false},
		"the global directory, the variable set": {".config/opencode/opencode.json", true, false},
		"the variable's directory":               {"custom/opencode.jsonc", true, true},
		"~/.opencode beneath the variable's":     {".opencode/opencode.jsonc", true, false},
	} {
		var vars map[string]string
		if tc.custom {
			vars = map[string]string{"OPENCODE_CONFIG_DIR": custom}
		}
		env := testEnv(t, vars, nil)
		path := filepath.Join(env.Home, filepath.FromSlash(tc.file))
		if rest, inCustom := strings.CutPrefix(tc.file, "custom/"); inCustom {
			path = filepath.Join(custom, rest)
		}
		write(t, path, entry)
		if ok, err := (Opencode{}).Configured(env); ok != tc.want || err != nil {
			t.Errorf("an entry only in %s: Configured = %v, %v", name, ok, err)
		}
		// Setup makes it so, and says what became of the other entry.
		p := setUp(t, Opencode{}, env, opencodeRequest())
		if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
			t.Errorf("%s, after setup: Configured = %v, %v", name, ok, err)
		}
		copies := hasNote(p, "setup.note.opencode_copy") || hasNote(p, "setup.note.opencode_home_copy")
		if copies == tc.want {
			t.Errorf("%s: notes %v", name, p.Notes)
		}
		os.RemoveAll(custom)
		if err := os.MkdirAll(custom, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// The two lists of providers are judged as opencode ends up with them: by
// the last file that has the key, and once.
func TestOpencodeProviderListsAreJudgedOnce(t *testing.T) {
	const shut, open = `{"enabled_providers": ["anthropic"], "disabled_providers": ["evren"]}`, `{"enabled_providers": ["anthropic", "evren"], "disabled_providers": []}`
	for name, tc := range map[string]struct {
		files map[string]string // under the home directory, with "/"
		from  string            // the file both notes name; "" for no note
	}{
		"shut beneath, open above":           {map[string]string{".config/opencode/config.json": shut, ".config/opencode/opencode.json": open}, ""},
		"open beneath, shut above":           {map[string]string{".config/opencode/config.json": open, ".config/opencode/opencode.json": shut}, ".config/opencode/opencode.json"},
		"shut in both":                       {map[string]string{".config/opencode/opencode.json": shut, ".config/opencode/opencode.jsonc": shut}, ".config/opencode/opencode.jsonc"},
		"shut in ~/.opencode, over the file": {map[string]string{".config/opencode/opencode.json": open, ".opencode/opencode.json": shut}, ".opencode/opencode.json"},
		"open in ~/.opencode, over the file": {map[string]string{".config/opencode/opencode.json": shut, ".opencode/opencode.json": open}, ""},
	} {
		env := testEnv(t, nil, nil)
		for file, text := range tc.files {
			write(t, filepath.Join(env.Home, filepath.FromSlash(file)), text)
		}
		p := setUp(t, Opencode{}, env, opencodeRequest())
		for _, key := range []string{"setup.note.opencode_disabled", "setup.note.opencode_not_enabled"} {
			count := 0
			for _, n := range p.Notes {
				if n.Key == key {
					count++
				}
			}
			switch {
			case tc.from == "" && count != 0, tc.from != "" && count != 1:
				t.Errorf("%s: %d notes %s: %v", name, count, key, p.Notes)
			case tc.from != "" && noteArgs(t, p, key)[0] != Path(filepath.Join(env.Home, filepath.FromSlash(tc.from))):
				t.Errorf("%s: %s names %v", name, key, noteArgs(t, p, key))
			}
		}
	}
}

// What the rule for "Ülgen's entry" overwrites or drops in an entry gets a
// note: a key of the user's own in options.apiKey, and models EVREN does not
// have. What the user added to a model that stays, stays.
func TestOpencodeSaysWhatItTakesOutOfTheEntry(t *testing.T) {
	const mine = `"qwen3-vl-30b": { "name": "mine", "options": { "temperature": 0.2 }, "limit": { "context": 1, "output": 2, "input": 3 }, "modalities": { "input": ["text"], "extra": true } }`
	for name, tc := range map[string]struct {
		entry    string
		replaced bool   // the note about options.apiKey
		dropped  string // the models the other note names; "" for no note
	}{
		"a key file of the user's":     {`{"env": ["EVREN_API_KEY"], "options": {"apiKey": "{file:~/.secrets/evren}"}}`, true, ""},
		"another variable's reference": {`{"env": ["EVREN_API_KEY"], "options": {"apiKey": "{env:OTHER_KEY}"}}`, true, ""},
		"an empty key":                 {`{"env": ["EVREN_API_KEY"], "options": {"apiKey": ""}}`, true, ""},
		"no key yet":                   {`{"env": ["EVREN_API_KEY"], "options": {"timeout": 600000}}`, false, ""},
		"the reference to EVREN's key": {`{"options": {"apiKey": "{env:EVREN_API_KEY}"}}`, false, ""},
		"models EVREN does not have":   {`{"env": ["EVREN_API_KEY"], "models": {"zeta": {}, "gone": {"name": "Gone"}, ` + mine + `}}`, false, "gone, zeta"},
		"a model of the user's, stays": {`{"env": ["EVREN_API_KEY"], "models": {` + mine + `}}`, false, ""},
		"models of no shape":           {`{"env": ["EVREN_API_KEY"], "models": ["qwen3-vl-30b"]}`, false, ""},
	} {
		env := testEnv(t, nil, nil)
		path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{"provider": {"evren": `+tc.entry+`}}`)
		p := setUp(t, Opencode{}, env, opencodeRequest())
		doc := decoded(t, content(t, path))
		if hasNote(p, "setup.note.opencode_key_replaced") != tc.replaced || hasNote(p, "setup.note.opencode_models_dropped") != (tc.dropped != "") {
			t.Errorf("%s: notes %v", name, p.Notes)
		}
		if tc.replaced {
			if args := noteArgs(t, p, "setup.note.opencode_key_replaced"); len(args) != 1 || args[0] != Path(path) {
				t.Errorf("%s: note arguments %v", name, args)
			}
		}
		if tc.dropped != "" {
			if args := noteArgs(t, p, "setup.note.opencode_models_dropped"); len(args) != 2 || args[0] != tc.dropped || args[1] != Path(path) {
				t.Errorf("%s: note arguments %v", name, args)
			}
		}
		for _, n := range p.Notes {
			for _, arg := range n.Args {
				if text, _ := arg.(string); strings.Contains(text, "secrets") || strings.Contains(text, "OTHER_KEY") {
					t.Errorf("%s: the note %s shows the old value: %v", name, n.Key, n.Args)
				}
			}
		}
		models, _ := dig(doc, "provider", "evren", "models").(map[string]any)
		if dig(doc, "provider", "evren", "options", "apiKey") != "{env:EVREN_API_KEY}" || len(models) != 2 {
			t.Errorf("%s: provider.evren = %v", name, dig(doc, "provider", "evren"))
		}
		if strings.Contains(tc.entry, mine) {
			want := map[string]any{
				"name": "qwen3-vl-30b", "tool_call": true, "options": map[string]any{"temperature": 0.2},
				"limit":      map[string]any{"context": float64(262144), "output": float64(8192), "input": float64(3)},
				"modalities": map[string]any{"input": []any{"text", "image"}, "output": []any{"text"}, "extra": true},
			}
			if !reflect.DeepEqual(models["qwen3-vl-30b"], want) {
				t.Errorf("%s: the model that stays is %v\nwant %v", name, models["qwen3-vl-30b"], want)
			}
		}
	}

	// Nothing to drop: the user's addition keeps its bytes and its comment.
	env := testEnv(t, nil, nil)
	path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), "{\n  \"provider\": {\n    \"evren\": {\n      \"env\": [\"EVREN_API_KEY\"],\n      \"models\": {\n        \"qwen3-vl-30b\": {\n          \"options\": { \"temperature\": 0.2 } // mine\n        }\n      }\n    }\n  }\n}\n")
	setUp(t, Opencode{}, env, opencodeRequest())
	if got := content(t, path); !strings.Contains(got, "          \"options\": { \"temperature\": 0.2 }, // mine\n") {
		t.Errorf("the user's line is not as it was:\n%s", got)
	}
}

func TestOpencodeRefusesWhatItCannotRead(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, filepath.Join(opencodeHome(env), "opencode.json"), `{"provider": [}`)
	_, err := Opencode{}.Plan(env, opencodeRequest())
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Path != path || pe.Detail != "line 1, column 15: invalid character '}' at start of value" {
		t.Errorf("err = %v", err)
	}
	if ok, err := (Opencode{}).Configured(env); ok || !errors.As(err, &pe) {
		t.Errorf("Configured = %v, %v", ok, err)
	}
	// A file beneath the one Ülgen writes that cannot be read stops the setup
	// too: what it says about the default is not known.
	write(t, path, "{}")
	lower := write(t, filepath.Join(opencodeHome(env), "config.json"), "[]")
	if _, err = (Opencode{}).Plan(env, opencodeRequest()); !errors.As(err, &pe) || pe.Path != lower {
		t.Errorf("err = %v", err)
	}
	unchanged(t, path, "{}")
}

// A directory where one of the files belongs is reported as that.
func TestOpencodeWrongKindOfFile(t *testing.T) {
	for _, name := range []string{"opencode.jsonc", "opencode.json", "config.json"} {
		env := testEnv(t, nil, nil)
		dir := filepath.Join(opencodeHome(env), name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p, err := Opencode{}.Plan(env, opencodeRequest())
		var ke *KindError
		if !errors.As(err, &ke) || ke.Path != dir || ke.WantDir || len(p.Files) != 0 {
			t.Errorf("%s: Plan = %v, %T %v", name, paths(p), err, err)
		}
		if ok, err := (Opencode{}).Configured(env); ok || !errors.As(err, &ke) {
			t.Errorf("%s: Configured = %v, %T %v", name, ok, err, err)
		}
	}
}

func TestOpencodeDetect(t *testing.T) {
	ctx := context.Background()
	d := Opencode{}.Detect(ctx, testEnv(t, nil, map[string]string{"opencode": "1.18.35\n"}))
	if !d.Installed || d.Version != "1.18.35" || d.Binary != testBin("opencode") {
		t.Errorf("on PATH: %+v", d)
	}
	env := testEnv(t, nil, nil)
	own := filepath.Join(env.Home, ".opencode", "bin", "opencode")
	env.LookPath = func(name string) (string, error) {
		if name == own {
			return own, nil
		}
		return "", errors.New("not found")
	}
	env.Version = func(context.Context, string) (string, error) { return "2.0.26", nil }
	if d = (Opencode{}).Detect(ctx, env); !d.Installed || d.Binary != own || d.Version != "2.0.26" {
		t.Errorf("installer directory: %+v", d)
	}
	if d = (Opencode{}).Detect(ctx, testEnv(t, nil, nil)); d.Installed {
		t.Errorf("nothing installed: %+v", d)
	}
	if (Opencode{}).ID() != "opencode" || (Opencode{}).Name() != "opencode" || (Opencode{}).InstallHint(Env{}).Key != "setup.install.opencode" {
		t.Error("ID, Name or InstallHint changed")
	}
}
