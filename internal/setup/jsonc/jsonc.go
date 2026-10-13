package jsonc

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tailscale/hujson"
)

const (
	// defaultIndent is used when a document has no indentation to copy.
	defaultIndent = "  "
	// byteOrderMark opens the UTF-8 files of some Windows editors.
	byteOrderMark = "\xef\xbb\xbf"
)

// Doc is a parsed document. Its text changes only where Set and UpdateArray
// write.
type Doc struct {
	bom  bool         // the text began with a byte-order mark
	cut  bool         // the text ends inside a line comment (see Bytes)
	root hujson.Value // root.Value is nil while the document holds no value
	unit string       // one level of indentation
	eol  string       // the document's line break, "\n" or "\r\n"
}

// Member is one key of an Object.
type Member struct {
	Key   string // the name, unescaped
	Value any    // anything encoding/json can marshal
}

// Object is a JSON object whose keys keep the order given, which a Go map
// cannot do. Use it for values passed to Set when the order matters.
type Object []Member

// MarshalJSON writes the members in order.
func (o Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(m.Key)
		if err != nil {
			return nil, err
		}
		v, err := marshal(m.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// New returns an empty document: "{}" and a final line break.
func New() *Doc {
	d, _ := Parse([]byte("{}\n"))
	return d
}

// Parse reads a document; it keeps no reference to src. A leading
// byte-order mark is set aside and written back by Bytes. A text with
// nothing but whitespace and comments is a document without a value, which
// the first Set turns into an object. A text may end inside a line comment,
// without a line break behind it.
//
// Any other text that does not parse is refused, and so is one whose top
// level is a list or a single value instead of an object: the package edits
// objects. The error names the line and column (counted behind a byte-order
// mark) and the kind of problem, and holds no text of the document (see
// withoutText): the document is the user's file, and what does not parse in
// it can be a secret.
func Parse(src []byte) (*Doc, error) {
	text, bom := bytes.CutPrefix(src, []byte(byteOrderMark))
	text = bytes.Clone(text)
	d := &Doc{bom: bom}
	if blank(text) {
		d.root.BeforeExtra = text
		d.readStyle(text)
		return d, nil
	}
	root, err := hujson.Parse(text)
	if err != nil {
		// hujson wants a line break behind every line comment. A text that
		// parses with one at its end ends inside a line comment.
		closed, cerr := hujson.Parse(withBreak(text))
		if cerr != nil {
			return nil, withoutText(err)
		}
		root, d.cut = closed, true
	}
	if _, ok := root.Value.(*hujson.Object); !ok {
		line, column := position(text, len(root.BeforeExtra))
		return nil, fmt.Errorf("line %d, column %d: the top-level value is not an object", line, column)
	}
	d.root = root
	d.readStyle(text)
	return d, nil
}

// readStyle takes the line break and the indentation the package writes
// with from the document's text: the line break of its first line (LF
// without one) and the indentation of the top-level members (two spaces when
// there is none to copy). Parse and every edit that is taken over call it,
// so what an edit writes depends on the text as it is now, not on the text
// the document was once parsed from.
func (d *Doc) readStyle(text []byte) {
	d.eol, d.unit = lineEnding(text), defaultIndent
	if unit := frameOf(d.root.Value).childIndent("", ""); unit != "" {
		d.unit = unit
	}
}

// position returns the line and column of the byte at offset n of text,
// both counted from 1, the column in bytes.
func position(text []byte, n int) (line, column int) {
	return 1 + bytes.Count(text[:n], []byte("\n")), n - bytes.LastIndexByte(text[:n], '\n')
}

// syntaxCharacters are the characters JSON's own syntax is made of. In a
// parse error one of them says what is wrong; any other character there is
// a piece of what the user wrote.
const syntaxCharacters = `{}[],:"/`

// What hujson says about a text that does not parse, behind the position.
// parseProblems are the messages that hold no text of the document.
// characterPlaces are the places it names for a character that is out of
// place.
var (
	parseProblems = []string{
		"parsing value: unexpected EOF",
		"parsing object after name: unexpected EOF",
		"parsing object after value: unexpected EOF",
		"parsing array after value: unexpected EOF",
		"parsing string: unexpected EOF",
		"parsing comment: unexpected EOF",
		"invalid UTF-8 in comment",
	}
	characterPlaces = []string{
		"at start of value",
		"at start of object name",
		"after object name",
		"after object value (expecting ',' or '}')",
		"after array value (expecting ',' or ']')",
		"after top-level value",
		"in line comment",
	}
)

// withoutText returns a parse error of hujson as the position and the kind
// of problem, without the text of the document that hujson quotes:
//
//   - "line 2, column 28: invalid literal" for a value that is not JSON.
//     hujson prints the value behind it, and a value whose quotes were
//     forgotten or that has a bad escape in it can be a key.
//   - "line 2, column 9: invalid character after object name" for a
//     character that is out of place. hujson names the character; it stays
//     only when it is one of JSON's own, like the brace in "invalid
//     character '}' at start of value".
//
// Only messages known to hold no text are passed on. Anything else, such as
// a message a later version of hujson may add, becomes "the text does not
// parse".
func withoutText(err error) error {
	msg := plain(err).Error()
	place := ""
	var line, column int
	if n, _ := fmt.Sscanf(msg, "line %d, column %d: ", &line, &column); n == 2 {
		place = fmt.Sprintf("line %d, column %d: ", line, column)
		msg = strings.TrimPrefix(msg, place)
	}
	return errors.New(place + problem(msg))
}

// problem returns the kind of problem a message of hujson names, the
// position cut off, in words that hold no text of the document.
func problem(msg string) string {
	const literal, character = "invalid literal", "invalid character "
	switch {
	case strings.HasPrefix(msg, literal):
		return literal
	case slices.Contains(parseProblems, msg):
		return msg
	case !strings.HasPrefix(msg, character):
		return "the text does not parse"
	}
	// What follows is the character between single quotes, as Go writes a
	// rune ('x', '\'', '\xff'), then a space and where the character stands.
	rest := msg[len(character):]
	for _, where := range characterPlaces {
		quoted, found := strings.CutSuffix(rest, " "+where)
		switch {
		case !found:
		case len(quoted) == 3 && strings.IndexByte(syntaxCharacters, quoted[1]) >= 0:
			return character + quoted + " " + where
		default:
			return character + where
		}
	}
	return "invalid character"
}

// blank reports whether text holds nothing but whitespace and comments. A
// line comment that the end of the text cuts short counts as one.
func blank(text []byte) bool {
	return hujson.Extra(text).IsValid() || hujson.Extra(withBreak(text)).IsValid()
}

// withBreak returns a copy of text with a line break at its end.
func withBreak(text []byte) []byte {
	return append(text[:len(text):len(text)], '\n')
}

// lineEnding returns the line break of a text: that of its first line.
func lineEnding(text []byte) string {
	if i := bytes.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// Bytes returns the document's text.
func (d *Doc) Bytes() []byte {
	text := d.text()
	if d.bom {
		text = append([]byte(byteOrderMark), text...)
	}
	return text
}

// text returns the document's text without the byte-order mark.
func (d *Doc) text() []byte {
	text := d.root.Pack()
	if d.cut {
		// The tree of a text that ends inside a line comment holds the line
		// break hujson wants behind the comment. It is not part of the
		// text, and no edit puts anything behind it.
		text = text[:len(text)-1]
	}
	return text
}

// HasValue reports whether the document holds a value: the object that is
// its top level. A text with nothing but whitespace and comments holds none
// (G9), and the first Set gives it one. This is the question to ask of a
// file that may be empty. Get does not answer it: Get also says "not there"
// for a value it cannot decode, such as one with a number too large to read.
func (d *Doc) HasValue() bool { return d.root.Value != nil }

// Get returns the value at a path of object keys, decoded as encoding/json
// decodes into an empty interface, and whether it exists. Of a key that
// occurs more than once the last one counts, as it does for JSON loaders.
func (d *Doc) Get(path ...string) (any, bool) {
	v := &d.root
	for _, key := range path {
		obj, ok := v.Value.(*hujson.Object)
		if !ok {
			return nil, false
		}
		i, _ := find(obj, key)
		if i < 0 {
			return nil, false
		}
		v = &obj.Members[i].Value
	}
	if v.Value == nil {
		return nil, false // the document holds no value
	}
	out, err := decode(*v, false)
	return out, err == nil
}

// find returns the index of the last member of obj named key, or -1, and
// how many members have that name.
func find(obj *hujson.Object, key string) (index, count int) {
	index = -1
	for i, m := range obj.Members {
		if name, ok := m.Name.Value.(hujson.Literal); ok && name.String() == key {
			index, count = i, count+1
		}
	}
	return index, count
}

// TrailingComma reports whether any object or array in the document ends
// with a comma, which strict loaders reject.
func (d *Doc) TrailingComma() bool {
	for v := range d.root.All() {
		if frameOf(v.Value).trailingComma() {
			return true
		}
	}
	return false
}
