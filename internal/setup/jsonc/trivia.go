package jsonc

import (
	"bytes"
	"slices"

	"github.com/tailscale/hujson"
)

// The text between two JSON tokens, whitespace and comments (hujson calls
// it an Extra), is handled as a row of tokens and only ever cut between two
// of them, so no cut can land inside a comment. The token of a line comment
// includes its line break: whatever is put behind it starts on a new line,
// and no bracket can end up inside the comment.

// kind says what a token is.
type kind int

const (
	space        kind = iota // spaces, tabs, a carriage return without a line feed
	newline                  // one line break
	lineComment              // "// ..." with the line break that ends it
	blockComment             // "/* ... */"
)

// token is one piece of the text between two JSON tokens.
type token struct {
	kind kind
	text []byte
}

func (t token) comment() bool  { return t.kind == lineComment || t.kind == blockComment }
func (t token) endsLine() bool { return t.kind == newline || t.kind == lineComment }

// scan splits the text between two JSON tokens into its tokens.
func scan(extra []byte) []token {
	var row []token
	for len(extra) > 0 {
		k, n := next(extra)
		row = append(row, token{k, extra[:n]})
		extra = extra[n:]
	}
	return row
}

// next returns the kind and the length of the token extra starts with.
func next(extra []byte) (kind, int) {
	switch {
	case extra[0] == '\n':
		return newline, 1
	case bytes.HasPrefix(extra, []byte("\r\n")):
		return newline, 2
	case bytes.HasPrefix(extra, []byte("//")):
		if i := bytes.IndexByte(extra, '\n'); i >= 0 {
			return lineComment, i + 1
		}
		return lineComment, len(extra)
	case bytes.HasPrefix(extra, []byte("/*")):
		if i := bytes.Index(extra[2:], []byte("*/")); i >= 0 {
			return blockComment, i + 4
		}
		return blockComment, len(extra)
	}
	n := 1
	for n < len(extra) && isSpace(extra[n:]) {
		n++
	}
	return space, n
}

// isSpace reports whether extra starts with a space, a tab, or a carriage
// return that is not half of a line break.
func isSpace(extra []byte) bool {
	return extra[0] == ' ' || extra[0] == '\t' || extra[0] == '\r' && !bytes.HasPrefix(extra, []byte("\r\n"))
}

// join puts rows of tokens back together as text.
func join(rows ...[]token) hujson.Extra {
	text := hujson.Extra{}
	for _, row := range rows {
		for _, t := range row {
			text = append(text, t.text...)
		}
	}
	return text
}

// trimLeft drops the spaces a row starts with.
func trimLeft(row []token) []token {
	for len(row) > 0 && row[0].kind == space {
		row = row[1:]
	}
	return row
}

// trimRight drops the spaces a row ends with.
func trimRight(row []token) []token {
	for len(row) > 0 && row[len(row)-1].kind == space {
		row = row[:len(row)-1]
	}
	return row
}

// indentOf returns the indentation of the line a row ends on, and whether
// it can be read: the row must end with a line break and, after it, nothing
// but spaces.
func indentOf(row []token) (string, bool) {
	indent := ""
	if n := len(row); n > 0 && row[n-1].kind == space {
		indent, row = string(row[n-1].text), row[:n-1]
	}
	if n := len(row); n > 0 && row[n-1].endsLine() {
		return indent, true
	}
	return "", false
}

// lineStart returns the spaces at the start of the line a row ends on, and
// whether the row has a line break at all.
func lineStart(row []token) (string, bool) {
	for i := len(row) - 1; i >= 0; i-- {
		if !row[i].endsLine() {
			continue
		}
		if i+1 < len(row) && row[i+1].kind == space {
			return string(row[i+1].text), true
		}
		return "", true
	}
	return "", false
}

// extras calls visit with every text between two JSON tokens in v, in the
// order of the document.
func extras(v *hujson.Value, visit func(hujson.Extra)) {
	visit(v.BeforeExtra)
	switch c := v.Value.(type) {
	case *hujson.Object:
		for i := range c.Members {
			extras(&c.Members[i].Name, visit)
			extras(&c.Members[i].Value, visit)
		}
		visit(c.AfterExtra)
	case *hujson.Array:
		for i := range c.Elements {
			extras(&c.Elements[i], visit)
		}
		visit(c.AfterExtra)
	}
	visit(v.AfterExtra)
}

// commentsIn returns the comments in and around v, in order.
func commentsIn(v *hujson.Value) []token {
	var found []token
	extras(v, func(extra hujson.Extra) {
		for _, t := range scan(extra) {
			if t.comment() {
				found = append(found, t)
			}
		}
	})
	return found
}

// comments returns the text of every comment in and around v, sorted, a
// line comment without its line break. Two documents hold the same comments
// when the two lists are equal.
func comments(v *hujson.Value) []string {
	var texts []string
	for _, t := range commentsIn(v) {
		texts = append(texts, string(bytes.TrimRight(t.text, "\r\n")))
	}
	slices.Sort(texts)
	return texts
}

// lift lays out comments that lose the value they stood in, so that they
// can stand in front of whatever comes next on a line indented by indent. A
// line comment is followed by the indentation; a block comment by a line
// break and the indentation when ownLine is set, and by a space otherwise.
func (d *Doc) lift(found []token, indent string, ownLine bool) []token {
	var row []token
	for _, c := range found {
		row = append(row, c)
		switch {
		case c.kind == lineComment:
			row = append(row, token{space, []byte(indent)})
		case ownLine:
			row = append(row, token{newline, []byte(d.eol)}, token{space, []byte(indent)})
		default:
			row = append(row, token{space, []byte(" ")})
		}
	}
	return row
}
