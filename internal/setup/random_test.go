package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The randomized test of the two adapters that edit JSON files. It writes a
// settings file as a user could have left it, in a random layout, plans a
// random request, applies the plan and plans again, and checks what must
// hold for every file:
//
//   - the second plan is empty;
//   - the byte-order mark, the line breaks and every comment are still
//     there;
//   - what the file means differs only under the keys the design names;
//   - the user's own entries are as they were, in their order.
//
// The seed is fixed, so the run is the same every time; a failure prints the
// number of its case and the file.
const (
	randomSeed  = 20261012
	randomCases = 600
)

// layout is how a random file is written.
type layout struct {
	eol, indent string
	trailing    bool // containers end with a comma
	bom         bool
	comments    bool
}

func pick[T any](r *rand.Rand, options ...T) T { return options[r.Intn(len(options))] }

// writePlan puts the files of case i's plan on the disk. Every eighth case
// goes through Apply, the real writer. The others are written plainly:
// Apply flushes every file to the disk, which would take nearly all of the
// test's time, and what is checked here is the plan.
func writePlan(t *testing.T, i int, p Plan, toolID string) error {
	t.Helper()
	if i%8 == 0 {
		return Apply(p, toolID, filepath.Join(t.TempDir(), "backups"))
	}
	for _, f := range p.Files {
		if current, err := readFile(f.Path); err != nil || string(current) != string(f.Old) || (current == nil) != (f.Old == nil) {
			return fmt.Errorf("%s is not what the plan was made from (%v)", f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(f.Path, f.New, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func randomLayout(r *rand.Rand) layout {
	return layout{
		eol: pick(r, "\n", "\n", "\r\n"), indent: pick(r, "  ", "\t", "    "),
		trailing: r.Intn(4) == 0, bom: r.Intn(6) == 0, comments: r.Intn(2) == 0,
	}
}

// build writes an object from its members, each a key and a JSON value on
// one line, in the layout.
func (l layout) build(r *rand.Rand, members [][2]string) string {
	var b strings.Builder
	if l.bom {
		b.WriteString("\xef\xbb\xbf")
	}
	b.WriteString("{" + l.eol)
	for i, m := range members {
		if l.comments && r.Intn(3) == 0 {
			b.WriteString(l.indent + fmt.Sprintf("// c%d before %s", i, m[0]) + l.eol)
		}
		b.WriteString(l.indent + `"` + m[0] + `": ` + m[1])
		if i < len(members)-1 || l.trailing {
			b.WriteString(",")
		}
		if l.comments && r.Intn(3) == 0 {
			b.WriteString(fmt.Sprintf(" /* c%d after */", i))
		}
		b.WriteString(l.eol)
	}
	if l.comments && r.Intn(4) == 0 {
		b.WriteString(l.indent + "// last" + l.eol)
	}
	b.WriteString("}")
	if r.Intn(5) != 0 {
		b.WriteString(l.eol)
	}
	return b.String()
}

// check compares a file after setup with the file before it: the mark, the
// line breaks and the comments must be as they were.
func (l layout) check(before, after string) error {
	comments := func(s string) int {
		return strings.Count(s, "// c") + strings.Count(s, "/* c") + strings.Count(s, "// last")
	}
	switch {
	case l.bom != strings.HasPrefix(after, "\xef\xbb\xbf"):
		return errors.New("the byte-order mark changed")
	case l.eol == "\r\n" && strings.Contains(strings.ReplaceAll(after, "\r\n", ""), "\n"):
		return errors.New("a bare line feed in a file with CRLF line breaks")
	case l.eol == "\n" && strings.Contains(after, "\r"):
		return errors.New("a carriage return in a file with LF line breaks")
	case comments(before) != comments(after):
		return fmt.Errorf("%d comments became %d", comments(before), comments(after))
	}
	return nil
}

// randomRequest picks some of the base request's models, in any order, a
// default among them and the options.
func randomRequest(r *rand.Rand, base Request) Request {
	req := base
	req.Models = nil
	for _, i := range r.Perm(len(base.Models))[:1+r.Intn(len(base.Models))] {
		req.Models = append(req.Models, base.Models[i])
	}
	req.Default = pick(r, req.Models...).ID
	req.ModelGiven = r.Intn(3) == 0
	req.MakeDefault = r.Intn(4) == 0
	return req
}

// leaves lists every leaf of a decoded document under its path.
func leaves(prefix string, v any, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			out[prefix] = "{}"
		}
		for k, c := range x {
			leaves(prefix+"."+k, c, out)
		}
	case []any:
		if len(x) == 0 {
			out[prefix] = "[]"
		}
		for i, c := range x {
			leaves(fmt.Sprintf("%s[%d]", prefix, i), c, out)
		}
	default:
		b, _ := json.Marshal(x)
		out[prefix] = string(b)
	}
}

// changedPaths returns the paths of the leaves that differ between two
// decoded documents, sorted. That an empty document is no longer empty is
// not a change of its own.
func changedPaths(before, after any) []string {
	a, b := map[string]string{}, map[string]string{}
	leaves("", before, a)
	leaves("", after, b)
	delete(a, "")
	var out []string
	for path, value := range a {
		if other, has := b[path]; !has || other != value {
			out = append(out, path)
		}
	}
	for path := range b {
		if _, has := a[path]; !has {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// under reports whether a leaf path lies at or under one of the roots.
func under(path string, roots ...string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+".") || strings.HasPrefix(path, root+"[") {
			return true
		}
	}
	return false
}

func TestRandomQwenFiles(t *testing.T) { randomQwenFiles(t, randomSeed, randomCases) }

// randomQwenFiles is the randomized test of the Qwen Code adapter for one
// seed.
func randomQwenFiles(t *testing.T, seed int64, cases int) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	evren := func(id, root string) string {
		return `{ "id": "` + id + `", "name": "EVREN ` + id + `", "envKey": "EVREN_API_KEY", "baseUrl": "` + root + `", "generationConfig": { "contextWindowSize": 1048576, "modalities": { "image": false }, "enableRequestMetadata": false } }`
	}
	entries := []string{
		`{ "id": "llama", "envKey": "LOCAL", "baseUrl": "http://127.0.0.1:1/v1" }`,
		`{ "id": "deepseek-v4-flash", "envKey": "MY_KEY", "baseUrl": "` + testRoot + `" }`, // the user's own, at EVREN's address
		evren("deepseek-v4-flash", testRoot), evren("gone", testRoot), `{ "id": "x" }`, `"text"`,
		evren("qwen3-vl-30b", "http://old.example/v1"), evren("deepseek-v4.1-flash", testRoot), evren("qwen3-vl-30b", testRoot),
		evren("corp-model", "https://proxy.example/v1"), // another service's, left on EVREN's variable
	}
	const corp = `{ "id": "qwen3-vl-30b", "envKey": "CORP_KEY", "baseUrl": "https://llm.corp.example/v1" }`
	changed, refused, outside := 0, 0, 0
	for i := range cases {
		var members [][2]string
		if r.Intn(3) != 0 {
			members = append(members, [2]string{"$version", pick(r, "4", "3", "2")})
		}
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"ui", `{ "theme": "dark" }`})
		}
		if r.Intn(3) != 0 {
			var list []string
			for _, entry := range entries {
				if r.Intn(2) == 0 {
					list = append(list, entry)
				}
			}
			r.Shuffle(len(list), func(a, b int) { list[a], list[b] = list[b], list[a] })
			// Sometimes another list next to it: one of another protocol, or a
			// custom provider's, which may speak the OpenAI protocol.
			others := pick(r, "", "", `, "anthropic": [{ "id": "claude" }]`, `, "corp": [`+corp+`]`)
			members = append(members, [2]string{"modelProviders", `{ "openai": [` + strings.Join(list, ", ") + `]` + others + ` }`})
		}
		if r.Intn(4) == 0 {
			members = append(members, [2]string{"providerProtocol", pick(r, `{ "corp": "openai" }`, `{ "corp": "openai" }`, `{ "corp": "anthropic" }`)})
		}
		// A default with a kind of sign-in, or, as "/model" leaves it, without.
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"security", `{ "auth": { "selectedType": "` + pick(r, "openai", "openai", "qwen-oauth", "anthropic") + `" } }`})
		}
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"model", pick(r,
				`{ "name": "llama" }`, `{ "name": "qwen3-vl-30b", "baseUrl": "`+testRoot+`" }`, `{ "name": "qwen3-vl-30b", "baseUrl": "" }`,
				`{ "name": "gone", "baseUrl": "`+testRoot+`/" }`, `{ "name": "deepseek-v4-flash" }`, `{ "name": "", "baseUrl": null }`,
				`{ "name": "qwen3-vl-30b", "baseUrl": "https://llm.corp.example/v1" }`, `{ "name": "corp-model", "baseUrl": "" }`, `{ "skipNextSpeakerCheck": true }`)})
		}
		if r.Intn(3) == 0 {
			members = append(members, [2]string{"general", pick(r, `{ "language": "de" }`, `{ "language": "tr" }`, `{ "language": "auto" }`, `{ "vimMode": true }`, `{}`)})
		}
		if r.Intn(3) == 0 {
			members = append(members, [2]string{"privacy", pick(r, `{ "usageStatisticsEnabled": true }`, `{ "usageStatisticsEnabled": false }`, `{}`)})
		}
		r.Shuffle(len(members), func(a, b int) { members[a], members[b] = members[b], members[a] })
		l := randomLayout(r)
		src := l.build(r, members)
		req := randomRequest(r, testRequest())

		// Sometimes a default in the environment.
		vars := map[string]string{}
		if r.Intn(6) == 0 {
			vars["OPENAI_BASE_URL"] = "https://api.other.example/v1"
		}
		if r.Intn(6) == 0 {
			vars["OPENAI_MODEL"] = pick(r, "their-model", "deepseek-v4-flash", "qwen3-vl-30b")
		}
		if r.Intn(12) == 0 {
			vars["QWEN_MODEL"] = pick(r, "their-model", "deepseek-v4.1-flash")
		}
		env := testEnv(t, vars, nil)
		path := write(t, qwenSettings(env), src)
		fail := func(format string, args ...any) {
			t.Helper()
			t.Fatalf("case %d: %s\nrequest: %+v\nenvironment: %v\nbefore:\n%s\nafter:\n%s", i, fmt.Sprintf(format, args...), req, vars, src, content(t, path))
		}
		before := decoded(t, src)
		p, err := Qwen{}.Plan(env, req)
		if qwenClash(before, vars, req) {
			var clash *DefaultClashError
			if !errors.As(err, &clash) || len(p.Files) != 0 {
				fail("a default that Ülgen's entry would take over was not refused: %T %v", err, err)
			}
			unchanged(t, path, src)
			refused++
			continue
		}
		if err != nil {
			fail("Plan: %v", err)
		}
		if err := writePlan(t, i, p, "qwen"); err != nil {
			fail("the plan could not be written: %v", err)
		}
		got := content(t, path)
		if got != src {
			changed++
		}
		if again, err := (Qwen{}).Plan(env, req); err != nil || len(again.Files) != 0 {
			fail("a second plan: %v, files %v", err, paths(again))
		}
		if err := l.check(src, got); err != nil {
			fail("%v", err)
		}
		after := decoded(t, got)
		for _, path := range changedPaths(before, after) {
			if !under(path, ".modelProviders.openai", ".modelProviders", ".security.auth.selectedType", ".security", ".model.name", ".model.baseUrl", ".model", ".general.language", ".general", ".privacy.usageStatisticsEnabled", ".privacy") ||
				under(path, ".modelProviders.anthropic", ".modelProviders.corp") {
				fail("%s changed", path)
			}
		}
		// A default that lives in the environment is not written over.
		if qwenDefaultOutside(before, vars) && !req.MakeDefault {
			outside++
			if qwenDefaultOf(after) != qwenDefaultOf(before) || !hasNote(p, "setup.note.qwen_env_default") {
				fail("the default of the environment was written over: %v, notes %v", qwenDefaultOf(after), p.Notes)
			}
		}
		// The user's entries are as they were, in their order; Ülgen's are the
		// request's models, each once.
		split := func(doc map[string]any) (theirs []any, ours []string) {
			for _, entry := range qwenList(doc) {
				if qwenOurs(entry) {
					ours = append(ours, qwenID(entry))
				} else {
					theirs = append(theirs, entry)
				}
			}
			return theirs, ours
		}
		theirsBefore, oursBefore := split(before)
		theirsAfter, oursAfter := split(after)
		if !reflect.DeepEqual(theirsBefore, theirsAfter) {
			fail("the user's entries changed")
		}
		var want []string
		for _, m := range req.Models {
			want = append(want, m.ID)
		}
		if !reflect.DeepEqual(slices.Sorted(slices.Values(oursAfter)), slices.Sorted(slices.Values(want))) {
			fail("Ülgen's entries are %v, want the models %v", oursAfter, want)
		}
		// An entry that stayed kept its place among the ones that stayed.
		var stayed, expected []string
		seen := map[string]bool{}
		for _, id := range oursBefore {
			if slices.Contains(want, id) && !seen[id] {
				seen[id] = true
				expected = append(expected, id)
			}
		}
		for _, id := range oursAfter {
			if seen[id] {
				stayed = append(stayed, id)
			}
		}
		if !reflect.DeepEqual(stayed, expected) {
			fail("the entries that stayed are in the order %v, were %v", stayed, expected)
		}
		for _, entry := range qwenList(after) {
			if m, offered := req.model(qwenID(entry)); qwenOurs(entry) && offered {
				wantEntry, _ := json.Marshal(qwenEntryFor(m, req.BaseURL))
				if !reflect.DeepEqual(entry, dig(decoded(t, `{"e": `+string(wantEntry)+`}`), "e")) {
					fail("the entry of %s is %v", m.ID, entry)
				}
			}
		}
		// An entry of another service that went was named in a note.
		named := slices.ContainsFunc(p.Notes, func(n Note) bool {
			return n.Key == "setup.note.qwen_entry_removed" && len(n.Args) == 2 && n.Args[0] == "corp-model" && n.Args[1] == "https://proxy.example/v1"
		})
		if slices.Contains(oursBefore, "corp-model") != named {
			fail("the entry of another service: notes %v", p.Notes)
		}
		if ok, err := (Qwen{}).Configured(env); !ok || err != nil {
			fail("Configured = %v, %v", ok, err)
		}
	}
	if changed < cases/2 || refused < cases/100 || outside < cases/100 {
		t.Errorf("%d of %d files changed, %d refused, %d with a default in the environment; the generator no longer exercises the adapter", changed, cases, refused, outside)
	}
}

