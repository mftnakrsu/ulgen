package jsonc

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tailscale/hujson"
)

// Set writes value at a path of object keys. Missing parent objects are
// created. An existing value is replaced in place: only its own text
// changes, the comments around it stay, and comments inside the old text
// move in front of the member. A value equal to the one already there
// changes no byte. A new member goes behind the last one and starts a line
// of its own; in an object that is written on one line it stays on that
// line.
//
// It is an error when a key on the way holds something other than an object
// ("a.b is not an object") or occurs more than once in its object ("a.b
// appears more than once"), and when value itself holds an object with the
// same key twice ("k appears more than once in the value to write"). After
// an error the document is as it was.
func (d *Doc) Set(value any, path ...string) error {
	w, s, err := d.open(path)
	if err != nil {
		return err
	}
	want, err := exact(value)
	if err != nil {
		return err
	}
	result := put(w.decoded, path, want)
	if equal(result, w.decoded) {
		return nil
	}
	if err := w.set(s, value, path); err != nil {
		return err
	}
	return d.commit(w, result)
}

// UpdateArray rewrites the array at a path of object keys. drop is called
// once for each element, in order, with the decoded element, and says which
// elements are the caller's; add is what should be there instead:
//
//   - The i-th dropped element is replaced in place by the i-th value of
//     add, and left alone when the two are equal.
//   - Values of add beyond that are appended behind the last element, each
//     starting a line of its own.
//   - Dropped elements beyond the length of add are removed. Their comments
//     stay.
//
// The other elements stay exactly as they are. A nil drop drops nothing, so
// with a nil drop every call appends all of add again: nothing tells such a
// call which elements an earlier one wrote. A caller that wants a second
// identical call to change nothing gives a drop that knows its own elements.
// A missing array is created with its parents, but only when there is
// something to add. It is an error when the path holds something other
// than an array ("a.b is not a list"); the errors of Set apply as well.
func (d *Doc) UpdateArray(drop func(any) bool, add []any, path ...string) error {
	w, s, err := d.open(path)
	if err != nil {
		return err
	}
	want, err := exactAll(add)
	if err != nil {
		return err
	}
	if s.member < 0 {
		if len(add) == 0 {
			return nil
		}
		return d.Set(add, path...)
	}
	arr, ok := s.obj.Members[s.member].Value.Value.(*hujson.Array)
	if !ok {
		return fmt.Errorf("%s is not a list", dotted(path))
	}
	p, err := newPlan(arr, drop, want)
	if err != nil {
		return err
	}
	result := put(w.decoded, path, p.result)
	if equal(result, w.decoded) {
		return nil
	}
	if err := w.rewrite(arr, p, add, s.line); err != nil {
		return err
	}
	return d.commit(w, result)
}

func dotted(path []string) string { return strings.Join(path, ".") }

// draft is a copy of the document that an edit works on. The document takes
// it over in commit, and only when it passes the checks there, so an edit
// that fails on the way leaves the document as it was.
type draft struct {
	doc      *Doc
	root     hujson.Value
	decoded  any      // what the document decodes to before the edit
	comments []string // its comments before the edit
}

// open starts an edit at path: a draft, and the spot the path leads to.
func (d *Doc) open(path []string) (*draft, spot, error) {
	if len(path) == 0 {
		return nil, spot{}, errors.New("the path is empty")
	}
	w, err := d.begin()
	if err != nil {
		return nil, spot{}, err
	}
	s, err := w.resolve(path)
	return w, s, err
}

// begin copies the document into a draft by parsing its text again; a
// document without a value gets an empty object behind its text there.
// (hujson's Clone will not do: it turns an empty text into nil, and with
// that drops every trailing comma that has nothing in front of it.)
func (d *Doc) begin() (*draft, error) {
	text := d.root.Pack()
	if d.root.Value == nil {
		text = d.seed()
	}
	root, err := hujson.Parse(text)
	if err != nil {
		return nil, withoutText(err)
	}
	decoded, err := decode(root, true)
	if err != nil {
		return nil, err
	}
	return &draft{doc: d, root: root, decoded: decoded, comments: comments(&root)}, nil
}

// seed returns the text of a document without a value with an empty object
// behind it, on a line of its own and with a final line break.
func (d *Doc) seed() []byte {
	text := bytes.Clone(d.root.BeforeExtra)
	if len(text) > 0 && !bytes.HasSuffix(text, []byte("\n")) {
		text = append(text, d.eol...)
	}
	return append(text, "{}"+d.eol...)
}

