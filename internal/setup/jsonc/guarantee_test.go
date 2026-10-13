package jsonc

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// A test for each guarantee of the package (G1 to G10 in doc.go). The
// tables are written with two spaces and line feeds and run in every style;
// each case goes through verify, which checks all the guarantees that can
// be seen from the outside, so a table's name only says which one its
// inputs are aimed at.

// uxn is an entry that isU("x") takes for ours, with a number to tell two
// of them apart.
func uxn(n int) Object { return Object{{"u", "x"}, {"n", n}} }

// appendCall is an UpdateArray that drops nothing.
func appendCall(values ...any) call { return updateCall("nothing", nil, values, "l") }

func dropNum(n float64) call { return updateCall("a number", isNum(n), nil, "l") }

// G1: the result parses. These are the places where cutting text at a raw
// line break would cut a comment or let a line comment swallow a bracket.
func TestG1ResultParses(t *testing.T) {
	runCases(t, g1Cases, styles...)
}

// g1Cases are the cases of the test above.
var g1Cases = []editCase{
	{"replace behind a block comment that spans lines", `{
  "l": [
    {"u": "o"}, /* my favourite
       model */
    {"u": "x"}
  ]
}
`, oursX(uxn(2)), `{
  "l": [
    {"u": "o"}, /* my favourite
       model */
    {
      "u": "x",
      "n": 2
    }
  ]
}
`, ""},
	{"replace in front of a block comment that spans lines", `{
  "l": [
    {"u": "o"},
    {"u": "x"} /* ours
       really */
  ]
}
`, oursX(uxn(2)), `{
  "l": [
    {"u": "o"},
    {
      "u": "x",
      "n": 2
    } /* ours
       really */
  ]
}
`, ""},
	{"append behind a line comment", `{
  "l": [
    1 // one
  ]
}
`, appendCall(2), `{
  "l": [
    1, // one
    2
  ]
}
`, ""},
	{"new member behind a line comment", `{
  "a": 1 // c
}
`, setCall(2, "b"), `{
  "a": 1, // c
  "b": 2
}
`, ""},
	{"remove the first of two elements with line comments", `{
  "l": [
    1, // one
    2 // two
  ]
}
`, dropNum(1), `{
  "l": [
    // one
    2 // two
  ]
}
`, ""},
	{"remove the last of two elements with line comments", `{
  "l": [
    1, // one
    2 // two
  ]
}
`, dropNum(2), `{
  "l": [
    1 // one
    // two
  ]
}
`, ""},
	{"a block comment that holds a line comment and a quote", `{
  "a": 1 /* // not a line " */
}
`, setCall(2, "b"), `{
  "a": 1 /* // not a line " */,
  "b": 2
}
`, ""},
	{"a line comment that opens a block comment", `{
  "a": 1 // has /* in it
}
`, setCall(2, "b"), `{
  "a": 1, // has /* in it
  "b": 2
}
`, ""},
	{"strings that look like comments", `{
  "a": "// not a comment",
  "b": "/* nor this */"
}
`, setCall("*/ // \"", "c"), `{
  "a": "// not a comment",
  "b": "/* nor this */",
  "c": "*/ // \""
}
`, ""},
	{"a list that holds only a comment, on one line", `{
  "l": [ /* none */ ]
}
`, oursX(ux), `{
  "l": [ /* none */
    {
      "u": "x"
    } ]
}
`, ""},
	{"a closing bracket on the last element's line", `{
  "l": [
    1,
    2]
}
`, appendCall(3), `{
  "l": [
    1,
    2,
    3]
}
`, ""},
}

