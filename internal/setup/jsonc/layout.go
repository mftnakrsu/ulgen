package jsonc

import (
	"bytes"
	"slices"

	"github.com/tailscale/hujson"
)

// frame gives one view of an object and an array: the text in front of each
// item (a member or an element), the text between each item and the comma
// behind it, and the text in front of the closing bracket. The fields point
// into the tree, so a frame is void once items are added or removed.
type frame struct {
	leads []*hujson.Extra
	ends  []*hujson.Extra
	tail  *hujson.Extra
}

// frameOf returns the frame of an object or an array. That of any other
// value is empty.
func frameOf(v hujson.ValueTrimmed) frame {
	var f frame
	switch c := v.(type) {
	case *hujson.Object:
		for i := range c.Members {
			f.leads = append(f.leads, &c.Members[i].Name.BeforeExtra)
			f.ends = append(f.ends, &c.Members[i].Value.AfterExtra)
		}
		f.tail = &c.AfterExtra
	case *hujson.Array:
		for i := range c.Elements {
			f.leads = append(f.leads, &c.Elements[i].BeforeExtra)
			f.ends = append(f.ends, &c.Elements[i].AfterExtra)
		}
		f.tail = &c.AfterExtra
	}
	return f
}

// trailingComma reports whether the container ends with a comma. hujson
// marks that on the last item: its text in front of the comma is not nil.
func (f frame) trailingComma() bool {
	n := len(f.ends)
	return n > 0 && *f.ends[n-1] != nil
}

// oneLine reports whether an object or an array is written on one line:
// its text has no line break. One that is hollow has no layout of its own
// and takes that of the container around it.
func oneLine(v hujson.ValueTrimmed, around bool) bool {
	if frameOf(v).hollow() {
		return around
	}
	return !bytes.Contains(hujson.Value{Value: v}.Pack(), []byte("\n"))
}

// hollow reports whether a container has nothing between its brackets, or
// nothing but spaces: no item, no comment, no line break. "{ }" is laid out
// like "{}".
func (f frame) hollow() bool {
	return len(f.leads) == 0 && !slices.ContainsFunc(scan(*f.tail), func(t token) bool { return t.kind != space })
}

// childIndent returns the indentation of the items: that of the last item
// that starts a line. When none does, or a comment stands in front of it on
// its line, it is one unit more than indent, the indentation of the line
// the container opens on.
func (f frame) childIndent(indent, unit string) string {
	for i := len(f.leads) - 1; i >= 0; i-- {
		if own, ok := indentOf(scan(*f.leads[i])); ok {
			return own
		}
	}
	return indent + unit
}

// lineIndent returns the indentation of the line item i stands on: the
// spaces that line starts with. An item that does not start a line shares
// that of the item in front of it, and the first one may share the line the
// container opens on, which is indented by indent.
func (f frame) lineIndent(i int, indent string) string {
	for ; i >= 0; i-- {
		if start, ok := lineStart(scan(*f.leads[i])); ok {
			return start
		}
	}
	return indent
}

// joint is the text that joins new items to the end of a container.
type joint struct {
	comma hujson.Extra // between the item that was last and the comma now behind it
	first hujson.Extra // in front of the first new item
	next  hujson.Extra // in front of each further one
	tail  hujson.Extra // in front of the closing bracket
}

// joint works out how new items join the container f. It only adds text:
// whatever stood between the last item and the closing bracket stays, in
// front of the new items or behind them (see splitTail). With inline set
// everything stays on one line. Otherwise each new item starts a line that
// is indented by child, and a hollow container closes on a line of its own
// that is indented by indent; the spaces it held go, as the only text an
// insertion ever takes away.
func (d *Doc) joint(f frame, inline bool, child, indent string) joint {
	stay, rest, lines := splitTail(scan(*f.tail))
	var comma []token
	if len(f.leads) > 0 && !f.trailingComma() {
		comma, stay = splitComma(stay)
	}
	j := joint{comma: join(comma), first: join(stay), next: hujson.Extra(d.eol + child), tail: join(rest)}
	empty := len(f.leads) == 0 && len(*f.tail) == 0
	switch {
	case inline:
		if !empty {
			j.first = append(j.first, ' ')
		}
	case lines:
		j.first = append(j.first, child...)
		j.tail = append(hujson.Extra(d.eol), j.tail...)
	case f.hollow():
		j.first, j.tail = j.next, hujson.Extra(d.eol+indent)
	default:
		j.first = append(j.first, j.next...)
	}
	return j
}

// take makes the last item of f ready for a comma behind it: the text j
// keeps in front of that comma goes there. It reports whether f ends with a
// comma already, which the last of the new items then has to do as well.
func (f frame) take(j joint) bool {
	trailing := f.trailingComma()
	if n := len(f.ends); n > 0 && !trailing {
		*f.ends[n-1] = j.comma
	}
	return trailing
}

// splitTail cuts the text in front of a closing bracket in two: what stays
// in front of an item appended to the container, and what stays behind it.
// When the text has a line break (lines), the first part is the rest of the
// last item's line and the comment lines below it, the second the blank
// lines and the line of the bracket: the new item gets a line in between.
// When it has none, the bracket stands on the last item's line and stays
// behind the new item with the spaces in front of it; the comments stay
// with the item they follow.
func splitTail(tail []token) (stay, rest []token, lines bool) {
	cut, lastComment := -1, 0
	open := true // the line so far is worth keeping in front: an item or a comment is on it
	for i, t := range tail {
		if t.comment() {
			lastComment, open = i+1, true
		}
		if t.endsLine() {
			if open {
				cut = i + 1
			}
			open = false
		}
	}
	if cut < 0 {
		return tail[:lastComment], tail[lastComment:], false
	}
	return tail[:cut], tail[cut:], true
}

// splitComma cuts the text that stays behind the last item where the comma
// goes: behind the block comments on the item's line, so that they stay
// with the item, and in front of everything else.
func splitComma(stay []token) (before, after []token) {
	cut := 0
	for i, t := range stay {
		if t.endsLine() {
			break
		}
		if t.kind == blockComment {
			cut = i + 1
		}
	}
	return stay[:cut], stay[cut:]
}

// closeGap returns what stays of the text left and right of an item that
// goes together with the comma behind it. The spaces behind it go too, and
// so does its line when nothing else is on it; a comment keeps its place.
func closeGap(left, right []token) []token {
	if n := len(left); n == 0 || left[n-1].kind != blockComment {
		right = trimLeft(right)
	}
	if len(right) > 0 && right[0].kind == newline {
		left = trimRight(left)
		if n := len(left); n > 0 && left[n-1].endsLine() {
			right = right[1:]
		}
	}
	return slices.Concat(left, right)
}

// leave returns what stays of the text left and right of the last item of
// a container when it goes together with the comma in front of it. With no
// comment next to it, it takes the text in front of it along; otherwise it
// leaves a gap for closeGap.
func leave(left, right []token) []token {
	behind := trimLeft(right)
	if slices.ContainsFunc(left, token.comment) || len(behind) > 0 && behind[0].comment() {
		return closeGap(left, right)
	}
	return right
}