// commit is the safety net under every edit. The draft's text is parsed
// again, and the document takes it over only if it parses, decodes to want
// and holds the comments the document had, each once. Otherwise the
// document stays as it was and the error says which promise would break.
func (d *Doc) commit(w *draft, want any) error {
	root, err := hujson.Parse(w.root.Pack())
	if err != nil {
		return fmt.Errorf("the change was not made, its result would not parse: %v", withoutText(err))
	}
	if got, err := decode(root, true); err != nil || !equal(got, want) {
		return errors.New("the change was not made, its result would not hold the requested value")
	}
	if !slices.Equal(comments(&root), w.comments) {
		return errors.New("the change was not made, it would lose or repeat a comment")
	}
	d.root = root
	d.readStyle(d.text())
	return nil
}

// spot is where a path of object keys ends in the tree: the last object on
// the way, and in it the member that the next key names, if there is one.
type spot struct {
	obj    *hujson.Object
	depth  int    // path[depth] is the key looked up in obj
	member int    // the member with that key, or -1
	indent string // indentation of the line obj opens on
	child  string // indentation of the members of obj
	line   string // indentation of the line the member stands on
	inline bool   // obj is written on one line
}

// resolve follows path from the root for as long as its keys exist.
func (w *draft) resolve(path []string) (spot, error) {
	var s spot
	v, indent, around := &w.root, "", false
	for i, key := range path {
		obj, ok := v.Value.(*hujson.Object)
		if !ok {
			return s, fmt.Errorf("%s is not an object", dotted(path[:i]))
		}
		f := frameOf(obj)
		s = spot{obj: obj, depth: i, member: -1, indent: indent, inline: oneLine(obj, around)}
		s.child = f.childIndent(indent, w.doc.unit)
		j, count := find(obj, key)
		if count > 1 {
			return s, fmt.Errorf("%s appears more than once", dotted(path[:i+1]))
		}
		if j < 0 {
			break
		}
		s.member, s.line = j, f.lineIndent(j, indent)
		v, indent, around = &obj.Members[j].Value, s.line, s.inline
	}
	return s, nil
}

// set writes value at the end of path: over the member s found there, or
// as a new member of the last object the path reaches, inside the objects
// that the rest of the path names.
func (w *draft) set(s spot, value any, path []string) error {
	if s.member >= 0 {
		m := &s.obj.Members[s.member]
		return w.replace(&m.Value, &m.Name.BeforeExtra, value, s.line, s.inline)
	}
	for i := len(path) - 1; i > s.depth; i-- {
		value = Object{{Key: path[i], Value: value}}
	}
	return w.addMember(s, path[s.depth], value)
}

// replace writes value over v, which stands on a line indented by indent,
// or, with inline set, in a container written on one line. Only the text of
// v itself changes. Comments inside the old text move to the end of lead,
// the text in front of the member or element that v belongs to.
func (w *draft) replace(v *hujson.Value, lead *hujson.Extra, value any, indent string, inline bool) error {
	text, err := w.doc.render(value, indent, inline)
	if err != nil {
		return err
	}
	before := scan(*lead)
	_, ownLine := indentOf(before)
	*lead = join(before, w.doc.lift(commentsIn(&hujson.Value{Value: v.Value}), indent, ownLine))
	v.Value = text
	return nil
}

// addMember appends the member key: value to the object at s.
func (w *draft) addMember(s spot, key string, value any) error {
	name, err := marshal(key)
	if err != nil {
		return err
	}
	text, err := w.doc.render(value, s.child, s.inline)
	if err != nil {
		return err
	}
	f := frameOf(s.obj)
	j := w.doc.joint(f, s.inline, s.child, s.indent)
	m := hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: j.first, Value: hujson.Literal(name)},
		Value: hujson.Value{BeforeExtra: hujson.Extra(" "), Value: text},
	}
	if f.take(j) {
		m.Value.AfterExtra = hujson.Extra{}
	}
	s.obj.Members = append(s.obj.Members, m)
	s.obj.AfterExtra = j.tail
	return nil
}

// What a plan does with an element when it is not replaced.
const (
	kept = -1
	gone = -2
)

// plan is what UpdateArray does to an array.
type plan struct {
	fate   []int // for each element: kept, gone, or the index in add of the value that replaces it
	used   int   // how many values of add replace an element; the others are appended
	result []any // what the array decodes to afterwards
}

