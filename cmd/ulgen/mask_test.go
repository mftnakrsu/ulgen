package main

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

// maskCases is the table of TestMaskSecretsCases: an input and what must hold
// of maskSecrets(input). An entry with want gives the exact output; an entry
// with absent gives text that must not be there; an entry with kept gives
// text that must still be there.
type maskCase struct {
	in     string
	want   string // the exact output, when not empty
	absent []string
	kept   []string
}

var maskCases = []maskCase{
	// Two secrets on one line: both values go.
	{in: `{"apiKey":"AAA","password":"BBB"}`, want: `{"apiKey":[hidden],"password":[hidden]}`},
	{in: `{"token": abc, "password": "BBB"}`, want: `{"token": [hidden], "password": [hidden]}`},
	{in: `provider = { name = "n", apiKey = "AAA", token = "BBB" }`, want: `provider = { name = "n", apiKey = [hidden], token = [hidden] }`},
	// A container opened by a secret name hides everything inside it, at any
	// depth, and "a" (the container's own key) stays.
	{in: `"apiKey": {"a":[1,2,"VVV"]},"password":"WWW"`, absent: []string{"VVV", "WWW"}, kept: []string{`"a"`}},
	// A value that ends in an escaped backslash must not swallow what follows.
	{in: `"apiKey": "ab\\", "password": "HHH"`, absent: []string{"HHH"}},
	// An array under a secret name, over several lines, is hidden to its
	// closing bracket; the line count stays.
	{in: "\"apiKeys\": [\n  \"CCC\",\n  \"DDD\"\n]", absent: []string{"CCC", "DDD"}},
	{in: "api_keys = [\n \"CCC\",\n]", absent: []string{"CCC"}},
	{in: "\"token\": {\n  \"value\": \"EEE\"\n}", absent: []string{"EEE"}, kept: []string{`"value"`}},
	// A value on the line after its name is hidden; two lines still.
	{in: "\"apiKey\":\n  \"AAA\"", absent: []string{"AAA"}},
	// More secret names.
	{in: `"credentials": "HHH"`, absent: []string{"HHH"}},
	{in: `"passphrase": "III"`, absent: []string{"III"}},
	{in: `"secrets": "JJJ"`, absent: []string{"JJJ"}},
	{in: `"bearer": "EEE"`, absent: []string{"EEE"}},
	{in: `"oauth_tokens_secret": "KKK"`, absent: []string{"KKK"}},
	{in: `"private_key": "LLL"`, absent: []string{"LLL"}},
	{in: `"OPENAI_API_KEY": "MMM"`, absent: []string{"MMM"}},
	// Names that are not secrets: a count of tokens, a variable's name, a
	// reference to a variable, an empty value, a false, "auth" alone (Qwen
	// Code keeps its selected type there) and interface text.
	{in: `"maxOutputTokens": 8192`},
	{in: `"max_tokens": 100`},
	{in: `"apiKeyEnv": "X_KEY"`},
	{in: `"envKey": "EVREN_API_KEY"`},
	{in: `env_key = "EVREN_API_KEY"`},
	{in: `"apiKey": "{env:EVREN_API_KEY}"`},
	{in: `"apiKey": "$EVREN_API_KEY"`},
	{in: `"apiKey": "${EVREN_API_KEY}"`},
	{in: `"apiKey": ""`},
	{in: `"store_secret": false`},
	{in: `"security": {"auth": {"selectedType": "openai"}}`},
	{in: `"Session token": "Oturum belirteci"`},
	{in: `"model": "deepseek-v4.1-flash"`},
	// A name ending in "tokens": strings go, a bare number stays; a value
	// that merely begins with "$" is not a reference and is hidden.
	{in: `"access_tokens": ["TTT"]`, absent: []string{"TTT"}},
	{in: `"apiKey": "$ecret1"`, absent: []string{"$ecret1"}},
	// URL scrubbing: the password in userinfo, a query parameter named like a
	// secret, a bare user name that stays, and "sig" with the fragment kept.
	{in: `"url": "https://u:FFF@host/x?token=GGG&a=1"`, want: `"url": "https://[hidden]@host/x?token=[hidden]&a=1"`},
	{in: `"baseURL": "https://user@host/v1"`, want: `"baseURL": "https://user@host/v1"`},
	{in: `"u": "https://h/p?sig=SSS#f"`, absent: []string{"SSS"}, kept: []string{"#f"}},
	// A comment: only a secret name with a value in the comment is masked.
	{in: `api_key = abc123  # the key`, want: `api_key = [hidden]  # the key`},
	{in: "# api_key = abc123\nmodel = \"m\"\n", want: "# api_key = [hidden]\nmodel = \"m\"\n"},
	{in: "// token: see the docs\n\"model\": \"m\"\n", want: "// token: [hidden]\n\"model\": \"m\"\n"},
	// A /* ... */ comment, over several lines, is masked the same way and its
	// closing marker stays.
	{in: "/* api_key = abc123\nmore */\nmodel = \"m\"\n", want: "/* api_key = [hidden]\nmore */\nmodel = \"m\"\n"},
	// Two adjacent literal strings are two tokens; only the first is the
	// value of the name.
	{in: `password = 'it''s'`, want: `password = [hidden]'s'`},
	// A multi-line string holds its line breaks when it is hidden.
	{in: "api_key = \"\"\"\nline1\nline2\n\"\"\"\nnext = 1\n", absent: []string{"line1", "line2"}, kept: []string{"next = 1"}},
	// Two values on two lines under a secret name.
	{in: "\"apiKey\": [\"x\"\n\"III\"]", absent: []string{"x", "III"}},
	// A name with no value: nothing changes.
	{in: "token =\nnext = 1\n", want: "token =\nnext = 1\n"},
	// A line that was commented out can hold a value with spaces: the rest of
	// the comment's line goes, the next line stays.
	{in: "# password = \"my pass\" # old\nmodel = \"m\"\n", want: "# password = [hidden]\nmodel = \"m\"\n"},
	{in: "/* api_key: AAA BBB\n   model: m */\n\"x\": 1\n", want: "/* api_key: [hidden]\n   model: m */\n\"x\": 1\n"},
	// An escaped quote does not end a multi-line string, so what follows the
	// string is still read as settings.
	{in: "note = \"\"\"a \\\"\"\" b\"\"\"\napi_key = \"AAA\"\n", want: "note = \"\"\"a \\\"\"\" b\"\"\"\napi_key = [hidden]\n"},
	// Every URL of a string, and only a real user:password part.
	{in: `"urls": "https://u:P1@a.example/x https://v:P2@b.example/y"`, want: `"urls": "https://[hidden]@a.example/x https://[hidden]@b.example/y"`},
	{in: `"url": "https://host.example:443/users/@me"`, want: `"url": "https://host.example:443/users/@me"`},
	{in: `"url": "http://localhost:8080/v1"`, want: `"url": "http://localhost:8080/v1"`},
	// A secret under a table of secrets, three levels down, on one line.
	{in: `{"credentials":{"a":{"b":["DEEP1",{"c":"DEEP2"}]}},"model":"m"}`, want: `{"credentials":{"a":{"b":[[hidden],{"c":[hidden]}]}},"model":"m"}`},
	// What ulgen itself writes into a tool's file is shown as it is.
	{in: "# Ülgen sets: model, model_provider, web_search, model_context_window,\n", want: "# Ülgen sets: model, model_provider, web_search, model_context_window,\n"},
	{in: `{"id": "glm-5.3", "name": "EVREN glm-5.3", "envKey": "EVREN_API_KEY", "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1"}`, want: `{"id": "glm-5.3", "name": "EVREN glm-5.3", "envKey": "EVREN_API_KEY", "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1"}`},
}

