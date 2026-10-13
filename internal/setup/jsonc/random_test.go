package jsonc

import (
	"maps"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// The randomized test writes documents in every layout the generator below
// knows, makes one random edit on each and lets verify check the
// guarantees; then it makes the same edit again. The seed is fixed, so the
// run is the same every time. A failure prints the seed of its case, the
// document and the call, which is all an editCase in a table needs.
const (
	randomSeed  = 20261012
	randomCases = 4000
)

func TestRandomEdits(t *testing.T) {
	for i := range randomCases {
		randomCase(t, randomSeed+int64(i))
	}
}

func randomCase(t *testing.T, seed int64) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	src := randomDocument(r)
	defer func() {
		if t.Failed() {
			t.Logf("seed %d, document %q", seed, src)
		}
	}()
	c := randomCall(t, r, src)
	again := verify(t, src, c, "")
	verify(t, again, c, "")
}

// pick returns one of the options.
func pick[T any](r *rand.Rand, options ...T) T { return options[r.Intn(len(options))] }

// writer writes a random document as text.
type writer struct {
	*rand.Rand
	strings.Builder
	eol  string
	unit string
}

func (w *writer) chance(percent int) bool { return w.Intn(100) < percent }

func randomDocument(r *rand.Rand) string {
	w := &writer{Rand: r, eol: pick(r, "\n", "\n", "\r\n"), unit: pick(r, "  ", "  ", "    ", "\t")}
	if w.chance(8) {
		w.WriteString(bom)
	}
	w.commentLines("")
	w.object(0, "")
	switch w.Intn(5) {
	case 0: // the text ends with the closing brace
	case 1:
		w.WriteString(w.eol)
		w.commentLines("")
	case 2: // the text ends inside a line comment
		w.WriteString(pick(w.Rand, " ", "", w.eol) + w.comment(false))
	default:
		w.WriteString(w.endOfLine())
	}
	return w.String()
}

// The keys and scalars a document is made of, as they are written in it:
// plain ones, ones that need escaping and ones that look like comments.
var (
	keyTexts = []string{`"a"`, `"b"`, `"c"`, `"d"`, `"l"`, `"we\"ird"`, `"back\\slash"`, `"new\nline"`,
		`"ş<ğ>&"`, `"uni"`, `""`, `"dot.ted"`, `"sp ace"`}
	scalarTexts = []string{`"s"`, `""`, `"\"quoted\" \\ and\nmore"`, `"ş<ğ>&"`, `"é"`, `"// no comment"`,
		`"/* nor this */"`, `0`, `1`, `-7`, `2.5`, `1.0`, `1e3`, `-2.5E-3`, `true`, `false`, `null`}
	commentWords = []string{"note", "TODO", `"quoted"`, "]", "}", ",", "//", "/*", `"key": 1,`, "ş"}
)

// comment returns a block comment, sometimes over two lines, or a line
// comment without its line break.
func (w *writer) comment(block bool) string {
	text := pick(w.Rand, commentWords...) + " " + pick(w.Rand, commentWords...)
	switch {
	case !block:
		return "// " + text
	case w.chance(25):
		return "/* " + text + w.eol + "   " + pick(w.Rand, commentWords...) + " */"
	}
	return "/* " + text + " */"
}

// gap returns what stands between two tokens: mostly nothing, sometimes a
// block comment, now and then a line comment, which breaks the line.
func (w *writer) gap() string {
	switch n := w.Intn(100); {
	case n < 12:
		return pick(w.Rand, "", " ") + w.comment(true) + pick(w.Rand, "", " ")
	case n < 15:
		return pick(w.Rand, "", " ") + w.comment(false) + w.eol + pick(w.Rand, "", " ", w.unit, w.unit+w.unit)
	}
	return ""
}

// endOfLine returns a line break, sometimes with a comment in front of it.
func (w *writer) endOfLine() string {
	switch n := w.Intn(100); {
	case n < 20:
		return " " + w.comment(false) + w.eol
	case n < 26:
		return " " + w.comment(true) + w.eol
	}
	return w.eol
}