// G1, the safety net: a draft that breaks a promise is not taken over.
func TestG1SafetyNet(t *testing.T) {
	const src = "{\n  \"a\": 1 // one\n}\n"
	for name, breakIt := range map[string]func(w *draft){
		"would not parse":  func(w *draft) { w.root.Value.(*hujson.Object).AfterExtra = []byte(" // one") },
		"requested value":  func(w *draft) { w.root.Value.(*hujson.Object).Members[0].Value.Value = hujson.Literal("2") },
		"lose or repeat a": func(w *draft) { w.root.Value.(*hujson.Object).AfterExtra = []byte("\n") },
	} {
		d := mustParse(t, src)
		w, err := d.begin()
		if err != nil {
			t.Fatal(err)
		}
		breakIt(w)
		err = d.commit(w, map[string]any{"a": json.Number("1")})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("commit = %v, want an error that says %q", err, name)
		}
		if got := string(d.Bytes()); got != src {
			t.Errorf("%s: the document changed: %q", name, got)
		}
	}
	// The same draft, left whole, is taken over.
	d := mustParse(t, src)
	w, _ := d.begin()
	if err := d.commit(w, map[string]any{"a": json.Number("1")}); err != nil || string(d.Bytes()) != src {
		t.Errorf("commit of an untouched draft = %v, text %q", err, d.Bytes())
	}
	// What the net says about a draft that would not parse holds the place
	// and the kind of problem, and no text: the draft is the user's file.
	d = mustParse(t, src)
	w, _ = d.begin()
	w.root.Value.(*hujson.Object).Members[0].Value.Value = hujson.Literal("ZqZqZq")
	err := d.commit(w, map[string]any{"a": json.Number("1")})
	if want := "the change was not made, its result would not parse: line 2, column 8: invalid literal"; err == nil || err.Error() != want {
		t.Errorf("commit = %v, want %q", err, want)
	}
}

// Two edits on one document give the text that the second edit gives on the
// text the first one left: what an edit does depends on the text alone, not
// on what the document looked like when it was parsed. The adapters make
// several edits on one document, and a second run of setup starts from the
// text.
func TestTwoEditsOnOneDocument(t *testing.T) {
	isOne := func(e any) bool { return e == float64(1) || e == float64(2) }
	for name, tc := range map[string]struct {
		src           string
		first, second call
	}{
		// The review's input: the only line break of the text goes with the
		// elements, so the text has no line break of its own any more.
		"the last CRLF goes": {"{\"l\": [1,\r\n2]}",
			updateCall("1 or 2", isOne, nil, "l"), updateCall("nothing", nil, []any{3}, "l")},
		"the first line break changes": {"{\"l\": [1,\r\n2], \"x\": 1\n}",
			updateCall("1 or 2", isOne, nil, "l"), setCall(Object{{"k", 1}}, "new")},
		"the first indentation appears": {"{}",
			setCall(Object{{"k", 1}}, "a"), setCall(Object{{"k", 2}}, "b")},
		"tabs appear with the first member": {"{\"a\": {\n\t\t\"k\": 1\n\t}}",
			setCall(Object{{"k", 1}}, "b"), setCall([]any{1, 2}, "c")},
	} {
		twoEdits(t, name, tc.src, tc.first, tc.second)
	}
	// And at random: the documents and calls of the randomized test, with a
	// second call picked for the text the first one left.
	for i := range 3000 {
		seed := randomSeed + int64(i)
		r := rand.New(rand.NewSource(seed))
		src := randomDocument(r)
		first := randomCall(t, r, src)
		d := mustParse(t, src)
		if first.run(d) != nil {
			continue
		}
		twoEdits(t, fmt.Sprintf("seed %d", seed), src, first, randomCall(t, r, string(d.Bytes())))
	}
}

// twoEdits makes both calls on one document and, next to it, the second
// call on a document parsed from the text the first call left. Both must
// end with the same text and the same answer.
func twoEdits(t *testing.T, name, src string, first, second call) {
	t.Helper()
	one := mustParse(t, src)
	if err := first.run(one); err != nil {
		t.Fatalf("%s: %s on %q: %v", name, first, src, err)
	}
	between := string(one.Bytes())
	again := mustParse(t, between)
	errOne, errAgain := second.run(one), second.run(again)
	if (errOne == nil) != (errAgain == nil) || string(one.Bytes()) != string(again.Bytes()) {
		t.Errorf("%s: %q\n after %s: %q\n then %s\n  on the same document: %q (%v)\n  on its text, parsed:  %q (%v)",
			name, src, first, between, second, one.Bytes(), errOne, again.Bytes(), errAgain)
	}
}