func TestMaskSecretsCases(t *testing.T) {
	for _, tc := range maskCases {
		got := maskSecrets(tc.in)
		if tc.want != "" {
			if got != tc.want {
				t.Errorf("maskSecrets(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
			continue
		}
		for _, s := range tc.absent {
			if strings.Contains(got, s) {
				t.Errorf("maskSecrets(%q) still holds %q:\n%s", tc.in, s, got)
			}
		}
		for _, s := range tc.kept {
			if !strings.Contains(got, s) {
				t.Errorf("maskSecrets(%q) lost %q:\n%s", tc.in, s, got)
			}
		}
	}
}

// A masked value never takes a line break with it: the text after masking has
// the lines it had before. This holds for every case, and for inputs built at
// random from fragments of real files.
func TestMaskSecretsNeverChangesTheLineCount(t *testing.T) {
	check := func(in string) {
		t.Helper()
		out := maskSecrets(in)
		if strings.Count(out, "\n") != strings.Count(in, "\n") {
			t.Errorf("maskSecrets(%q) changed the number of lines:\n%q", in, out)
		}
	}
	for _, tc := range maskCases {
		check(tc.in)
	}
	rnd := newRand(0x5ec4e7)
	for i := 0; i < 2000; i++ {
		check(randomFragment(rnd))
	}
}

// marker starts every value the generator puts under a secret name.
const marker = "S3CR3T"

// genSecretNames are names that mark their value as a secret, used by the
// generator of the marked-secret test.
var genSecretNames = []string{
	"apiKey", "password", "token", "api_keys", "private_key", "client_secret",
	"Authorization", "credentials", "passphrase", "bearer", "gh_pat", "PASSWD",
	"secret", "x-api-key",
}

// genPlainNames are names that do not mark their value as a secret.
var genPlainNames = []string{
	"model", "name", "baseURL", "theme", "timeout", "retries", "security",
	"auth", "selectedType", "envKey", "apiKeyEnv", "note", "Session token",
	"comment", "version",
}

// docBuild writes a JSON or TOML document and remembers every value that is
// not under a secret name, which must survive the masking.
type docBuild struct {
	rnd    *rand.Rand
	b      strings.Builder
	plain  []string
	minify bool
}

func (d *docBuild) pickName() (string, bool) {
	if d.rnd.Intn(2) == 0 {
		return genSecretNames[d.rnd.Intn(len(genSecretNames))], true
	}
	return genPlainNames[d.rnd.Intn(len(genPlainNames))], false
}

func (d *docBuild) ref() string {
	s := "plain" + strconv.Itoa(d.rnd.Intn(1000000)) + randomLetters(d.rnd, 4)
	d.plain = append(d.plain, s)
	return s
}

// str writes one string value: a marker string under a secret name, and a
// remembered plain string otherwise.
func (d *docBuild) str(secret bool) {
	if secret {
		d.b.WriteString(strconv.Quote(marker + strconv.Itoa(d.rnd.Intn(1000000)) + randomLetters(d.rnd, 3)))
		return
	}
	d.b.WriteString(strconv.Quote(d.ref()))
}

func (d *docBuild) scalar() {
	switch d.rnd.Intn(3) {
	case 0:
		d.b.WriteString(strconv.Itoa(d.rnd.Intn(100000)))
	case 1:
		d.b.WriteString([]string{"true", "false"}[d.rnd.Intn(2)])
	default:
		d.b.WriteString("null")
	}
}

func (d *docBuild) indentLines(depth int) {
	for i := 0; i < depth; i++ {
		d.b.WriteString("  ")
	}
}

func (d *docBuild) jsonObject(depth int, secret bool) {
	d.b.WriteByte('{')
	for i, n := 0, d.rnd.Intn(4)+1; i < n; i++ {
		if i > 0 {
			d.b.WriteByte(',')
		}
		if !d.minify {
			d.b.WriteByte('\n')
			d.indentLines(depth + 1)
		}
		name, sec := d.pickName()
		d.b.WriteString(strconv.Quote(name))
		if d.minify {
			d.b.WriteByte(':')
		} else {
			d.b.WriteString(": ")
		}
		d.jsonValue(depth+1, secret || sec)
	}
	if !d.minify {
		d.b.WriteByte('\n')
		d.indentLines(depth)
	}
	d.b.WriteByte('}')
}

func (d *docBuild) jsonArray(depth int, secret bool) {
	d.b.WriteByte('[')
	for i, n := 0, d.rnd.Intn(4); i < n; i++ {
		if i > 0 {
			d.b.WriteByte(',')
		}
		if !d.minify {
			d.b.WriteByte('\n')
			d.indentLines(depth + 1)
		}
		d.jsonValue(depth+1, secret)
	}
	if !d.minify && d.rnd.Intn(4) != 0 {
		d.b.WriteByte('\n')
		d.indentLines(depth)
	}
	d.b.WriteByte(']')
}

func (d *docBuild) jsonValue(depth int, secret bool) {
	switch k := d.rnd.Intn(10); {
	case k < 3 && depth < 4:
		d.jsonObject(depth, secret)
	case k < 6 && depth < 4:
		d.jsonArray(depth, secret)
	case k < 8:
		d.str(secret)
	default:
		d.scalar()
	}
}

// randomJSON builds one well-formed JSON document, minified or indented, with
// nested objects and arrays to depth 4. It returns the document and every
// value that is not under a secret name.
func randomJSON(rnd *rand.Rand) (doc string, plain []string) {
	d := &docBuild{rnd: rnd, minify: rnd.Intn(2) == 0}
	if d.rnd.Intn(2) == 0 {
		d.jsonObject(0, false)
	} else {
		d.jsonArray(0, false)
	}
	return d.b.String(), d.plain
}

// tomlName writes a TOML key: bare when it is a simple name, quoted otherwise.
func tomlName(name string) string {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return strconv.Quote(name)
		}
	}
	if name == "" {
		return strconv.Quote(name)
	}
	return name
}