// commentLines writes up to two lines that hold a comment or nothing.
func (w *writer) commentLines(indent string) {
	for range 2 {
		switch n := w.Intn(100); {
		case n < 12:
			w.WriteString(indent + w.comment(w.chance(40)) + w.eol)
		case n < 16:
			w.WriteString(w.eol)
		}
	}
}

func (w *writer) object(depth int, indent string) {
	keys := w.Perm(len(keyTexts))[:w.Intn(4)]
	w.container("{", "}", len(keys), indent, func(i int, indent string) {
		w.WriteString(keyTexts[keys[i]] + w.gap() + ":" + pick(w.Rand, " ", " ", "") + w.gap())
		w.value(depth+1, indent)
	})
}

// array writes a list. Some of its elements are entries that isU("x")
// takes for ours, which is what the random UpdateArray drops.
func (w *writer) array(depth int, indent string) {
	w.container("[", "]", w.Intn(4), indent, func(_ int, indent string) {
		if !w.chance(40) {
			w.value(depth+1, indent)
			return
		}
		w.WriteString(pick(w.Rand, `{"u": "x"}`, `{"u": "x", "n": 1}`, `{ "n": 2, "u": "x" }`,
			`{"u": "x" /* ours */}`, `{"u": "x", "n": [1, 2,],}`,
			"{"+w.eol+indent+w.unit+`"u": "x" // ours`+w.eol+indent+"}"))
	})
}

func (w *writer) value(depth int, indent string) {
	switch n := w.Intn(10); {
	case n < 2 && depth < 4:
		w.object(depth, indent)
	case n < 4 && depth < 4:
		w.array(depth, indent)
	default:
		w.WriteString(pick(w.Rand, scalarTexts...))
	}
}

// container writes the brackets of an object or an array and, on one line
// or on several, its n items; item writes one, and is told the indentation
// of the line it is on.
func (w *writer) container(open, close string, n int, indent string, item func(i int, indent string)) {
	w.WriteString(open)
	if w.chance(35) {
		w.oneLine(n, indent, item)
	} else {
		w.manyLines(n, indent, item)
	}
	w.WriteString(close)
}

func (w *writer) oneLine(n int, indent string, item func(i int, indent string)) {
	pad := pick(w.Rand, "", "", " ")
	if n == 0 {
		if w.chance(30) {
			w.WriteString(pad + w.gap() + pad)
		}
		return
	}
	w.WriteString(pad)
	for i := range n {
		if i > 0 {
			w.WriteString("," + pick(w.Rand, " ", " ", ""))
		}
		w.WriteString(w.gap())
		item(i, indent)
		w.WriteString(w.gap())
	}
	if w.chance(20) {
		w.WriteString("," + w.gap())
	}
	w.WriteString(pad)
}

func (w *writer) manyLines(n int, indent string, item func(i int, indent string)) {
	child := indent + w.unit + pick(w.Rand, "", "", "", "", "", " ", w.unit) // not always one unit deeper
	w.WriteString(w.endOfLine())
	trailingComma := w.chance(25)
	atLineStart := true
	for i := range n {
		if atLineStart {
			w.commentLines(child)
			w.WriteString(child)
		}
		w.WriteString(w.gap())
		item(i, child)
		w.WriteString(w.gap())
		if i < n-1 || trailingComma {
			w.WriteString(",")
		}
		switch {
		case i < n-1 && w.chance(10): // the next item on the same line
			w.WriteString(" ")
			atLineStart = false
		case i == n-1 && w.chance(8): // the closing bracket on the last item's line
			return
		default:
			w.WriteString(w.endOfLine())
			atLineStart = true
		}
	}
	w.commentLines(child)
	w.WriteString(indent + w.gap()) // the closing line, sometimes with a comment on it
}

// place is a value that a path of object keys leads to.
type place struct {
	path  []string
	value any
}

