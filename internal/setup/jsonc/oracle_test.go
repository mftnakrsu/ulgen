package jsonc

// The tests' own measure of what an edit may do. Nothing in this file uses
// the package's internals: the text is read by a scanner written here, the
// expected value is worked out on the decoded document, and hujson is only
// asked whether a text parses and where a value starts and ends.

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

const bom = "\xef\xbb\xbf"

// call is one edit, kept as data: a test can run it, print it and work out
// from the decoded document what it has to do.
type call struct {
	path     []string
	update   bool           // UpdateArray rather than Set
	value    any            // what Set writes
	drop     func(any) bool // what UpdateArray drops
	dropName string         // how to print drop
	add      []any          // what UpdateArray adds
}

func setCall(value any, path ...string) call { return call{path: path, value: value} }

func updateCall(dropName string, drop func(any) bool, add []any, path ...string) call {
	return call{path: path, update: true, drop: drop, dropName: dropName, add: add}
}

func (c call) run(d *Doc) error {
	if c.update {
		return d.UpdateArray(c.drop, c.add, c.path...)
	}
	return d.Set(c.value, c.path...)
}

func (c call) String() string {
	if c.update {
		return fmt.Sprintf("UpdateArray(%s, %s, %q)", c.dropName, asText(c.add), c.path)
	}
	return fmt.Sprintf("Set(%s, %q)", asText(c.value), c.path)
}

// asText is v as JSON, for messages.
func asText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(b)
}

// plainValue is v as encoding/json decodes its JSON form.
func plainValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

// plainAdd is what an UpdateArray adds, decoded.
func (c call) plainAdd(t *testing.T) []any {
	t.Helper()
	add, _ := plainValue(t, c.add).([]any)
	return add
}

// scanned is what strip reads from a text.
type scanned struct {
	plain    []byte   // the text as plain JSON: comments blanked, trailing commas removed
	comments []string // every comment, a line comment without its line break
	commas   int      // how many objects and arrays end with a comma
	cut      bool     // the text ends inside a line comment
}

// strip reads JSON with comments. ok is false when a string or a block
// comment is not closed.
func strip(src string) (s scanned, ok bool) {
	s.plain = []byte(src)
	comma := -1 // a comma with nothing but whitespace and comments after it so far
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '"':
			end := stringEnd(src, i)
			if end < 0 {
				return s, false
			}
			i, comma = end, -1
		case strings.HasPrefix(src[i:], "//"), strings.HasPrefix(src[i:], "/*"):
			end := commentEnd(src, i)
			if end < 0 {
				return s, false
			}
			s.comments = append(s.comments, strings.TrimSuffix(src[i:end], "\r"))
			s.cut = end == len(src) && src[i+1] == '/'
			for ; i < end; i++ {
				if src[i] != '\n' && src[i] != '\r' {
					s.plain[i] = ' '
				}
			}
		case c == ',':
			comma = i
			i++
		case c == '}' || c == ']':
			if comma >= 0 {
				s.plain[comma] = ' '
				s.commas++
			}
			comma = -1
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		default:
			comma = -1
			i++
		}
	}
	return s, true
}

// stringEnd returns the offset after the string that starts at src[i], or -1.
func stringEnd(src string, i int) int {
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return -1
}

