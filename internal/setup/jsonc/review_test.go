package jsonc

import (
	"strings"
	"testing"
)

// Every input the review of the first version reproduced (its labels E… and
// R…), with the call the review made. Where the first version produced a
// broken or poorer text, want is what the guarantees ask for instead; the
// cases the review found clean are here as well.

func isU(val string) func(any) bool {
	return func(e any) bool {
		m, ok := e.(map[string]any)
		return ok && m["u"] == val
	}
}

func isNum(n float64) func(any) bool {
	return func(e any) bool { f, ok := e.(float64); return ok && f == n }
}

var (
	ux      = Object{{"u", "x"}}
	oursX   = func(add ...any) call { return updateCall(`u=="x"`, isU("x"), add, "l") }
	drop2   = updateCall("==2", isNum(2), nil, "l")
	deepKey = strings.Split(strings.TrimSpace(strings.Repeat("k ", 40)), " ")
)

var reviewCases = []editCase{
	// Line endings (finding 4).
	{"E1a", "{\r\n  \"a\": 1\r\n}\r\n", setCall(2, "b"),
		"{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n", ""},
	{"E1b", "{\r\n  \"a\": 1\r\n}\r\n", setCall(Object{{"k", 1}}, "b"),
		"{\r\n  \"a\": 1,\r\n  \"b\": {\r\n    \"k\": 1\r\n  }\r\n}\r\n", ""},
	{"E1c", "{\r\n  \"a\": 1,\r\n  \"z\": 0\r\n}\r\n", setCall(Object{{"k", 1}}, "a"),
		"{\r\n  \"a\": {\r\n    \"k\": 1\r\n  },\r\n  \"z\": 0\r\n}\r\n", ""},
	{"E1d", "{\r\n  \"l\": [\r\n    {\"u\": \"o\"}, // stays\r\n    {\"u\": \"x\"}\r\n  ]\r\n}\r\n", oursX(ux, ux),
		"{\r\n  \"l\": [\r\n    {\"u\": \"o\"}, // stays\r\n    {\"u\": \"x\"},\r\n    {\r\n      \"u\": \"x\"\r\n    }\r\n  ]\r\n}\r\n", ""},

	// Byte-order mark.
	{"E2", bom + "{\n  \"a\": 1\n}\n", setCall(2, "b"),
		bom + "{\n  \"a\": 1,\n  \"b\": 2\n}\n", ""},

	// Duplicate keys (finding 3).
	{"E3a", "{\n  \"a\": 1,\n  \"a\": 2\n}\n", setCall(9, "a"),
		same, "a appears more than once"},
	{"E3b", "{\n  \"m\": {\"x\": 1},\n  \"m\": {\"x\": 2}\n}\n", setCall(9, "m", "x"),
		same, "m appears more than once"},
	{"E3c", "{\n  \"l\": [{\"u\": \"x\"}],\n  \"l\": [{\"u\": \"o\"}]\n}\n", oursX(ux),
		same, "l appears more than once"},

	// Block comments that span lines (finding 1).
	{"E4a", "{\n  \"l\": [\n    {\"u\": \"o\"}, /* my favourite\n       model */\n    {\"u\": \"x\"}\n  ]\n}\n", oursX(ux),
		same, ""},
	{"E4b", "{\n  \"l\": [\n    {\"u\": \"o\"},\n    {\"u\": \"x\"} /* ours\n       really */\n  ]\n}\n", oursX(),
		"{\n  \"l\": [\n    {\"u\": \"o\"}\n    /* ours\n       really */\n  ]\n}\n", ""},
	{"E4c", "{\n  \"l\": [\n    {\"u\": \"o\"},\n    {\"u\": \"x\"} /* ours\n       really */\n  ]\n}\n", oursX(ux),
		same, ""},

	// Comments on and above the closing line (findings 1 and 2).
	{"E5a", "{\n  \"a\": 1\n  /* end */ }\n", setCall(2, "b"),
		"{\n  \"a\": 1,\n  \"b\": 2\n  /* end */ }\n", ""},
	{"E5b", "{\n  \"a\": 1 /* note\n  continues */ }\n", setCall(2, "b"),
		"{\n  \"a\": 1 /* note\n  continues */,\n  \"b\": 2 }\n", ""},
	{"E5c", "{\n  \"a\": 1\n  /*\n   * \"b\": 2\n   */\n}\n", setCall(2, "b"),
		"{\n  \"a\": 1,\n  /*\n   * \"b\": 2\n   */\n  \"b\": 2\n}\n", ""},
	{"E5d", "{\n  \"l\": [\n    {\"u\": \"o\"}\n    /* end */ ]\n}\n", oursX(ux),
		"{\n  \"l\": [\n    {\"u\": \"o\"},\n    {\n      \"u\": \"x\"\n    }\n    /* end */ ]\n}\n", ""},

	// Comments next to the entry a second run replaces (finding 2).
	{"E6", "{\n  \"l\": [\n    {\"u\": \"x\"}\n    // add more here\n  ]\n}\n", oursX(ux),
		same, ""},
	{"E7", "{\n  \"l\": [\n    // my local models\n    {\"u\": \"x\"},\n    {\"u\": \"o\"}\n  ]\n}\n", oursX(ux),
		same, ""},

	// A comment in front of a member on its line: no indentation to copy.
	{"E8a", "{\n  /* c */ \"a\": 1\n}\n", setCall(Object{{"k", 1}}, "b"),
		"{\n  /* c */ \"a\": 1,\n  \"b\": {\n    \"k\": 1\n  }\n}\n", ""},
	{"E8b", "{\n  \"l\": [\n    /* c */ {\"u\": \"o\"}\n  ]\n}\n", oursX(ux),
		"{\n  \"l\": [\n    /* c */ {\"u\": \"o\"},\n    {\n      \"u\": \"x\"\n    }\n  ]\n}\n", ""},

	// Keys that need escaping.
	{"E9a", "{\n  \"a\": 1\n}\n", setCall(1, "we\"ird\\k\ney", "ş<ğ>&"),
		"{\n  \"a\": 1,\n  \"we\\\"ird\\\\k\\ney\": {\n    \"ş<ğ>&\": 1\n  }\n}\n", ""},
	{"E9b", "{\n  \"a\\u0062\": 1\n}\n", setCall(2, "ab"),
		"{\n  \"a\\u0062\": 2\n}\n", ""},

	// Empty objects.
	{"E10a", "{\"a\": {}}", setCall(1, "a", "x"),
		"{\"a\": {\"x\": 1}}", ""},
	{"E10b", "{\n  // nothing yet\n}\n", setCall(1, "x"),
		"{\n  // nothing yet\n  \"x\": 1\n}\n", ""},
	{"E10c", "{ /* c */ }", setCall(1, "x"),
		"{ /* c */ \"x\": 1 }", ""},
	{"E10d", "{\n  \"a\": {\n  }\n}\n", setCall(1, "a", "x"),
		"{\n  \"a\": {\n    \"x\": 1\n  }\n}\n", ""},

	// A removal on a path that does not exist.
	{"E11", "{\n  \"a\": 1\n}\n", updateCall(`u=="x"`, isU("x"), nil, "p", "l"),
		same, ""},

	// Texts without a value.
	{"E12a", "", setCall(1, "a"),
		"{\n  \"a\": 1\n}\n", ""},
	{"E12b", "\n", setCall(1, "a"),
		"\n{\n  \"a\": 1\n}\n", ""},
	{"E12c", "// nothing\n", setCall(1, "a"),
		"// nothing\n{\n  \"a\": 1\n}\n", ""},

	// A deep path, twice (the runner makes every call twice).
	{"E13", "{\n\t\"a\": 1\n}\n", setCall(true, deepKey...),
		"{\n\t\"a\": 1,\n\t" + nest(deepKey, "true", "\t", "\t") + "\n}\n", ""},

	// Other kinds of value where an object or a list is expected.
	{"E14 Set through null", `{"a": null}`, setCall(1, "a", "b"), same, "a is not an object"},
	{"E14 Set through list", `{"a": [1]}`, setCall(1, "a", "b"), same, "a is not an object"},
	{"E14 Set through number", `{"a": 5}`, setCall(1, "a", "b"), same, "a is not an object"},
	{"E14 UpdateArray through null", `{"a": null}`, updateCall(`u=="x"`, isU("x"), []any{ux}, "a", "b"), same, "a is not an object"},
	{"E14 UpdateArray through list", `{"a": [1]}`, updateCall(`u=="x"`, isU("x"), []any{ux}, "a", "b"), same, "a is not an object"},
	{"E14 UpdateArray through number", `{"a": 5}`, updateCall(`u=="x"`, isU("x"), []any{ux}, "a", "b"), same, "a is not an object"},
	{"E14 UpdateArray on null", `{"a": null}`, updateCall(`u=="x"`, isU("x"), []any{ux}, "a"), same, "a is not a list"},
	{"E14 mixed list", "{\n  \"a\": [1, \"s\", null, [2], {\"u\": \"x\"}]\n}\n", updateCall(`u=="x"`, isU("x"), []any{ux}, "a"),
		same, ""},

	// Trailing commas.
	{"E15a", "{\n  \"l\": [\n    1,\n    2\n  ]\n}\n", drop2,
		"{\n  \"l\": [\n    1\n  ]\n}\n", ""},
	{"E15b", "{\n  \"l\": [1 /* c */, 2]\n}\n", drop2,
		"{\n  \"l\": [1 /* c */]\n}\n", ""},
	{"E15c", "{\n  \"l\": [\n    1, // one\n    2, // two\n  ],\n}\n", drop2,
		"{\n  \"l\": [\n    1, // one\n    // two\n  ],\n}\n", ""},
	{"E15d", `{"a": 1,}`, setCall(2, "b"),
		`{"a": 1, "b": 2,}`, ""},

	// An equal value over the user's own layout.
	{"E16", "{\n  \"a\": {\"x\": 1}\n}\n", setCall(Object{{"x", 1}}, "a"),
		same, ""},

	// A one-line object with a comment at its end.
	{"E17", `{"a": 1 /* c */}`, setCall(2, "b"),
		`{"a": 1 /* c */, "b": 2}`, ""},

	// Empty lists.
	{"E20a", "{\n  \"l\": []\n}\n", oursX(ux),
		"{\n  \"l\": [\n    {\n      \"u\": \"x\"\n    }\n  ]\n}\n", ""},
	{"E20b", "{\n  \"l\": [\n    // none yet\n  ]\n}\n", oursX(ux),
		"{\n  \"l\": [\n    // none yet\n    {\n      \"u\": \"x\"\n    }\n  ]\n}\n", ""},

	// Line comments next to what is removed (finding 1).
	{"E21a", "{\n  \"l\": [\n    {\"u\": \"x\"}\n    // add more here\n  ]\n}\n", oursX(),
		"{\n  \"l\": [\n    // add more here\n  ]\n}\n", ""},
	{"E21b", "{\n  \"l\": [\n    {\"u\": \"o\"}, // stays\n    {\"u\": \"x\"}]\n}\n", oursX(),
		"{\n  \"l\": [\n    {\"u\": \"o\"} // stays\n    ]\n}\n", ""},
	{"E21c", "{\n  \"l\": [1, // one\n    2, 3]\n}\n", drop2,
		"{\n  \"l\": [1, // one\n    3]\n}\n", ""},
	{"E21d", "{\n  \"l\": [\n    {\"u\": \"x\"} // ours\n  ]\n}\n", oursX(),
		"{\n  \"l\": [\n    // ours\n  ]\n}\n", ""},

	// Repeated runs.
	{"R1", "{\n  \"list\": [{\"u\": \"o\"}, {\"u\": \"x\"},],\n}\n", updateCall(`u=="x"`, isU("x"), []any{ux}, "list"),
		same, ""},
	{"R2", "{\n  \"l\": [\n    {\"u\": \"o\"},\n    // added by hand below\n    {\"u\": \"x\"},\n    {\"u\": \"o2\"}\n  ]\n}\n", oursX(ux),
		same, ""},
	{"R3", "{\n  \"l\": [\n    {\"u\": \"o\"},\n    {\"u\": \"x\"}\n    // keep this list short\n  ]\n}\n", oursX(ux),
		same, ""},
	{"R4", "{\n  \"a\": 1\n\n}\n", setCall(2, "b"),
		"{\n  \"a\": 1,\n  \"b\": 2\n\n}\n", ""},
	{"R5", "{\n  \"a\": 1 // one\n}\n", setCall(1, "a"),
		same, ""},
}