// places lists the values of a decoded document that a path of object keys
// reaches, the document itself first, in a fixed order.
func places(v any, path []string, found *[]place) {
	*found = append(*found, place{path, v})
	if m, ok := v.(map[string]any); ok {
		for _, key := range slices.Sorted(maps.Keys(m)) {
			places(m[key], append(slices.Clone(path), key), found)
		}
	}
}

// randomCall picks an edit for the document src: mostly one that has
// something to do, sometimes one that must change nothing or fail.
func randomCall(t *testing.T, r *rand.Rand, src string) call {
	t.Helper()
	_, doc := mustRead(t, strings.TrimPrefix(src, bom))
	var all, objects, lists []place
	places(doc, nil, &all)
	for _, p := range all {
		switch p.value.(type) {
		case map[string]any:
			objects = append(objects, p)
		case []any:
			lists = append(lists, p)
		}
	}
	inner := all[1:] // every place but the document itself
	switch n := r.Intn(20); {
	case n < 4 && len(inner) > 0: // another value over an existing one
		return setCall(randomValue(r, 0), pick(r, inner...).path...)
	case n < 6 && len(inner) > 0: // the value that is there already
		p := pick(r, inner...)
		return setCall(p.value, p.path...)
	case n < 7 && len(inner) > 0: // through whatever is there: an error unless it is an object
		return setCall(randomValue(r, 0), append(slices.Clone(pick(r, inner...).path), "k")...)
	case n < 11: // a new key, sometimes below parents that do not exist either
		return setCall(randomValue(r, 0), newPath(r, pick(r, objects...))...)
	case n < 17 && len(lists) > 0: // the list of a document
		p := pick(r, lists...)
		return randomUpdate(r, p.value.([]any), p.path)
	case n < 19: // a list that does not exist: created, or left alone when there is nothing to add
		return randomUpdate(r, nil, newPath(r, pick(r, objects...)))
	}
	return randomUpdate(r, nil, pick(r, all...).path) // most often not a list: an error
}

// newPath returns a path to a key that the object at p does not have.
func newPath(r *rand.Rand, p place) []string {
	keys := []string{"new", "we\"ird\\k\ney", "ş<ğ>&", "", "k2", "zz"}
	obj := p.value.(map[string]any)
	key := pick(r, keys...)
	for _, taken := obj[key]; taken; _, taken = obj[key] {
		key += "'"
	}
	path := append(slices.Clone(p.path), key)
	for r.Intn(3) == 0 {
		path = append(path, pick(r, keys...))
	}
	return path
}

// randomUpdate picks an UpdateArray at path, where elems is the list there
// now, if there is one. It mostly drops our entries and adds entries of
// ours, new ones and ones that are in the list already.
func randomUpdate(r *rand.Rand, elems []any, path []string) call {
	if len(path) == 0 {
		path = []string{"l"}
	}
	var ours []any
	for _, e := range elems {
		if isU("x")(e) {
			ours = append(ours, e)
		}
	}
	var add []any
	for range r.Intn(4) {
		switch n := r.Intn(10); {
		case n < 3 && len(ours) > 0:
			add = append(add, pick(r, ours...))
		case n < 9:
			add = append(add, uxn(r.Intn(4)))
		default:
			add = append(add, randomValue(r, 1))
		}
	}
	if r.Intn(100) < 15 {
		return updateCall("nothing", nil, add, path...)
	}
	return updateCall(`u=="x"`, isU("x"), add, path...)
}

// randomValue returns a value of any JSON kind for Set to write.
func randomValue(r *rand.Rand, depth int) any {
	switch n := r.Intn(12); {
	case n < 2 && depth < 2:
		obj := Object{}
		for _, i := range r.Perm(5)[:r.Intn(4)] {
			obj = append(obj, Member{[]string{"a", "k", "we\"ird", "ş<ğ>&", ""}[i], randomValue(r, depth+1)})
		}
		return obj
	case n < 4 && depth < 2:
		list := []any{}
		for range r.Intn(4) {
			list = append(list, randomValue(r, depth+1))
		}
		return list
	}
	return pick[any](r, "s", "", "<a&b>\n\"q\"", 0, 1, -7, 2.5, 1e21, true, false, nil)
}