func (d *docBuild) tomlValue(depth int, secret bool) {
	switch k := d.rnd.Intn(10); {
	case k < 4:
		d.str(secret)
	case k < 7 && depth < 4:
		d.b.WriteByte('[')
		for i, n := 0, d.rnd.Intn(3); i < n; i++ {
			if i > 0 {
				d.b.WriteString(", ")
			}
			d.tomlValue(depth+1, secret)
		}
		d.b.WriteByte(']')
	case k < 9 && depth < 4:
		d.b.WriteString("{ ")
		for i, n := 0, d.rnd.Intn(3)+1; i < n; i++ {
			if i > 0 {
				d.b.WriteString(", ")
			}
			name, sec := d.pickName()
			d.b.WriteString(tomlName(name))
			d.b.WriteString(" = ")
			d.tomlValue(depth+1, secret || sec)
		}
		d.b.WriteString(" }")
	default:
		d.scalar()
	}
}

// randomTOML builds one well-formed TOML document, with nested arrays and
// inline tables to depth 4 and several members on a line. It returns the
// document and every value that is not under a secret name.
func randomTOML(rnd *rand.Rand) (doc string, plain []string) {
	d := &docBuild{rnd: rnd}
	for g, tables := 0, rnd.Intn(2); g <= tables; g++ {
		if g > 0 {
			d.b.WriteString("[group" + strconv.Itoa(g) + "]\n")
		}
		for i, n := 0, rnd.Intn(3)+1; i < n; i++ {
			name, sec := d.pickName()
			d.b.WriteString(tomlName(name))
			d.b.WriteString(" = ")
			d.tomlValue(0, sec)
			d.b.WriteByte('\n')
		}
	}
	return d.b.String(), d.plain
}