// commentEnd returns the offset after the comment that starts at src[i] (a
// line comment ends in front of its line break, or with the text), or -1.
func commentEnd(src string, i int) int {
	if src[i+1] == '/' {
		if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
			return i + j
		}
		return len(src)
	}
	if j := strings.Index(src[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return -1
}

// tree asks hujson for the syntax tree of a text. hujson wants a line break
// behind every line comment, so a text that ends inside one gets it here;
// the offsets in the tree stay those of the text.
func tree(text string) (hujson.Value, error) {
	if s, _ := strip(text); s.cut {
		text += "\n"
	}
	return hujson.Parse([]byte(text))
}

// read strips and decodes a text. A text without a value reads as an empty
// object, which is what an edit starts from there.
func read(src string) (scanned, any, error) {
	s, ok := strip(src)
	if !ok {
		return s, nil, fmt.Errorf("a string or a comment is not closed")
	}
	if strings.TrimSpace(string(s.plain)) == "" {
		return s, map[string]any{}, nil
	}
	var doc any
	if err := json.Unmarshal(s.plain, &doc); err != nil {
		return s, nil, fmt.Errorf("not JSON once the comments are gone: %v", err)
	}
	return s, doc, nil
}

// mustRead is read for a text that is known to be good.
func mustRead(t *testing.T, src string) (scanned, any) {
	t.Helper()
	s, doc, err := read(src)
	if err != nil {
		t.Fatalf("%v\ntext: %q", err, src)
	}
	return s, doc
}

// fate is what an UpdateArray does to one element.
type fate int

const (
	stays fate = iota
	replaced
	removed
)

// arrayModel applies the rule of UpdateArray to a decoded array.
func arrayModel(elems []any, drop func(any) bool, add []any) (out []any, fates []fate) {
	out = []any{}
	dropped := 0
	for _, e := range elems {
		switch {
		case drop == nil || !drop(e):
			out, fates = append(out, e), append(fates, stays)
			continue
		case dropped >= len(add):
			fates = append(fates, removed)
		case reflect.DeepEqual(e, add[dropped]):
			out, fates = append(out, e), append(fates, stays)
		default:
			out, fates = append(out, add[dropped]), append(fates, replaced)
		}
		dropped++
	}
	if dropped < len(add) {
		out = append(out, add[dropped:]...)
	}
	return out, fates
}

// model works out on the decoded document what c has to do: the document
// afterwards, or the text of the error that c must return instead.
func (c call) model(t *testing.T, doc any) (after any, errText string) {
	t.Helper()
	if !c.update {
		value := plainValue(t, c.value)
		return rewrite(doc, c.path, 0, func(any, bool) (any, bool, string) { return value, true, "" })
	}
	add := c.plainAdd(t)
	return rewrite(doc, c.path, 0, func(old any, found bool) (any, bool, string) {
		elems, isList := old.([]any)
		switch {
		case !found:
			return add, len(add) > 0, ""
		case !isList:
			return nil, false, strings.Join(c.path, ".") + " is not a list"
		}
		out, _ := arrayModel(elems, c.drop, add)
		return out, true, ""
	})
}

// rewrite returns the object doc with the value at path[i:] replaced by
// what leaf makes of it. leaf is given the value and whether there is one;
// it returns the new value, whether to write it at all, and an error text.
// A missing parent is created only when leaf writes something.
func rewrite(doc any, path []string, i int, leaf func(old any, found bool) (any, bool, string)) (any, string) {
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, strings.Join(path[:i], ".") + " is not an object"
	}
	old, found := m[path[i]]
	var val any
	var errText string
	write := true
	switch {
	case i == len(path)-1:
		val, write, errText = leaf(old, found)
	case found:
		val, errText = rewrite(old, path, i+1, leaf)
	default:
		val, errText = rewrite(map[string]any{}, path, i+1, leaf)
		if sub, _ := val.(map[string]any); len(sub) == 0 {
			write = false
		}
	}
	if errText != "" || !write {
		return doc, errText
	}
	out := maps.Clone(m)
	out[path[i]] = val
	return out, ""
}

// verify runs c on a document with the text src and checks, from the
// outside, what the package promises about an edit. wantErr, when not
// empty, replaces the model's verdict (the model cannot see a duplicate
// key). It returns the text afterwards.
func verify(t *testing.T, src string, c call, wantErr string) string {
	t.Helper()
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v\ntext: %q", err, src)
	}
	_, doc := mustRead(t, strings.TrimPrefix(src, bom))
	want, modelErr := c.model(t, doc)
	if wantErr == "" {
		wantErr = modelErr
	}
	err = c.run(d)
	out := string(d.Bytes())
	var wrong string
	switch {
	case wantErr != "":
		wrong = refused(src, out, err, wantErr)
	case err != nil:
		wrong = "error = " + err.Error()
	default:
		wrong = broken(t, src, out, c, want)
	}
	if wrong != "" {
		t.Fatalf("%s\ncall:   %s\nbefore: %q\nafter:  %q", wrong, c, src, out)
	}
	return out
}

// refused says what is wrong with a call that had to fail with wantErr.
func refused(src, out string, err error, wantErr string) string {
	switch {
	case err == nil || err.Error() != wantErr:
		return fmt.Sprintf("error = %v, want %q", err, wantErr)
	case out != src:
		return "a call that failed changed the text"
	}
	return ""
}

