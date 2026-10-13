package jsonc

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func set(t *testing.T, d *Doc, value any, path ...string) {
	t.Helper()
	if err := d.Set(value, path...); err != nil {
		t.Fatalf("Set %v: %v", path, err)
	}
}

func TestUneditedDocumentIsUnchanged(t *testing.T) {
	src := "// header\n{\n\t\"a\": 1, // one\n\t/* two */ \"b\": [1, 2,],\n}\n"
	if got := string(mustParse(t, src).Bytes()); got != src {
		t.Errorf("got %q", got)
	}
}

func TestSetInEmptyDocument(t *testing.T) {
	d := New()
	set(t, d, "tr", "general", "language")
	set(t, d, 4, "$version")
	want := "{\n  \"general\": {\n    \"language\": \"tr\"\n  },\n  \"$version\": 4\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetKeepsCommentsAndFormatting(t *testing.T) {
	src := `{
  // my theme
  "theme": "dark", // trailing
  "model": {
    "name": "x" // the model
  }
  /* end */
}
`
	d := mustParse(t, src)
	set(t, d, "light", "theme")
	set(t, d, "https://e/v1", "model", "baseUrl")
	set(t, d, Object{{"language", "tr"}}, "general")
	want := `{
  // my theme
  "theme": "light", // trailing
  "model": {
    "name": "x", // the model
    "baseUrl": "https://e/v1"
  },
  /* end */
  "general": {
    "language": "tr"
  }
}
`
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetReplacesOnlyTheValue(t *testing.T) {
	src := "{\n  \"a\": /* before */ {\"x\": 1} /* after */,\n  \"b\": 2\n}\n"
	d := mustParse(t, src)
	set(t, d, []int{1, 2}, "a")
	want := "{\n  \"a\": /* before */ [\n    1,\n    2\n  ] /* after */,\n  \"b\": 2\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetUsesTheDocumentsIndentation(t *testing.T) {
	d := mustParse(t, "{\n\t\"a\": {\n\t\t\"x\": 1\n\t}\n}\n")
	set(t, d, Object{{"z", true}}, "a", "y")
	set(t, d, Object{{"k", Object{{"deep", 1}}}}, "b")
	want := "{\n\t\"a\": {\n\t\t\"x\": 1,\n\t\t\"y\": {\n\t\t\t\"z\": true\n\t\t}\n\t},\n\t\"b\": {\n\t\t\"k\": {\n\t\t\t\"deep\": 1\n\t\t}\n\t}\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWrittenValuesFollowTheLineTheyStandOn(t *testing.T) {
	runCases(t, lineCases, asWritten)
}

// lineCases are the cases of the test above.
var lineCases = []editCase{
	{"a member with an indentation of its own", `{
  "a": 1,
        "odd": {"x": 1},
  "z": 0
}
`, setCall(Object{{"y", 2}}, "odd"), `{
  "a": 1,
        "odd": {
          "y": 2
        },
  "z": 0
}
`, ""},
	{"a member behind another on its line", `{
    "a": 1, "b": {"x": 1}
}
`, setCall(Object{{"y", 2}}, "b"), `{
    "a": 1, "b": {
        "y": 2
    }
}
`, ""},
	{"a list on the line its document opens on", `{"l": []}`, oursX(ux), `{"l": [
  {
    "u": "x"
  }
]}`, ""},
	{"a member behind a comment on its line", `{
      /* c */ "b": {"x": 1},
  "z": 0
}
`, setCall(Object{{"y", 2}}, "b"), `{
      /* c */ "b": {
        "y": 2
      },
  "z": 0
}
`, ""},
	{"an element that shares its lines with the brackets", `{
  "l": [{
    "u": "x"
  }]
}
`, oursX(uxn(2)), `{
  "l": [{
    "u": "x",
    "n": 2
  }]
}
`, ""},
	{"an object with a value over several lines is not on one line", `{"a": {
  "x": 1
}}`, setCall(2, "b"), `{"a": {
  "x": 1
},
  "b": 2}`, ""},
	{"new members are indented like the last one", `{
      "a": 1,
    "b": 2
}
`, setCall(3, "c"), `{
      "a": 1,
    "b": 2,
    "c": 3
}
`, ""},
	{"a comment in front of every member: one unit more than the parent", `{
  "p": {
        /* a */ "a": 1
  }
}
`, setCall(2, "p", "b"), `{
  "p": {
        /* a */ "a": 1,
    "b": 2
  }
}
`, ""},
}

func TestParseKeepsNoReferenceToItsInput(t *testing.T) {
	const text = "{\n  \"a\": 1 // one\n}\n"
	src := []byte(text)
	d := mustParse(t, string(src))
	d2, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for i := range src {
		src[i] = ' '
	}
	if got := string(d2.Bytes()); got != text || string(d.Bytes()) != text {
		t.Errorf("the document changed with the caller's buffer: %q", got)
	}
}

func TestSetCreatesParents(t *testing.T) {
	d := mustParse(t, "{\n  \"a\": 1\n}\n")
	set(t, d, false, "privacy", "usageStatisticsEnabled")
	want := "{\n  \"a\": 1,\n  \"privacy\": {\n    \"usageStatisticsEnabled\": false\n  }\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetKeepsTrailingCommaStyle(t *testing.T) {
	d := mustParse(t, "{\n  \"a\": 1,\n}\n")
	if !d.TrailingComma() {
		t.Error("TrailingComma = false")
	}
	set(t, d, 2, "b")
	want := "{\n  \"a\": 1,\n  \"b\": 2,\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if mustParse(t, "{\n  \"a\": [1, 2]\n}\n").TrailingComma() {
		t.Error("TrailingComma = true for a strict document")
	}
}

func TestSetInOneLineObject(t *testing.T) {
	d := mustParse(t, `{"a": {"x": 1}}`)
	set(t, d, 2, "a", "y")
	set(t, d, 3, "b")
	want := `{"a": {"x": 1, "y": 2}, "b": 3}`
	if got := string(d.Bytes()); got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestSetThroughANonObjectFails(t *testing.T) {
	d := mustParse(t, `{"model": "x"}`)
	err := d.Set("y", "model", "name")
	if err == nil || !strings.Contains(err.Error(), "model is not an object") {
		t.Errorf("err = %v", err)
	}
	if string(d.Bytes()) != `{"model": "x"}` {
		t.Errorf("document changed: %s", d.Bytes())
	}
}

func TestObjectKeepsOrderAndDoesNotEscapeHTML(t *testing.T) {
	d := New()
	set(t, d, Object{{"zeta", "{env:A&B}"}, {"alpha", []string{"<x>"}}}, "p")
	want := "{\n  \"p\": {\n    \"zeta\": \"{env:A&B}\",\n    \"alpha\": [\n      \"<x>\"\n    ]\n  }\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGet(t *testing.T) {
	d := mustParse(t, "{\n  // c\n  \"model\": {\"name\": \"x\", \"n\": [1, 2,],},\n}\n")
	if v, ok := d.Get("model", "name"); !ok || v != "x" {
		t.Errorf("name = %v, %v", v, ok)
	}
	if v, ok := d.Get("model", "n"); !ok || len(v.([]any)) != 2 {
		t.Errorf("n = %v, %v", v, ok)
	}
	if _, ok := d.Get("model", "missing"); ok {
		t.Error("missing key found")
	}
	if _, ok := d.Get("model", "name", "deeper"); ok {
		t.Error("path through a string found")
	}
	if v, ok := d.Get(); !ok || v.(map[string]any)["model"] == nil {
		t.Errorf("root = %v, %v", v, ok)
	}
}

func TestEmptyPathIsAnError(t *testing.T) {
	const src = "{\n  \"a\": 1\n}\n"
	d := mustParse(t, src)
	for name, err := range map[string]error{
		"Set":         d.Set(1),
		"UpdateArray": d.UpdateArray(nil, []any{1}),
	} {
		if err == nil || err.Error() != "the path is empty" {
			t.Errorf("%s without a path: err = %v", name, err)
		}
	}
	if got := string(d.Bytes()); got != src {
		t.Errorf("the document changed: %q", got)
	}
}

func TestParseErrors(t *testing.T) {
	_, err := Parse([]byte("{\n  \"a\": 1,\n  \"b\": \n}\n"))
	if err == nil || !strings.Contains(err.Error(), "line 4, column 1") {
		t.Errorf("err = %v", err)
	}
	if _, err := Parse([]byte("[1]")); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Errorf("err = %v", err)
	}
}

// A parse error says where the text stops making sense and what kind of
// problem that is. It never repeats text of the file: a value whose quotes
// were forgotten, or that has a bad escape in it, can be a key.
func TestParseErrorsHoldNoTextOfTheFile(t *testing.T) {
	const secret = "ZqZqZq" // letters that no message of the parser holds
	for name, tc := range map[string]struct{ src, want string }{
		"a value without quotes":      {"{\n  \"key\": " + secret + "\n}\n", "line 2, column 10: invalid literal"},
		"a bad escape in a value":     {"{\n  \"key\": \"" + secret + "\\q\"\n}\n", "line 2, column 10: invalid literal"},
		"a bad escape in a name":      {"{\n  \"" + secret + "\\q\": 1\n}\n", "line 2, column 3: invalid literal"},
		"a name without quotes":       {"{\n  " + secret + ": 1\n}\n", "line 2, column 3: invalid literal"},
		"text behind a value":         {"{\n  \"key\": \"x\" " + secret + "\n}\n", "line 2, column 14: invalid character after object value (expecting ',' or '}')"},
		"text behind a name":          {"{\n  \"key\" " + secret + "\n}\n", "line 2, column 9: invalid character after object name"},
		"text behind an element":      {"{\"l\": [1 " + secret + "]}", "line 1, column 10: invalid character after array value (expecting ',' or ']')"},
		"text behind the document":    {"{}\n" + secret + "\n", "line 2, column 1: invalid character after top-level value"},
		"text in front of the object": {secret + " {}", "line 1, column 1: invalid literal"},
		"a character of no kind":      {"{\n  \"key\": ?" + secret + "\n}\n", "line 2, column 10: invalid character at start of value"},
		"a byte that is not text":     {"{\n  \"key\": \xff" + secret + "\n}\n", "line 2, column 10: invalid character at start of value"},
		"a string that never ends":    {"{\"key\": \"" + secret, "line 1, column 16: parsing string: unexpected EOF"},
		"a comment that never ends":   {"{} /* " + secret, "line 1, column 4: parsing comment: unexpected EOF"},
		// The two inputs of the review: a key whose quotes were forgotten, and
		// a string with a bad escape, which the parser used to print whole.
		"a key without quotes":         {"{\n  \"apiKey\": sk-" + secret + "-abc123DEF456\n}\n", "line 2, column 13: invalid literal"},
		"a path with single backslash": {"{\n  \"path\": \"C:\\Users\\x\\" + secret + "-token-123\"\n}\n", "line 2, column 11: invalid literal"},
		"a raw tab in a string":        {"{\n  \"key\": \"" + secret + "\ttab\"\n}\n", "line 2, column 10: invalid literal"},
		// A top-level value that is no object is refused where it starts.
		"a top-level list":   {"// mine\n  [\"" + secret + "\"]\n", "line 2, column 3: the top-level value is not an object"},
		"a top-level string": {"\"" + secret + "\"", "line 1, column 1: the top-level value is not an object"},
		"a top-level number": {"\xef\xbb\xbf\r\n\r\n 5", "line 3, column 2: the top-level value is not an object"},
		// A character of JSON's own syntax is not the user's text; it says
		// what is wrong and stays.
		"a brace too early": {"{\n  \"a\": \n}\n", "line 3, column 1: invalid character '}' at start of value"},
		"a missing comma":   {"{\"a\": 1 \"b\": 2}", "line 1, column 9: invalid character '\"' after object value (expecting ',' or '}')"},
		"a colon too many":  {"{\"a\":: 1}", "line 1, column 6: invalid character ':' at start of value"},
	} {
		_, err := Parse([]byte(tc.src))
		if err == nil {
			t.Errorf("%s: the text was accepted", name)
			continue
		}
		if got := err.Error(); got != tc.want || strings.ContainsAny(got, "Zq") {
			t.Errorf("%s:\n got  %q\n want %q", name, got, tc.want)
		}
	}
}

// A nil drop knows no element of its own, so every call appends its values
// again; a drop that knows them makes the second call change nothing.
func TestUpdateArrayWithoutADropAppendsEveryTime(t *testing.T) {
	d := mustParse(t, `{"l": [{"u": "o"}]}`)
	entry := Object{{"u", "x"}}
	for range 2 {
		if err := d.UpdateArray(nil, []any{entry}, "l"); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := d.Get("l"); len(list.([]any)) != 3 {
		t.Errorf("after two calls without a drop: %q", d.Bytes())
	}
	before := string(d.Bytes())
	d = mustParse(t, `{"l": [{"u": "o"}]}`)
	for range 2 {
		if err := d.UpdateArray(dropWhere("u", "x"), []any{entry}, "l"); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := d.Get("l"); len(list.([]any)) != 2 || string(d.Bytes()) == before {
		t.Errorf("after two calls with a drop: %q", d.Bytes())
	}
}

// HasValue is the question "is there anything in this document", asked of
// the text. Get cannot answer it: it also says "not there" for a value it
// cannot decode.
func TestHasValue(t *testing.T) {
	for src, want := range map[string]bool{
		"":                        false,
		" \n\t\r\n":               false,
		"// mine":                 false,
		"// mine\n/* and */\n":    false,
		"\xef\xbb\xbf":            false,
		"\xef\xbb\xbf// mine\n":   false,
		"{}":                      true,
		"{ }\n":                   true,
		"// mine\n{\"a\": 1}":     true,
		"{\"a\": 1e400}":          true, // a number Get cannot decode
		"{\"a\": {\"b\": 1e400}}": true,
	} {
		d := mustParse(t, src)
		if got := d.HasValue(); got != want {
			t.Errorf("%q: HasValue = %v, want %v", src, got, want)
		}
	}
	// Get gives the same answer for an empty text and for one it cannot
	// decode, which is why it is not the question to ask.
	if _, ok := mustParse(t, `{"a": 1e400}`).Get(); ok {
		t.Error("Get decodes a number that does not fit; the test above no longer shows the difference")
	}
	// The first Set gives a document its value.
	d := mustParse(t, "// mine\n")
	set(t, d, 1, "a")
	if !d.HasValue() {
		t.Errorf("after Set: %q has no value", d.Bytes())
	}
	if !New().HasValue() {
		t.Error("a new document has no value")
	}
}

// What can still go wrong in a text that parses is said in plain words, and
// without the value that caused it: that is text of the user's file too.
func TestDecodeErrorsArePlainSentences(t *testing.T) {
	deep := strings.Repeat(`{"ZqZq":`, 10001) + "1" + strings.Repeat("}", 10001)
	for name, tc := range map[string]struct {
		src  string
		edit func(*Doc) error
		want string
	}{
		"a number that does not fit": {`{"ZqZq": [{"u": "x", "n": 1e400}]}`,
			func(d *Doc) error { return d.UpdateArray(dropWhere("u", "x"), nil, "ZqZq") }, "a number is too large to read"},
		"a negative one": {`{"l": [-1e999, "ZqZq"]}`,
			func(d *Doc) error { return d.UpdateArray(dropWhere("u", "x"), []any{1}, "l") }, "a number is too large to read"},
		"values nested too deeply": {deep,
			func(d *Doc) error { return d.Set(1, "new") }, "the values are nested too deeply"},
		"the same for a list": {deep,
			func(d *Doc) error { return d.UpdateArray(nil, []any{1}, "new") }, "the values are nested too deeply"},
	} {
		d := mustParse(t, tc.src)
		err := tc.edit(d)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if got := string(d.Bytes()); got != tc.src {
			t.Errorf("%s: the document changed", name)
		}
	}
	// Without a drop nothing is decoded as a float, and the number stays as
	// it is written.
	d := mustParse(t, `{"l": [1e400]}`)
	if err := d.UpdateArray(nil, []any{1}, "l"); err != nil || !strings.HasPrefix(string(d.Bytes()), `{"l": [1e400,`) {
		t.Errorf("no drop: %v, %q", err, d.Bytes())
	}
}

func dropWhere(key, value string) func(any) bool {
	return func(e any) bool {
		m, ok := e.(map[string]any)
		return ok && m[key] == value
	}
}

func TestUpdateArrayCreatesTheArray(t *testing.T) {
	d := New()
	if err := d.UpdateArray(dropWhere("u", "x"), []any{Object{{"id", "a"}}, Object{{"id", "b"}}}, "p", "list"); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"p\": {\n    \"list\": [\n      {\n        \"id\": \"a\"\n      },\n      {\n        \"id\": \"b\"\n      }\n    ]\n  }\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateArrayKeepsForeignElementsUntouched(t *testing.T) {
	src := `{
  "p": {
    "list": [
      // mine
      { "id": "keep", "u": "other" }, // stays
      {
        "id": "old",
        "u": "x"
      }
    ]
  }
}
`
	d := mustParse(t, src)
	if err := d.UpdateArray(dropWhere("u", "x"), []any{Object{{"id", "new"}, {"u", "x"}}}, "p", "list"); err != nil {
		t.Fatal(err)
	}
	want := `{
  "p": {
    "list": [
      // mine
      { "id": "keep", "u": "other" }, // stays
      {
        "id": "new",
        "u": "x"
      }
    ]
  }
}
`
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// The same update again changes nothing.
	if err := d.UpdateArray(dropWhere("u", "x"), []any{Object{{"id", "new"}, {"u", "x"}}}, "p", "list"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != want {
		t.Errorf("second update changed the text:\n%s", got)
	}
}

func TestUpdateArrayMatchesSetLayout(t *testing.T) {
	// An array first written by Set and then rewritten by UpdateArray has
	// the same text, so a second setup run finds nothing to change.
	entries := []any{Object{{"id", "a"}, {"u", "x"}}, Object{{"id", "b"}, {"u", "x"}}}
	a := New()
	set(t, a, entries, "p", "list")
	b := mustParse(t, string(a.Bytes()))
	if err := b.UpdateArray(dropWhere("u", "x"), entries, "p", "list"); err != nil {
		t.Fatal(err)
	}
	if string(a.Bytes()) != string(b.Bytes()) {
		t.Errorf("Set:\n%s\nUpdateArray:\n%s", a.Bytes(), b.Bytes())
	}
}

func TestUpdateArrayDropsEverything(t *testing.T) {
	d := mustParse(t, "{\n  \"list\": [\n    {\"u\": \"x\"},\n    {\"u\": \"x\"}\n  ]\n}\n")
	if err := d.UpdateArray(dropWhere("u", "x"), nil, "list"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), "{\n  \"list\": []\n}\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateArrayKeepsTrailingCommaAndOneLineElements(t *testing.T) {
	const src = "{\n  \"list\": [{\"u\": \"o\"}, {\"u\": \"x\"},],\n}\n"
	d := mustParse(t, src)
	// The entry that is there already: not a byte changes.
	if err := d.UpdateArray(dropWhere("u", "x"), []any{Object{{"u", "x"}}}, "list"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != src {
		t.Errorf("got:\n%s\nwant:\n%s", got, src)
	}
	// Another entry takes its place on the line. A second one starts a line
	// of its own and takes the trailing comma; the bracket stays behind the
	// last element, where the user put it.
	entries := []any{Object{{"u", "x"}, {"n", 1}}, Object{{"u", "x"}, {"n", 2}}}
	if err := d.UpdateArray(dropWhere("u", "x"), entries, "list"); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"list\": [{\"u\": \"o\"}, {\"u\": \"x\", \"n\": 1},\n    {\n      \"u\": \"x\",\n      \"n\": 2\n    },],\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateArrayOnANonArrayFails(t *testing.T) {
	d := mustParse(t, `{"list": {"protocol": "openai"}}`)
	err := d.UpdateArray(dropWhere("u", "x"), nil, "list")
	if err == nil || !strings.Contains(err.Error(), "list is not a list") {
		t.Errorf("err = %v", err)
	}
}