func randomLetters(rnd *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rnd.Intn(len(letters))]
	}
	return string(b)
}

// The generator builds well-formed JSON and TOML documents in which every
// value at any depth under a secret name is a marker string and no other value
// is. The masked output never holds the marker, and every value that was not
// under a secret name is still there.
func TestMaskSecretsHidesEveryMarkedSecret(t *testing.T) {
	check := func(doc string, plain []string) {
		t.Helper()
		out := maskSecrets(doc)
		if strings.Contains(out, marker) {
			t.Errorf("a secret survived:\n in  %q\n out %q", doc, out)
		}
		for _, s := range plain {
			if !strings.Contains(out, s) {
				t.Errorf("the plain value %q was lost:\n in  %q\n out %q", s, doc, out)
			}
		}
	}
	rnd := newRand(0xa11ce)
	for i := 0; i < 2000; i++ {
		doc, plain := randomJSON(rnd)
		check(doc, plain)
	}
	for i := 0; i < 500; i++ {
		doc, plain := randomTOML(rnd)
		check(doc, plain)
	}
}

// A 1 MB text of one repeated shape must be masked in well under a second: the
// look-ahead for a separator and every token are walked once.
func TestMaskSecretsIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the speed check in short mode")
	}
	const size = 1 << 20
	inputs := map[string]string{
		"spaces":   strings.Repeat(" ", size),
		"brackets": strings.Repeat("[", size),
		"strings":  strings.Repeat(`"a "`, size/4),
		"objects":  strings.Repeat(`{"a":`, size/5),
		"words":    strings.Repeat("x", size),
		// Unclosed strings and long query strings must not be rescanned.
		"escapes": strings.Repeat(`"\`, size/2),
		"quotes":  strings.Repeat(`'a `, size/3),
		"query":   "?" + strings.Repeat("x", size),
		"params":  "u?" + strings.Repeat("a=b&", size/4),
		"hashes":  strings.Repeat("# a: b\n", size/7),
	}
	for name, in := range inputs {
		start := time.Now()
		out := maskSecrets(in)
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s: %d bytes took %v", name, len(in), took)
		}
		if len(out) == 0 && len(in) > 0 {
			t.Errorf("%s: empty output", name)
		}
	}
}