// qwenDefaultOutside says, by the decoded file and the environment alone,
// whether Qwen Code's default lives in the environment: the settings name
// no model and no other kind of sign-in than "openai", and one of the
// variables is set.
func qwenDefaultOutside(doc map[string]any, vars map[string]string) bool {
	authType, _ := dig(doc, "security", "auth", "selectedType").(string)
	name, _ := dig(doc, "model", "name").(string)
	return name == "" && (authType == "" || authType == "openai") && vars["OPENAI_BASE_URL"]+vars["OPENAI_MODEL"]+vars["QWEN_MODEL"] != ""
}

// qwenClash says, by the decoded file and the environment alone, whether
// setup has to refuse: the default is the user's, in the file or in the
// environment, names a model the request offers, and no list that speaks
// the OpenAI protocol has an entry with that id, so Ülgen's new entry would
// become the default.
func qwenClash(doc map[string]any, vars map[string]string, req Request) bool {
	authType, _ := dig(doc, "security", "auth", "selectedType").(string)
	name, _ := dig(doc, "model", "name").(string)
	base, _ := dig(doc, "model", "baseUrl").(string)
	if authType != "" && authType != "openai" {
		return false
	}
	if name == "" {
		if name, base = vars["OPENAI_MODEL"], ""; name == "" {
			name = vars["QWEN_MODEL"]
		}
	} else if strings.TrimRight(base, "/") == testRoot {
		return false // meant for EVREN
	}
	lists := []any{dig(doc, "modelProviders", "openai")}
	if dig(doc, "providerProtocol", "corp") == "openai" {
		lists = append(lists, dig(doc, "modelProviders", "corp"))
	}
	for _, list := range lists {
		entries, _ := list.([]any)
		for _, entry := range entries {
			if id, _ := dig(entry, "id").(string); id == name {
				return false
			}
		}
	}
	_, offered := req.model(name)
	return offered && !req.MakeDefault
}