// G2: the edit means exactly what was asked, to the last digit.
func TestG2ExactValue(t *testing.T) {
	runCases(t, g2Cases, styles...)

	// Numbers are compared digit by digit, not as float64.
	d := mustParse(t, "{\n  \"n\": 12345678901234567890,\n  \"big\": 1e400\n}\n")
	set(t, d, uint64(12345678901234567891), "n")
	if got, want := string(d.Bytes()), "{\n  \"n\": 12345678901234567891,\n  \"big\": 1e400\n}\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// g2Cases are the cases of the test above.
var g2Cases = []editCase{
	{"another kind of value", `{
  "a": [1, 2],
  "b": {"x": null}
}
`, setCall("s", "b"), `{
  "a": [1, 2],
  "b": "s"
}
`, ""},
	{"null is a value", `{
  "a": 1
}
`, setCall(nil, "b", "c"), `{
  "a": 1,
  "b": {
    "c": null
  }
}
`, ""},
	{"the first dropped is replaced, the rest appended", `{
  "l": [
    {"u": "x", "n": 1},
    {"u": "o"}
  ]
}
`, oursX(uxn(5), uxn(6)), `{
  "l": [
    {
      "u": "x",
      "n": 5
    },
    {"u": "o"},
    {
      "u": "x",
      "n": 6
    }
  ]
}
`, ""},
	{"more dropped than added", `{
  "l": [
    {"u": "x", "n": 1},
    {"u": "o"},
    {"u": "x", "n": 2},
    {"u": "x", "n": 3}
  ]
}
`, oursX(uxn(9)), `{
  "l": [
    {
      "u": "x",
      "n": 9
    },
    {"u": "o"}
  ]
}
`, ""},
	{"an equal one is left alone next to a replaced one", `{
  "l": [
    {"n": 1, "u": "x"},
    {"u": "x", "n": 2}
  ]
}
`, oursX(uxn(1), uxn(7)), `{
  "l": [
    {"n": 1, "u": "x"},
    {
      "u": "x",
      "n": 7
    }
  ]
}
`, ""},
}

// G3: no comment is lost, wherever it stands.
func TestG3CommentsStay(t *testing.T) {
	runCases(t, g3Cases, styles...)

	runCases(t, g3OneLineCases, asWritten)
}

// g3OneLineCases are the cases of the test above.
var g3OneLineCases = []editCase{
	{"inside a replaced value on one line", `{"a": {/* c */ "x": 1}, "b": 2}`,
		setCall(5, "a"), `{/* c */ "a": 5, "b": 2}`, ""},
	{"inside a removed element on one line", `{"l": [1, {"u": "x" /* in */}, 3]}`,
		oursX(), `{"l": [1, /* in */ 3]}`, ""},
	{"inside a removed last element on one line", `{"l": [1, {"u": "x" /* in */}]}`,
		oursX(), `{"l": [1 /* in */ ]}`, ""},
	{"right in front of a removed element", `{"l": [1, /* two */2, 3]}`,
		dropNum(2), `{"l": [1, /* two */ 3]}`, ""},
	{"in front of a removed element that ends its line", "{\"l\": [1, /* two */ 2,\n  3]}",
		dropNum(2), "{\"l\": [1, /* two */\n  3]}", ""},
	{"behind a removed element in the middle of a line", "{\"l\": [1, 2,\n  3]}",
		dropNum(2), "{\"l\": [1,\n  3]}", ""},
}

// g3Cases are the cases of the test above.
var g3Cases = []editCase{
	{"inside a replaced value: in front of the member", `{
  "p": {
    // note
    "name": "old" /* inline */
  },
  "z": 1
}
`, setCall(Object{{"name", "new"}}, "p"), `{
  // note
  /* inline */
  "p": {
    "name": "new"
  },
  "z": 1
}
`, ""},
	{"inside a replaced element: in front of the element", `{
  "l": [
    {"u": "o"},
    {
      "u": "x" // ours
    }
  ]
}
`, oursX(uxn(2)), `{
  "l": [
    {"u": "o"},
    // ours
    {
      "u": "x",
      "n": 2
    }
  ]
}
`, ""},
	{"inside a removed element: where it was", `{
  "l": [
    {"u": "o"},
    {
      "u": "x" // ours
    }
  ]
}
`, oursX(), `{
  "l": [
    {"u": "o"}
    // ours
  ]
}
`, ""},
	{"above and beside a removed element", `{
  "l": [
    1,
    // above two
    2, // beside two
    3
  ]
}
`, dropNum(2), `{
  "l": [
    1,
    // above two
    // beside two
    3
  ]
}
`, ""},
	{"between a removed element and its comma", `{
  "l": [
    1,
    2 /* two */,
    3
  ]
}
`, dropNum(2), `{
  "l": [
    1,
    /* two */
    3
  ]
}
`, ""},
	{"above a removed last element", `{
  "l": [
    1,
    // above two
    2
  ]
}
`, dropNum(2), `{
  "l": [
    1
    // above two
  ]
}
`, ""},
	{"around a name, a colon and a value", `{
  "a" /* k */ : /* v */ 1 /* after */,
  "b": 2
}
`, setCall(3, "a"), `{
  "a" /* k */ : /* v */ 3 /* after */,
  "b": 2
}
`, ""},
}

// G4: a trailing comma is kept where there was one and appears nowhere else.
func TestG4TrailingCommas(t *testing.T) {
	runCases(t, g4Cases, styles...)

	runCases(t, g4OneLineCases, asWritten)

	if d := mustParse(t, "{\n  \"a\": [1, 2]\n}\n"); d.TrailingComma() {
		t.Error("TrailingComma = true for a strict document")
	}
	if d := mustParse(t, "{\n  \"a\": {\"b\": [1,]}\n}\n"); !d.TrailingComma() {
		t.Error("TrailingComma = false for a comma deep inside")
	}
}

// g4OneLineCases are the cases of the test above.
var g4OneLineCases = []editCase{
	{"one line, the last element goes", `{"l": [1, 2,]}`, dropNum(2), `{"l": [1,]}`, ""},
	{"one line, a new member", `{"a": 1, /* c */}`, setCall(2, "b"), `{"a": 1, /* c */ "b": 2,}`, ""},
}

// g4Cases are the cases of the test above.
var g4Cases = []editCase{
	{"a new member takes it, a new object has none", `{
  "a": 1,
}
`, setCall(Object{{"k", 1}}, "b"), `{
  "a": 1,
  "b": {
    "k": 1
  },
}
`, ""},
	{"a new member behind a comment takes it", `{
  "a": 1, // one
}
`, setCall(2, "b"), `{
  "a": 1, // one
  "b": 2,
}
`, ""},
	{"new elements take it", `{
  "l": [
    1,
  ],
}
`, appendCall(2, 3), `{
  "l": [
    1,
    2,
    3,
  ],
}
`, ""},
	{"a replaced element keeps it", `{
  "l": [
    {"u": "x"},
  ]
}
`, oursX(uxn(2)), `{
  "l": [
    {
      "u": "x",
      "n": 2
    },
  ]
}
`, ""},
	{"the element in front of a removed last one takes it", `{
  "l": [
    1,
    2,
  ]
}
`, dropNum(2), `{
  "l": [
    1,
  ]
}
`, ""},
	{"none appears when the last element goes", `{
  "l": [
    1,
    2,
    3
  ]
}
`, dropNum(3), `{
  "l": [
    1,
    2
  ]
}
`, ""},
	{"a list left empty cannot have one", `{
  "l": [
    2,
  ],
  "z": 0
}
`, dropNum(2), `{
  "l": [],
  "z": 0
}
`, ""},
}

// G5: nothing changes where nothing was asked.
func TestG5NothingElseChanges(t *testing.T) {
	runCases(t, g5Cases, styles...)

	runCases(t, g5OneLineCases, asWritten)
}

// A container that holds nothing but spaces has no layout of its own: it
// is laid out like one with nothing between its brackets. The spaces go
// when it opens up, which is the only text an insertion takes away.
func TestHollowContainersAreLaidOutLikeEmptyOnes(t *testing.T) {
	for _, tc := range []struct {
		hollow, empty string
		call          call
	}{
		{"{ }", "{}", setCall(Object{{"k", 1}}, "a")},
		{"{\t }\n", "{}\n", setCall(1, "a")},
		{"// mine\n{  }", "// mine\n{}", setCall([]int{1, 2}, "a", "b")},
		{"{\"l\": [ ]}", "{\"l\": []}", appendCall(1, 2)},
		{"{\n  \"l\": [  ],\n  \"z\": 0\n}\n", "{\n  \"l\": [],\n  \"z\": 0\n}\n", appendCall(uxn(1))},
		{"{\n\t\"o\": { }\n}\r\n", "{\n\t\"o\": {}\n}\r\n", setCall(1, "o", "a")},
		{"{\"o\": { }}", "{\"o\": {}}", setCall(1, "o", "a")},
		{"{\"l\": [ ]}", "{\"l\": []}", setCall(1, "new")},
	} {
		got, want := verify(t, tc.hollow, tc.call, ""), verify(t, tc.empty, tc.call, "")
		// Inside a container that is written on one line the padding stays.
		if strings.Contains(got, "{ \"a\": 1 }") {
			got = strings.Replace(got, "{ \"a\": 1 }", "{\"a\": 1}", 1)
		}
		if tc.call.path[0] == "new" {
			got = strings.Replace(got, "[ ]", "[]", 1) // the list itself was not touched
		}
		if got != want {
			t.Errorf("%s\n on %q: %q\n on %q: %q", tc.call, tc.hollow, got, tc.empty, want)
		}
		verify(t, verify(t, tc.hollow, tc.call, ""), tc.call, "")
	}
}

// g5OneLineCases are the cases of the test above.
var g5OneLineCases = []editCase{
	{"a one-line object stays on one line", `{"a": 1}`,
		setCall(Object{{"k", []int{1, 2}}, {"e", Object{}}}, "b"), `{"a": 1, "b": {"k": [1, 2], "e": {}}}`, ""},
	{"a one-line object with padding", `{ "a": 1 }`, setCall(2, "b"), `{ "a": 1, "b": 2 }`, ""},
	{"an object with nothing but a space in it has no line to stay on", `{ }`, setCall(2, "b"), "{\n  \"b\": 2\n}", ""},
	{"inside a one-line object it stays on that line, padding and all", `{"a": { }, "z": 0}`, setCall(2, "a", "b"), `{"a": { "b": 2 }, "z": 0}`, ""},
	{"a comment in it makes it a one-line object", `{ /* mine */ }`, setCall(2, "b"), `{ /* mine */ "b": 2 }`, ""},
	{"a value in a one-line object", `{"a": {"x": 1}, "b": 2}`,
		setCall(Object{{"y", 2}}, "a"), `{"a": {"y": 2}, "b": 2}`, ""},
	{"an element in a one-line list", `{"l": [{"u": "o"}, {"u": "x"}]}`,
		oursX(uxn(2)), `{"l": [{"u": "o"}, {"u": "x", "n": 2}]}`, ""},
}

// g5Cases are the cases of the test above.
var g5Cases = []editCase{
	{"an equal object in another layout and order", `{
  "a": {"y": [1, 2,], "x": 1.0} // mine
}
`, setCall(Object{{"x", 1}, {"y", []int{1, 2}}}, "a"), same, ""},
	{"an equal string", `{
  "a": "s" // mine
}
`, setCall("s", "a"), same, ""},
	{"an equal null", `{
  "a": null
}
`, setCall(nil, "a"), same, ""},
	{"the same list", `{
  "l": [1, {"u": "x"} /* ours */, 3]
}
`, oursX(ux), same, ""},
	{"nothing to drop and nothing to add", `{
  "l": [1, 2,]
}
`, appendCall(), same, ""},
	{"only the written value changes", `// header
{
      "odd":1 ,   // strange spacing
  "keep" : [ 1,2 ,3 ],

  "a":   "old"   ,
  /* tail */
}
// footer
`, setCall("new", "a"), `// header
{
      "odd":1 ,   // strange spacing
  "keep" : [ 1,2 ,3 ],

  "a":   "new"   ,
  /* tail */
}
// footer
`, ""},
}

// G6: every line break the package writes is the document's own.
func TestG6LineEndings(t *testing.T) {
	// The tables of the other guarantees run with CRLF as well; here are
	// the texts that mix the two.
	for _, tc := range []struct{ name, src, want string }{
		{"CRLF first", "{\r\n  \"a\": 1\n}\n", "{\r\n  \"a\": 1,\n  \"b\": {\r\n    \"k\": 1\r\n  }\r\n}\n"},
		{"LF first", "{\n  \"a\": 1\r\n}\r\n", "{\n  \"a\": 1,\r\n  \"b\": {\n    \"k\": 1\n  }\n}\r\n"},
		{"no line break", `{"a": {}}` + "\r\n", `{"a": {}, "b": {"k": 1}}` + "\r\n"},
		{"a stray carriage return is no line break", "{\r  \"a\": 1}", "{\r  \"a\": 1, \"b\": {\"k\": 1}}"},
	} {
		d := mustParse(t, tc.src)
		set(t, d, Object{{"k", 1}}, "b")
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	d := New()
	set(t, d, Object{{"k", 1}}, "b")
	if got := string(d.Bytes()); strings.Contains(got, "\r") {
		t.Errorf("a new document has a carriage return: %q", got)
	}
}

// twice is a document in which the key x occurs twice.
const twice = `{
  "x": 1,
  "x": 2,
  "a": 1
}
`

// G7: a key that occurs twice on the path is refused; Get reads the last.
func TestG7DuplicateKeys(t *testing.T) {
	runCases(t, g7Cases, styles...)

	d := mustParse(t, twice)
	if v, ok := d.Get("x"); !ok || v != 2.0 {
		t.Errorf("Get(x) = %v, %v; want the last one", v, ok)
	}
	if v, ok := d.Get(); !ok || v.(map[string]any)["x"] != 2.0 {
		t.Errorf("Get() = %v, %v", v, ok)
	}
}

// g7Cases are the cases of the test above.
var g7Cases = []editCase{
	{"deeper on the path", `{
  "a": {"b": 1, "b": 2}
}
`, setCall(1, "a", "b"), same, "a.b appears more than once"},
	{"a parent of the list", `{
  "p": {"l": [1]},
  "p": {"l": [2]}
}
`, updateCall("nothing", nil, []any{3}, "p", "l"), same, "p appears more than once"},
	{"written in two ways", `{
  "ab": 1,
  "ab": 2
}
`, setCall(3, "ab"), same, "ab appears more than once"},
	{"a parent of a new key", `{
  "p": {},
  "p": {}
}
`, setCall(1, "p", "new"), same, "p appears more than once"},
	{"off the path it is not in the way", twice, setCall(3, "a"), `{
  "x": 1,
  "x": 2,
  "a": 3
}
`, ""},
	{"nor for a new key", twice, setCall(3, "n"), `{
  "x": 1,
  "x": 2,
  "a": 1,
  "n": 3
}
`, ""},
}

// G8: a byte-order mark is accepted and written back.
func TestG8ByteOrderMark(t *testing.T) {
	runCases(t, []editCase{
		{"a document", bom + "{\n  \"a\": 1\n}\n", setCall(1, "a"), same, ""},
		{"a new member", bom + "{\n  \"a\": 1\n}\n", setCall(Object{{"k", 1}}, "b"),
			bom + "{\n  \"a\": 1,\n  \"b\": {\n    \"k\": 1\n  }\n}\n", ""},
		{"nothing but the mark", bom, setCall(1, "a"), bom + "{\n  \"a\": 1\n}\n", ""},
		{"the mark and a comment", bom + "// c\r\n", setCall(1, "a"), bom + "// c\r\n{\r\n  \"a\": 1\r\n}\r\n", ""},
	}, asWritten)

	d := mustParse(t, bom+"{\n  \"a\": 1\n}\n")
	if v, ok := d.Get("a"); !ok || v != 1.0 {
		t.Errorf("Get(a) = %v, %v", v, ok)
	}
	if got := string(New().Bytes()); strings.HasPrefix(got, bom) {
		t.Errorf("a new document has a byte-order mark: %q", got)
	}
	// Only a mark at the very start is one.
	if _, err := Parse([]byte("{}" + bom)); err == nil {
		t.Error("a byte-order mark after the value was accepted")
	}
}

// G9: a text without a value is a document; the first Set gives it one.
func TestG9NoValue(t *testing.T) {
	const obj = "{\n  \"a\": 1\n}\n"
	for src, want := range map[string]string{
		"":                  obj,
		"\n":                "\n" + obj,
		"// c\n":            "// c\n" + obj,
		"/* c */":           "/* c */\n" + obj,
		"// cut short":      "// cut short\n" + obj,
		"  ":                "  \n" + obj,
		"// c\r\n":          "// c\r\n{\r\n  \"a\": 1\r\n}\r\n",
		"/* a\n   b */\n\n": "/* a\n   b */\n\n" + obj,
	} {
		d := mustParse(t, src)
		if got := string(d.Bytes()); got != src {
			t.Errorf("Bytes of %q = %q", src, got)
		}
		if _, ok := d.Get(); ok || d.TrailingComma() {
			t.Errorf("%q: Get finds a value, or TrailingComma is true", src)
		}
		if _, ok := d.Get("a"); ok {
			t.Errorf("%q: Get(a) finds a value", src)
		}
		// Nothing to add, an error: still no value.
		if err := d.UpdateArray(nil, nil, "l"); err != nil || string(d.Bytes()) != src {
			t.Errorf("%q: an empty UpdateArray = %v, text %q", src, err, d.Bytes())
		}
		if err := d.Set(make(chan int), "a"); err == nil || string(d.Bytes()) != src {
			t.Errorf("%q: a Set that cannot work = %v, text %q", src, err, d.Bytes())
		}
		editCase{src, src, setCall(1, "a"), want, ""}.run(t, asWritten)
	}
	editCase{"a list", "// c\n", appendCall(1), "// c\n{\n  \"l\": [\n    1\n  ]\n}\n", ""}.run(t, asWritten)

	// Any other text that does not parse is refused, with its position.
	for src, want := range map[string]string{
		"{":                              "line 1, column 2",
		"{\n  \"a\": 1,\n  \"b\": \n}\n": "line 4, column 1",
		"/* open":                        "line 1, column 1",
		"{\"a\": 1} x":                   "line 1, column 10",
		"{\"a\": 1 // cut short":         "line 1, column 9",
		"[1]":                            "the top-level value is not an object",
		"1":                              "the top-level value is not an object",
	} {
		if d, err := Parse([]byte(src)); d != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, %v; want an error with %q", src, d, err, want)
		}
	}
}

// G10: a removal never creates anything.
func TestG10RemovalCreatesNothing(t *testing.T) {
	const src = `{
  "a": 1,
  "p": {}
}
`
	nothing := func(add []any, path ...string) call { return updateCall(`u=="x"`, isU("x"), add, path...) }
	runCases(t, []editCase{
		{"no list", src, nothing(nil, "l"), same, ""},
		{"no parent", src, nothing(nil, "q", "r", "l"), same, ""},
		{"an empty parent", src, nothing(nil, "p", "l"), same, ""},
		{"an empty list to add", src, nothing([]any{}, "p", "l"), same, ""},
		{"no drop either", src, updateCall("nothing", nil, nil, "p", "l"), same, ""},
		{"something to add creates the list", src, nothing([]any{ux}, "p", "l"), `{
  "a": 1,
  "p": {
    "l": [
      {
        "u": "x"
      }
    ]
  }
}
`, ""},
		{"a parent of another kind is still an error", src, nothing(nil, "a", "l"), same, "a is not an object"},
		{"so is a value of another kind", src, nothing(nil, "a"), same, "a is not a list"},
	}, styles...)
}

// endsInComment is a document that ends inside a line comment: no line
// break follows its last byte.
const endsInComment = `{
  "l": [
    1,
    {"u": "x"}
  ],
  "a": 1
} // the end`

// G11: a text may end inside a line comment. It stays the end of the text.
func TestG11EndsInsideALineComment(t *testing.T) {
	runCases(t, g11Cases, styles...)
	runCases(t, g11NoValueCases, styles[:2]...) // nothing to copy an indentation from
	runCases(t, g11OneLineCases, asWritten)

	for _, s := range styles {
		src := s.in(endsInComment)
		d := mustParse(t, src)
		if got := string(d.Bytes()); got != src {
			t.Errorf("%s: Bytes = %q, want %q", s.name, got, src)
		}
		if v, ok := d.Get("a"); !ok || v != 1.0 || d.TrailingComma() {
			t.Errorf("%s: Get(a) = %v, %v", s.name, v, ok)
		}
	}
	// Inside a value the text may not end.
	if d, err := Parse([]byte("{\n  \"a\": 1 // one")); d != nil || err == nil || !strings.Contains(err.Error(), "line 2, column 10") {
		t.Errorf("Parse of an object that is not closed = %v, %v", d, err)
	}
}

// g11Cases are the cases of the test above.
var g11Cases = []editCase{
	{"an equal value", endsInComment, setCall(1, "a"), same, ""},
	{"an existing key", endsInComment, setCall(2, "a"), `{
  "l": [
    1,
    {"u": "x"}
  ],
  "a": 2
} // the end`, ""},
	{"a new key", endsInComment, setCall(Object{{"k", 3}}, "b"), `{
  "l": [
    1,
    {"u": "x"}
  ],
  "a": 1,
  "b": {
    "k": 3
  }
} // the end`, ""},
	{"an element replaced", endsInComment, oursX(uxn(2)), `{
  "l": [
    1,
    {
      "u": "x",
      "n": 2
    }
  ],
  "a": 1
} // the end`, ""},
	{"an element appended", endsInComment, appendCall(3), `{
  "l": [
    1,
    {"u": "x"},
    3
  ],
  "a": 1
} // the end`, ""},
	{"an element removed", endsInComment, oursX(), `{
  "l": [
    1
  ],
  "a": 1
} // the end`, ""},
	{"a duplicate key is still refused", `{
  "a": 1,
  "a": 2
} // the end`, setCall(3, "a"), same, "a appears more than once"},
	{"the comment on a line of its own", `{
  "a": 1 // one
}
// the end`, setCall(2, "b"), `{
  "a": 1, // one
  "b": 2
}
// the end`, ""},
}

// g11NoValueCases: something is put behind the comment, so it gets the
// document's line break.
var g11NoValueCases = []editCase{
	{"no value, two comments", `// one
// two`, setCall(1, "a"), `// one
// two
{
  "a": 1
}
`, ""},
	{"no value, a block comment and a line comment", `/* one
   more */ // two`, appendCall(1), `/* one
   more */ // two
{
  "l": [
    1
  ]
}
`, ""},
}

var g11OneLineCases = []editCase{
	{"on one line", `{"a": 1} // note`, setCall(2, "b"), `{"a": 1, "b": 2} // note`, ""},
	{"right behind the brace", `{"a": 1}//`, setCall(2, "a"), `{"a": 2}//`, ""},
	{"with a byte-order mark", bom + `{"a": 1} // note`, setCall(2, "a"), bom + `{"a": 2} // note`, ""},
	{"with a carriage return at the end", "{\"a\": 1} // note\r", setCall(2, "a"), "{\"a\": 2} // note\r", ""},
	{"no value, one comment", `// only this`, setCall(1, "a"), "// only this\n{\n  \"a\": 1\n}\n", ""},
	{"no value, nothing asked", `// only this`, updateCall("nothing", nil, nil, "l"), same, ""},
}

// G12: a value with a repeated key is refused.
func TestG12RepeatedKeyInTheValue(t *testing.T) {
	const src = `{
  "x": {"a": 2},
  "l": [
    {"u": "x"}
  ]
}
`
	const tail = " appears more than once in the value to write"
	twoA := Object{{"a", 1}, {"a", 2}}
	oursTwice := Object{{"u", "x"}, {"u", "y"}}
	runCases(t, []editCase{
		{"Set, at the top", src, setCall(twoA, "x"), same, "a" + tail},
		{"Set, in an object", src, setCall(Object{{"p", Object{{"q", twoA}}}}, "x"), same, "p.q.a" + tail},
		{"Set, in a list", src, setCall(Object{{"p", []any{1, twoA}}}, "x"), same, "p.1.a" + tail},
		{"Set, a new key below new parents", src, setCall(twoA, "n", "m"), same, "a" + tail},
		{"Set, raw JSON", src, setCall(json.RawMessage(`{"k": {"a\u0062": 1, "ab": 2}}`), "x"), same, "k.ab" + tail},
		{"UpdateArray, at the top of an element", src, oursX(ux, oursTwice), same, "u" + tail},
		{"UpdateArray, deeper in an element", src, oursX(Object{{"u", "x"}, {"g", twoA}}), same, "g.a" + tail},
		{"UpdateArray, a list to create", src, updateCall("nothing", nil, []any{ux, oursTwice}, "new", "l"), same, "u" + tail},
		{"the same key in two objects is no repeat", src, setCall(Object{{"p", Object{{"a", 1}}}, {"q", Object{{"a", 2}}}}, "x"), `{
  "x": {
    "p": {
      "a": 1
    },
    "q": {
      "a": 2
    }
  },
  "l": [
    {"u": "x"}
  ]
}
`, ""},
	}, styles...)

	// A text without a value stays without one.
	d := mustParse(t, "// c\n")
	if err := d.Set(twoA, "x"); err == nil || err.Error() != "a"+tail || string(d.Bytes()) != "// c\n" {
		t.Errorf("Set on a text without a value = %v, text %q", err, d.Bytes())
	}
}