// randomFragment builds one piece of a file out of the names, separators,
// values, brackets, commas, comments and line breaks that appear in the files
// a dry run shows.
func randomFragment(rnd *rand.Rand) string {
	names := []string{
		`"apiKey"`, `"password"`, `"token"`, `"tokens"`, `"maxOutputTokens"`,
		`"envKey"`, `"apiKeyEnv"`, `"security"`, `"auth"`, `"selectedType"`,
		`"Session token"`, `"model"`, `"url"`, `"baseURL"`, `api_key`, `env_key`,
		`token`, `password`, `key`, `name`, `a`, `b`, `"a"`, `"note"`,
	}
	values := []string{
		`"AAA"`, `"BBB"`, `"S3CR3T-1"`, `""`, `"$EVREN_API_KEY"`, `"{env:EVREN_API_KEY}"`,
		`"${EVREN_API_KEY}"`, `true`, `false`, `null`, `8192`, `abc123`, `$ecret1`,
		`'lit'`, `"""\nline\n"""`, `"https://u:pw@host/x?token=GGG&a=1"`, `"# not a comment"`,
	}
	seps := []string{": ", " = ", ":", "=", " : ", " =", ":\n  "}
	open := []string{"[", "{", "("}
	close := []string{"]", "}", ")"}
	misc := []string{",", "# a comment with token = XYZ\n", "// see the docs\n", "\n", " ", "\t"}
	var b strings.Builder
	for n := rnd.Intn(12); n >= 0; n-- {
		switch rnd.Intn(10) {
		case 0:
			b.WriteString(names[rnd.Intn(len(names))])
		case 1:
			b.WriteString(values[rnd.Intn(len(values))])
		case 2:
			b.WriteString(seps[rnd.Intn(len(seps))])
		case 3:
			b.WriteString(open[rnd.Intn(len(open))])
		case 4:
			b.WriteString(close[rnd.Intn(len(close))])
		default:
			b.WriteString(misc[rnd.Intn(len(misc))])
		}
	}
	return b.String()
}