func TestRandomOpencodeFiles(t *testing.T) { randomOpencodeFiles(t, randomSeed+1, randomCases) }

// randomOpencodeFiles is the randomized test of the opencode adapter for
// one seed. Next to the file Ülgen writes there is sometimes one beneath it
// and one in ~/.opencode, which lies over Ülgen's file, or beneath it when
// OPENCODE_CONFIG_DIR names the directory.
func randomOpencodeFiles(t *testing.T, seed int64, cases int) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	providers := []string{
		`"local": { "npm": "@ai-sdk/openai-compatible", "options": { "baseURL": "http://127.0.0.1:1/v1" } }`,
		`"evren": { "name": "older", "env": ["EVREN_API_KEY"], "models": { "gone": {}, "qwen3-vl-30b": { "options": { "temperature": 0.2 } } } }`,
		`"evren": { "options": { "apiKey": "{env:EVREN_API_KEY}", "timeout": 600000, "headers": { "X-Team": "platform" } }, "whitelist": ["a"] }`,
		`"evren": { "env": ["EVREN_API_KEY"], "options": { "apiKey": "{file:~/.secrets/evren}" }, "models": { "deepseek-v4-flash": { "cost": { "input": 0 } } } }`,
		`"evren": { "name": "Evren Corp", "options": { "baseURL": "https://llm.evren-corp.example/v1" } }`, // the user's own
		`"anthropic": {}`,
	}
	models := []string{`"anthropic/claude"`, `"evren/qwen3-vl-30b"`, `"evren/gone"`, `{ "providerID": "evren", "model": "deepseek-v4-flash" }`, `""`, `null`}
	object := func() (string, layout) {
		var members [][2]string
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"$schema", `"https://opencode.ai/config.json"`})
		}
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"theme", `"tokyonight"`})
		}
		if r.Intn(3) != 0 {
			var list []string
			evren := false
			for _, provider := range providers {
				isEvren := strings.HasPrefix(provider, `"evren"`)
				if r.Intn(4) == 0 && !(isEvren && evren) {
					list, evren = append(list, provider), evren || isEvren
				}
			}
			r.Shuffle(len(list), func(a, b int) { list[a], list[b] = list[b], list[a] })
			members = append(members, [2]string{"provider", `{ ` + strings.Join(list, ", ") + ` }`})
		}
		if r.Intn(2) == 0 {
			members = append(members, [2]string{"model", pick(r, models...)})
		}
		if r.Intn(3) == 0 {
			members = append(members, [2]string{"small_model", pick(r, `"anthropic/haiku"`, `"evren/deepseek-v4-flash"`, `"evren/gone"`)})
		}
		r.Shuffle(len(members), func(a, b int) { members[a], members[b] = members[b], members[a] })
		l := randomLayout(r)
		return l.build(r, members), l
	}
	foreign := func(text string) bool {
		entry, has := dig(decoded(t, text), "provider", "evren").(map[string]any)
		return has && dig(entry, "options", "apiKey") != "{env:EVREN_API_KEY}" && dig(entry, "env") == nil
	}
	planned, refused, above := 0, 0, 0
	for i := range cases {
		// Where Ülgen writes: the global directory, or the one the variable
		// names, with the global one beneath it.
		var vars map[string]string
		custom := ""
		if r.Intn(4) == 0 {
			custom = t.TempDir()
			vars = map[string]string{"OPENCODE_CONFIG_DIR": custom}
		}
		env := testEnv(t, vars, nil)
		dir := opencodeHome(env)
		if custom != "" {
			dir = custom
		}
		name := pick(r, "opencode.json", "opencode.jsonc")
		src, l := object()
		path := write(t, filepath.Join(dir, name), src)
		// The other files: path and content of each.
		others := map[string]string{}
		if r.Intn(3) == 0 { // beneath the file, in a place opencode reads before it
			lower := filepath.Join(opencodeHome(env), pick(r, "config.json", "opencode.json"))
			if custom == "" && name == "opencode.json" {
				lower = filepath.Join(dir, "config.json")
			}
			others[lower], _ = object()
		}
		home := filepath.Join(env.Home, ".opencode", pick(r, "opencode.json", "opencode.jsonc"))
		if r.Intn(3) == 0 {
			others[home], _ = object()
		}
		for file, text := range others {
			write(t, file, text)
		}
		req := randomRequest(r, opencodeRequest())
		fail := func(format string, args ...any) {
			t.Helper()
			t.Fatalf("case %d: %s\nrequest: %+v\n%s before:\n%s\nafter:\n%s\nthe other files: %q", i, fmt.Sprintf(format, args...), req, path, src, content(t, path), others)
		}
		refuse := foreign(src)
		for _, text := range others {
			refuse = refuse || foreign(text)
		}
		p, err := Opencode{}.Plan(env, req)
		if refuse {
			var fe *ForeignEntryError
			if !errors.As(err, &fe) || len(p.Files) != 0 {
				fail("a provider of the user's own was not refused: %T %v", err, err)
			}
			if ok, err := (Opencode{}).Configured(env); ok || !errors.As(err, &fe) {
				fail("Configured = %v, %v", ok, err)
			}
			unchanged(t, path, src)
			refused++
			continue
		}
		if err != nil {
			fail("Plan: %v", err)
		}
		planned++
		if err := writePlan(t, i, p, "opencode"); err != nil {
			fail("the plan could not be written: %v", err)
		}
		got := content(t, path)
		if again, err := (Opencode{}).Plan(env, req); err != nil || len(again.Files) != 0 {
			fail("a second plan: %v, files %v", err, paths(again))
		}
		if err := l.check(src, got); err != nil {
			fail("%v", err)
		}
		before, after := decoded(t, src), decoded(t, got)
		ours := []string{".provider.evren.npm", ".provider.evren.name", ".provider.evren.options.baseURL", ".provider.evren.options.apiKey", ".provider.evren.models", ".model", ".small_model"}
		if dig(before, "provider", "evren") == nil {
			ours = append(ours, ".provider.evren", ".provider")
		}
		for _, changed := range changedPaths(before, after) {
			if !under(changed, ours...) {
				fail("%s changed", changed)
			}
		}
		// A key that a file over Ülgen's sets is not written.
		if text, has := others[home]; has && custom == "" {
			above++
			for _, key := range []string{"model", "small_model"} {
				if _, set := decoded(t, text)[key]; set && !reflect.DeepEqual(before[key], after[key]) {
					fail("%s is set in %s and was written all the same", key, home)
				}
			}
		}
		if msg := opencodeEntryProblem(t, before, after, req); msg != "" {
			fail("%s", msg)
		}
		// Nothing else was written or created.
		for file, text := range others {
			unchanged(t, file, text)
		}
		files := 0
		for _, d := range []string{opencodeHome(env), custom, filepath.Join(env.Home, ".opencode")} {
			entries, _ := os.ReadDir(d)
			files += len(entries)
		}
		if files != 1+len(others) {
			fail("%d files in opencode's directories, want %d", files, 1+len(others))
		}
		if ok, err := (Opencode{}).Configured(env); !ok || err != nil {
			fail("Configured = %v, %v", ok, err)
		}
	}
	if planned < cases/2 || refused < cases/20 || above < cases/20 {
		t.Errorf("%d files set up, %d refused, %d with a file over Ülgen's; the generator no longer exercises all three", planned, refused, above)
	}
}