// broken says which guarantee the text out breaks. It is the result of c
// on src, which had to make the document decode to want.
func broken(t *testing.T, src, out string, c call, want any) string {
	t.Helper()
	body, outBody := strings.TrimPrefix(src, bom), strings.TrimPrefix(out, bom)
	before, beforeDoc := mustRead(t, body)
	after, afterDoc, err := read(outBody)
	if err == nil {
		_, err = Parse([]byte(out))
	}
	if hasValue := strings.TrimSpace(string(after.plain)) != ""; err == nil && hasValue {
		_, err = tree(outBody)
	}
	site, nothingAsked := locate(t, body, c), reflect.DeepEqual(want, beforeDoc)
	switch {
	case err != nil:
		return fmt.Sprintf("G1: the result does not parse: %v", err)
	case !reflect.DeepEqual(afterDoc, want):
		return fmt.Sprintf("G2: the result decodes to\n  %s\nwant\n  %s", asText(afterDoc), asText(want))
	case !slices.Equal(sorted(before.comments), sorted(after.comments)):
		return fmt.Sprintf("G3: comments before %q, after %q", before.comments, after.comments)
	case nothingAsked && out != src:
		return "G5, G10: nothing was asked for, but the text changed"
	case nothingAsked:
		return ""
	case after.commas != before.commas-site.lostCommas(t, c):
		return fmt.Sprintf("G4: %d containers end with a comma, before %d, of which %d went with what was replaced or removed",
			after.commas, before.commas, site.lostCommas(t, c))
	case strings.HasPrefix(out, bom) != strings.HasPrefix(src, bom):
		return "G8: the byte-order mark was not kept"
	}
	if msg := site.untouched(body, outBody); msg != "" {
		return "G5: " + msg
	}
	return lineEndings(body, outBody)
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// lineEndings checks that a text with one kind of line break still has only
// that kind.
func lineEndings(before, after string) string {
	bare := func(s string) int { return strings.Count(s, "\n") - strings.Count(s, "\r\n") }
	switch {
	case !strings.Contains(before, "\r") && strings.Contains(after, "\r"):
		return "G6: a carriage return appeared in a text that had none"
	case strings.Contains(before, "\n") && bare(before) == 0 && bare(after) != 0:
		return "G6: a bare line feed appeared in a text with CRLF line breaks"
	}
	return ""
}

// site is the part of a text that a call writes: the member the path names,
// or, when there is none, the object that will get it.
type site struct {
	text   string
	obj    *hujson.Value        // the last object the path reaches; nil in a text without a value
	member *hujson.ObjectMember // the member the path names in it, or nil
	fates  []fate               // for an UpdateArray on a list: what happens to each element
}

// locate finds the site of c in text.
func locate(t *testing.T, text string, c call) site {
	t.Helper()
	s := site{text: text}
	root, err := tree(text)
	if err != nil {
		return s // a text without a value: all of it may change
	}
	s.obj = &root
	for _, key := range c.path {
		obj := s.obj.Value.(*hujson.Object)
		s.member = nil
		for i := range obj.Members {
			if obj.Members[i].Name.Value.(hujson.Literal).String() == key {
				s.member = &obj.Members[i]
			}
		}
		if s.member == nil {
			return s
		}
		if _, ok := s.member.Value.Value.(*hujson.Object); !ok {
			break
		}
		s.obj = &s.member.Value
	}
	if arr, ok := s.member.Value.Value.(*hujson.Array); ok && c.update {
		var elems []any
		for _, e := range arr.Elements {
			_, v := mustRead(t, text[e.StartOffset:e.EndOffset])
			elems = append(elems, v)
		}
		_, s.fates = arrayModel(elems, c.drop, c.plainAdd(t))
	}
	return s
}

// lostCommas counts the trailing commas that go with the text the call
// replaces or removes: those inside a value that is written over, and that
// of a list that is left empty.
func (s site) lostCommas(t *testing.T, c call) int {
	t.Helper()
	inside := func(v *hujson.Value) int {
		in, _ := mustRead(t, s.text[v.StartOffset:v.EndOffset])
		return in.commas
	}
	switch {
	case s.member == nil:
		return 0
	case s.fates == nil:
		return inside(&s.member.Value)
	}
	arr := s.member.Value.Value.(*hujson.Array)
	lost := 0
	for i, f := range s.fates {
		if f != stays {
			lost += inside(&arr.Elements[i])
		}
	}
	emptied := len(c.add) == 0 && !slices.Contains(s.fates, stays)
	if n := len(arr.Elements); emptied && arr.Elements[n-1].AfterExtra != nil {
		lost++
	}
	return lost
}

// untouched checks that the text outside the site is the same in after,
// that a call which only adds takes nothing away, and that the elements an
// UpdateArray keeps are there as they were. It returns what is wrong, or
// "".
func (s site) untouched(before, after string) string {
	if s.obj == nil {
		return ""
	}
	from, to := s.span()
	head, tail := before[:from], before[to:]
	if !strings.HasPrefix(after, head) || !strings.HasSuffix(after, tail) || len(after) < len(head)+len(tail) {
		return fmt.Sprintf("the text outside %q changed", before[from:to])
	}
	middle := after[len(head) : len(after)-len(tail)]
	adds := s.member == nil || s.fates != nil && !slices.Contains(s.fates, replaced) && !slices.Contains(s.fates, removed)
	// The one thing an insertion may take away: the spaces of a container
	// that held nothing else (G5).
	hollow := s.items() == 0 && strings.Trim(before[from:to], " \t\r") == ""
	if adds && !hollow && !subsequence(before[from:to], middle) {
		return fmt.Sprintf("%q did not stay as it was in %q", before[from:to], middle)
	}
	for i, f := range s.fates {
		if f != stays {
			continue
		}
		e := s.member.Value.Value.(*hujson.Array).Elements[i]
		kept := before[e.StartOffset:e.EndOffset]
		at := strings.Index(middle, kept)
		if at < 0 {
			return fmt.Sprintf("the kept element %q changed or moved", kept)
		}
		middle = middle[at+len(kept):]
	}
	return ""
}

// items counts the members or elements of the container the call adds to:
// the list of an UpdateArray, else the object that gets a new member.
func (s site) items() int {
	if s.member != nil {
		arr, _ := s.member.Value.Value.(*hujson.Array)
		if arr == nil {
			return -1 // a value that is written over, not a container that grows
		}
		return len(arr.Elements)
	}
	return len(s.obj.Value.(*hujson.Object).Members)
}

// subsequence reports whether the bytes of part are in whole in the same
// order.
func subsequence(part, whole string) bool {
	i := 0
	for _, c := range []byte(part) {
		at := strings.IndexByte(whole[i:], c)
		if at < 0 {
			return false
		}
		i += at + 1
	}
	return true
}

// span returns the part of the text the call may change: the inside of the
// list for an UpdateArray, the member's value for a Set (and its name, when
// comments from inside the old value have to go in front of it), and for a
// new member what lies between the last member and the closing brace.
func (s site) span() (from, to int) {
	if s.member == nil {
		obj := s.obj.Value.(*hujson.Object)
		from = s.obj.StartOffset + 1
		if n := len(obj.Members); n > 0 {
			from = obj.Members[n-1].Value.EndOffset
		}
		return from, s.obj.EndOffset - 1
	}
	v := s.member.Value
	old, _ := strip(s.text[v.StartOffset:v.EndOffset])
	switch {
	case s.fates != nil:
		return v.StartOffset + 1, v.EndOffset - 1
	case len(old.comments) > 0:
		return s.member.Name.StartOffset, v.EndOffset
	}
	return v.StartOffset, v.EndOffset
}

// style is a layout a test text can be put in: the tests write their texts
// with two spaces and line feeds, and in turns them into the style.
type style struct{ name, unit, eol string }

var (
	asWritten = style{"as written", "  ", "\n"}
	styles    = []style{
		{"LF and spaces", "  ", "\n"},
		{"CRLF and spaces", "  ", "\r\n"},
		{"LF and tabs", "\t", "\n"},
		{"CRLF and tabs", "\t", "\r\n"},
		{"LF and four spaces", "    ", "\n"},
	}
)

func (s style) in(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		rest := strings.TrimLeft(line, " ")
		n := len(line) - len(rest)
		lines[i] = strings.Repeat(s.unit, n/2) + strings.Repeat(" ", n%2) + rest
	}
	return strings.Join(lines, s.eol)
}

// same as the text an editCase wants: the call must not change a byte.
const same = ""

// editCase is one call on one text.
type editCase struct {
	name string
	src  string
	call call
	want string // the text afterwards, or same
	err  string // the error the call must return; the text must then stay
}

// run makes the call in style s and checks every guarantee and the exact
// text. Then it makes the call again: verify sees to it that a second run
// that has nothing left to do changes no byte.
func (tc editCase) run(t *testing.T, s style) {
	t.Helper()
	src, want := s.in(tc.src), s.in(tc.want)
	if tc.want == same {
		want = src
	}
	got := verify(t, src, tc.call, tc.err)
	if got != want {
		t.Fatalf("%s\nbefore: %q\nafter:  %q\nwant:   %q", tc.call, src, got, want)
	}
	if tc.err != "" {
		return
	}
	verify(t, got, tc.call, "")
}

// runCases runs each case in each style.
func runCases(t *testing.T, cases []editCase, in ...style) {
	t.Helper()
	for _, s := range in {
		for _, tc := range cases {
			t.Run(tc.name+", "+s.name, func(t *testing.T) { tc.run(t, s) })
		}
	}
}