// newPlan decides what becomes of each element of arr; want is what the
// values to add decode to.
func newPlan(arr *hujson.Array, drop func(any) bool, want []any) (plan, error) {
	p := plan{fate: make([]int, len(arr.Elements)), result: []any{}}
	dropped := 0
	for i, e := range arr.Elements {
		was, ours, err := judge(e, drop)
		switch {
		case err != nil:
			return plan{}, err
		case !ours, dropped < len(want) && equal(was, want[dropped]):
			p.fate[i] = kept
		case dropped < len(want):
			p.fate[i], was = dropped, want[dropped]
		default:
			p.fate[i] = gone
		}
		if p.fate[i] != gone {
			p.result = append(p.result, was)
		}
		if ours {
			dropped++
		}
	}
	p.used = min(dropped, len(want))
	p.result = append(p.result, want[p.used:]...)
	return p, nil
}

// judge decodes an element, for comparing and for drop, and asks drop
// whether the element is the caller's.
func judge(e hujson.Value, drop func(any) bool) (was any, ours bool, err error) {
	if was, err = decode(e, true); err != nil || drop == nil {
		return was, false, err
	}
	seen, err := decode(e, false)
	return was, err == nil && drop(seen), err
}

// rewrite carries out p on arr, which opens on a line indented by indent.
func (w *draft) rewrite(arr *hujson.Array, p plan, add []any, indent string) error {
	f := frameOf(arr)
	child, inline := f.childIndent(indent, w.doc.unit), oneLine(arr, false)
	for i, k := range p.fate {
		if k < 0 {
			continue
		}
		e := &arr.Elements[i]
		if err := w.replace(e, &e.BeforeExtra, add[k], f.lineIndent(i, indent), inline); err != nil {
			return err
		}
	}
	for i := len(p.fate) - 1; i >= 0; i-- {
		if p.fate[i] == gone {
			w.remove(arr, i, child)
		}
	}
	return w.push(arr, add[p.used:], child, indent)
}

// remove takes element i out of arr, with one comma. The comments around it
// and inside it stay where it was; indent is the indentation of the
// elements.
func (w *draft) remove(arr *hujson.Array, i int, indent string) {
	x, last := arr.Elements[i], len(arr.Elements)-1
	left := scan(x.BeforeExtra)
	_, ownLine := indentOf(left)
	left = append(left, w.doc.lift(commentsIn(&hujson.Value{Value: x.Value}), indent, ownLine)...)
	if i < last {
		// The comma behind it goes; what stood around it leads up to the next element.
		next := &arr.Elements[i+1].BeforeExtra
		*next = join(closeGap(left, append(scan(x.AfterExtra), scan(*next)...)))
	} else {
		// The comma in front of it goes, or its own when the array ends with one.
		right := append(scan(x.AfterExtra), scan(arr.AfterExtra)...)
		arr.AfterExtra = join(handOver(arr.Elements[:i], x.AfterExtra != nil), leave(left, right))
	}
	arr.Elements = slices.Delete(arr.Elements, i, i+1)
	if len(arr.Elements) == 0 && !slices.ContainsFunc(scan(arr.AfterExtra), token.comment) {
		arr.AfterExtra = nil // nothing left: "[]"
	}
}

// handOver makes the last of rest the last element of its array, after the
// one behind it is gone. It takes over the trailing comma if there was
// one; if not, it loses its own comma, and the text that stood in front of
// that comma is returned, to stand in front of the closing bracket.
func handOver(rest []hujson.Value, trailingComma bool) []token {
	if len(rest) == 0 {
		return nil
	}
	prev := &rest[len(rest)-1]
	if trailingComma {
		if prev.AfterExtra == nil {
			prev.AfterExtra = hujson.Extra{}
		}
		return nil
	}
	moved := scan(prev.AfterExtra)
	prev.AfterExtra = nil
	return moved
}

// push appends values to arr as elements, each starting a line of its own
// that is indented by child. indent is the indentation of the line arr
// opens on.
func (w *draft) push(arr *hujson.Array, values []any, child, indent string) error {
	if len(values) == 0 {
		return nil
	}
	f := frameOf(arr)
	j := w.doc.joint(f, false, child, indent)
	trailing := f.take(j)
	for i, value := range values {
		text, err := w.doc.render(value, child, false)
		if err != nil {
			return err
		}
		e := hujson.Value{BeforeExtra: j.next, Value: text}
		if i == 0 {
			e.BeforeExtra = j.first
		}
		arr.Elements = append(arr.Elements, e)
	}
	if trailing {
		arr.Elements[len(arr.Elements)-1].AfterExtra = hujson.Extra{}
	}
	arr.AfterExtra = j.tail
	return nil
}