// nest writes value under a path of keys the way Set lays out new objects:
// the first key on a line indented by indent, each object one unit deeper.
func nest(path []string, value, indent, unit string) string {
	if len(path) == 0 {
		return value
	}
	inner := nest(path[1:], value, indent+unit, unit)
	if len(path) == 1 {
		return "\"" + path[0] + "\": " + inner
	}
	return "\"" + path[0] + "\": {\n" + indent + unit + inner + "\n" + indent + "}"
}

func TestReviewInputs(t *testing.T) {
	runCases(t, reviewCases, asWritten)
}

// What the review looked at besides the text after an edit.
func TestReviewInputsRead(t *testing.T) {
	for _, src := range []string{"", "\n", "// nothing\n"} { // E12
		d := mustParse(t, src)
		if got := string(d.Bytes()); got != src {
			t.Errorf("Bytes of %q = %q", src, got)
		}
		if v, ok := d.Get(); ok {
			t.Errorf("Get on %q = %v", src, v)
		}
	}
	d := mustParse(t, "{\n  \"a\": 1,\n  \"a\": 2\n}\n") // E3a
	if v, _ := d.Get("a"); v != 2.0 {
		t.Errorf("Get(a) = %v, want the last one", v)
	}
	d = mustParse(t, "{\n  \"m\": {\"x\": 1},\n  \"m\": {\"x\": 2}\n}\n") // E3b
	if v, _ := d.Get("m", "x"); v != 2.0 {
		t.Errorf("Get(m, x) = %v, want that of the last m", v)
	}
	d = mustParse(t, "{\n\t\"a\": 1\n}\n") // E13
	set(t, d, true, deepKey...)
	if v, ok := d.Get(deepKey...); !ok || v != true {
		t.Errorf("Get(deep) = %v, %v", v, ok)
	}
}