// opencodeEntryProblem says what is wrong with provider.evren after setup,
// or "": Ülgen's keys have the request's values, the models are the
// request's, and what the user added to a model that stays is still there.
func opencodeEntryProblem(t *testing.T, before, after map[string]any, req Request) string {
	t.Helper()
	entry, _ := dig(after, "provider", "evren").(map[string]any)
	if entry["npm"] != "@ai-sdk/openai-compatible" || entry["name"] != "EVREN" || dig(entry, "options", "baseURL") != req.BaseURL || dig(entry, "options", "apiKey") != "{env:EVREN_API_KEY}" {
		return fmt.Sprintf("provider.evren = %v", entry)
	}
	models, _ := entry["models"].(map[string]any)
	if len(models) != len(req.Models) {
		return fmt.Sprintf("provider.evren.models has %d models, the request %d", len(models), len(req.Models))
	}
	text, _ := json.Marshal(opencodeModels(req))
	for id, want := range decoded(t, string(text)) {
		model, _ := models[id].(map[string]any)
		for key, value := range want.(map[string]any) {
			if !reflect.DeepEqual(model[key], value) {
				return fmt.Sprintf("models.%s.%s = %v, want %v", id, key, model[key], value)
			}
		}
		had, _ := dig(before, "provider", "evren", "models", id).(map[string]any)
		for key, value := range had {
			if _, own := want.(map[string]any)[key]; !own && !reflect.DeepEqual(model[key], value) {
				return fmt.Sprintf("what the user added to models.%s.%s is %v, was %v", id, key, model[key], value)
			}
		}
	}
	return ""
}