// TestMaskSecrets is the older table, moved here. Some expectations changed
// under the new rules: a hidden multi-line string keeps its line breaks, and a
// hidden bare word or comment value is one run without white space, so what
// follows it ("and more", "abc def") is no longer hidden.
func TestMaskSecrets(t *testing.T) {
	for _, tc := range [][2]string{
		// The value of a name that looks like a secret is hidden.
		{`"apiKey": "sk-123"`, `"apiKey": [hidden]`},
		{`  "api_key": "sk-123",`, `  "api_key": [hidden],`},
		{`api-key = 'abc'`, `api-key = [hidden]`},
		{`OPENAI_API_KEY=sk-123`, `OPENAI_API_KEY=[hidden]`},
		{`export GITHUB_TOKEN=ghp_abc`, `export GITHUB_TOKEN=[hidden]`},
		{`"Authorization": "Bearer abc def"`, `"Authorization": [hidden]`},
		{`// Authorization: Bearer abc def`, `// Authorization: [hidden]`},
		{`password: hunter2 and more`, `password: [hidden] and more`},
		{`PASSWORD = "x" # note`, `PASSWORD = [hidden] # note`},
		{`client_secret = "a \" b"`, `client_secret = [hidden]`},
		// A name that holds "tokens" (in any case) is a count, not a secret.
		{`"max_tokens": 4096,`, ""},
		{`"maxOutputTokens": 4096,`, ""},
		{`{"a": 1, "token": 12}`, `{"a": 1, "token": [hidden]}`},
		{`"openrouter": {"options": {"apiKey": "sk-or-1"}},`, `"openrouter": {"options": {"apiKey": [hidden]}},`},
		{`x = { api_key = "k", name = "n" }`, `x = { api_key = [hidden], name = "n" }`},
		{"a: 1\n  \"token\": \"t\"\nb: 2\n", "a: 1\n  \"token\": [hidden]\nb: 2\n"},
		// A reference to a variable, a name of a variable and an empty value
		// are not secrets.
		{`"apiKey": "{env:EVREN_API_KEY}"`, ""},
		{`"apiKey": "$OPENAI_API_KEY"`, ""},
		{`"apiKey": "${OPENAI_API_KEY}"`, ""},
		{`api_key = $OPENAI_API_KEY`, ""},
		{`"apiKey": ""`, ""},
		{`"envKey": "EVREN_API_KEY",`, ""},
		{`env_key = "EVREN_API_KEY"`, ""},
		{`env_key_instructions = "Start Codex with \"ulgen run codex\", or put the key in your shell first: export EVREN_API_KEY=\"$(ulgen token)\""`, ""},
		// A TOML multi-line or literal string is hidden too.
		{"api_key = \"\"\"\na secret\nmore\n\"\"\"", "api_key = [hidden]\n\n\n"},
		{"api_key = '''\na secret\n'''", "api_key = [hidden]\n\n"},
		// Every string of an array or object is hidden, the address and the
		// punctuation keep.
		{`"api_keys": ["sk-1", "sk-2"]`, `"api_keys": [[hidden], [hidden]]`},
		{`"api_key": {"k": "v", "n": "w"}`, `"api_key": {"k": [hidden], "n": [hidden]}`},
		// A value that is a bare variable reference stays; one that merely
		// begins with a dollar sign is hidden.
		{`"apiKey": "$OPENAI_API_KEY"`, ""},
		{`"apiKey": "$ecret1"`, `"apiKey": [hidden]`},
		{`"apiKey": "prefix $SECRET"`, `"apiKey": [hidden]`},
		// More names are secrets.
		{`"openai_key": "k"`, `"openai_key": [hidden]`},
		{`"x-api-key": "k"`, `"x-api-key": [hidden]`},
		{`"gh_pat": "k"`, `"gh_pat": [hidden]`},
		{`"PASSWD": "k"`, `"PASSWD": [hidden]`},
		{`"credential": "k"`, `"credential": [hidden]`},
		// A name that ends in "env" or holds "tokens" names a variable or a
		// count, not a secret.
		{`"apiKeyEnv": "OPENAI_API_KEY"`, ""},
		{`"api_key_env": "OPENAI_API_KEY"`, ""},
		{`"keyEnv": "X"`, ""},
		{`"tokenizer": "cl100k"`, ""},
		{`"maxOutputTokens": 4096`, ""},
		// A container is not a value, and ordinary text is not a name.
		{`"secrets": {`, ""},
		{`"tokens": [`, ""},
		{`"Session token": "Oturum belirteci"`, ""},
		{`"name": "token"`, ""},
		{`"model": "deepseek-v4.1-flash"`, ""},
		{``, ""},
	} {
		want := tc[1]
		if want == "" {
			want = tc[0]
		}
		if got := maskSecrets(tc[0]); got != want {
			t.Errorf("maskSecrets(%q)\n got  %q\n want %q", tc[0], got, want)
		}
	}
}

// A masked value never takes the line break with it: the text after masking
// has the lines it had before. (A bare value that ran to the end of its line
// used to pull the next line up onto its own.) Moved here from setup_test.go.
func TestMaskSecretsKeepsEveryLineBreak(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"main:\n  api_key: abc123\nother: v\n", "main:\n  api_key: [hidden]\nother: v\n"},
		{"api_key = abc123\r\nmodel = \"m\"\r\n", "api_key = [hidden]\r\nmodel = \"m\"\r\n"},
		{"token = 12345\n", "token = [hidden]\n"},
		{"token = 12345", "token = [hidden]"},
		{"password = \"p\"\nname = \"n\"\n", "password = [hidden]\nname = \"n\"\n"},
		{"  \"apiKey\": \"abc\",\n  \"model\": \"m\"\n", "  \"apiKey\": [hidden],\n  \"model\": \"m\"\n"},
		{"secret = [\"a\", \"b\"]\nnext = 1\n", "secret = [[hidden], [hidden]]\nnext = 1\n"},
		{"token =\nnext = 1\n", "token =\nnext = 1\n"},
	} {
		got := maskSecrets(tc.in)
		if got != tc.want {
			t.Errorf("maskSecrets(%q)\n got  %q\n want %q", tc.in, got, tc.want)
		}
		if strings.Count(got, "\n") != strings.Count(tc.in, "\n") {
			t.Errorf("maskSecrets(%q) changed the number of lines: %q", tc.in, got)
		}
	}
}
