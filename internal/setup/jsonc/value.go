package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// marshal is json.Marshal without the escaping of <, > and &.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, plain(err)
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// plain drops the package prefix of an encoding/json or hujson error; the
// callers put the name of the file in front of the message.
func plain(err error) error {
	msg := strings.TrimPrefix(err.Error(), "json: ")
	return errors.New(strings.TrimPrefix(msg, "hujson: "))
}

// render formats value as it should appear on a line indented by indent:
// over several lines with the document's indentation and line break, or,
// with inline set, on one line.
func (d *Doc) render(value any, indent string, inline bool) (hujson.ValueTrimmed, error) {
	raw, err := marshal(value)
	if err != nil {
		return nil, err
	}
	unit, eol := d.unit, d.eol
	if inline {
		indent, unit, eol = "", "", ""
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, indent, unit); err != nil {
		return nil, plain(err)
	}
	// json.Indent breaks lines with "\n", and a JSON string holds none.
	text := b.Bytes()
	if inline {
		text = bytes.ReplaceAll(text, []byte(",\n"), []byte(", "))
	}
	v, err := hujson.Parse(bytes.ReplaceAll(text, []byte("\n"), []byte(eol)))
	if err != nil {
		return nil, withoutText(err)
	}
	return v.Value, nil
}

// decode returns v as encoding/json decodes it into an empty interface.
// With exact set a number keeps its digits (json.Number) instead of
// becoming a float64, so that two values can be compared without rounding.
func decode(v hujson.Value, exact bool) (any, error) {
	c := v.Clone()
	c.Standardize()
	return parseJSON(c.Pack(), exact)
}

// parseJSON decodes a text that hujson has accepted, so what can still go
// wrong is what encoding/json cannot hold: see decodeProblem.
func parseJSON(text []byte, exact bool) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	if exact {
		dec.UseNumber()
	}
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, decodeProblem(err)
	}
	return out, nil
}

// The two things encoding/json cannot hold of a text that parses.
var (
	errNumberTooLarge = errors.New("a number is too large to read")
	errTooDeep        = errors.New("the values are nested too deeply")
)

// decodeProblem says in plain words why a text that parses could not be
// decoded: a number does not fit into a float64 (only when numbers are not
// kept exact), or the values are nested deeper than encoding/json goes.
// encoding/json's own messages are not passed on: they are written for a Go
// programmer, and they quote the number and the keys that lead to it, which
// are text of the user's file.
func decodeProblem(err error) error {
	var number *json.UnmarshalTypeError
	switch {
	case errors.As(err, &number):
		return errNumberTooLarge
	case strings.Contains(err.Error(), "max depth"):
		return errTooDeep
	}
	return errors.New("the text cannot be decoded")
}

// exact returns value as decode returns it with exact set. A value that
// holds an object with the same key twice is refused: a loader would read
// only one of the two.
func exact(value any) (any, error) {
	raw, err := marshal(value)
	if err != nil {
		return nil, err
	}
	tree, err := hujson.Parse(raw)
	if err != nil {
		return nil, withoutText(err)
	}
	if key, found := repeated(tree.Value, nil); found {
		return nil, fmt.Errorf("%s appears more than once in the value to write", key)
	}
	return parseJSON(raw, true)
}

// exactAll is exact for each of values.
func exactAll(values []any) ([]any, error) {
	out := make([]any, len(values))
	for i, value := range values {
		var err error
		if out[i], err = exact(value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// repeated looks in v, which stands at path, for an object that has the
// same key twice, and returns the dotted path of the second one. An element
// of an array counts as a key that is its index.
func repeated(v hujson.ValueTrimmed, path []string) (string, bool) {
	switch c := v.(type) {
	case *hujson.Object:
		seen := map[string]bool{}
		for _, m := range c.Members {
			name, _ := m.Name.Value.(hujson.Literal)
			here := append(slices.Clip(path), name.String())
			if seen[name.String()] {
				return dotted(here), true
			}
			seen[name.String()] = true
			if key, found := repeated(m.Value.Value, here); found {
				return key, true
			}
		}
	case *hujson.Array:
		for i, e := range c.Elements {
			if key, found := repeated(e.Value, append(slices.Clip(path), strconv.Itoa(i))); found {
				return key, true
			}
		}
	}
	return "", false
}

// equal reports whether two values decoded with exact set are the same
// JSON: objects whatever the order of their keys, numbers by value.
func equal(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && maps.EqualFunc(x, y, equal)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case json.Number:
		y, ok := b.(json.Number)
		return ok && sameNumber(x, y)
	}
	return a == b // a string, a boolean or null
}

// sameNumber compares two JSON numbers by value, to the last digit: 1.0 is
// 1 and 1e2 is 100, but 12345678901234567890 is not 12345678901234567891.
func sameNumber(a, b json.Number) bool {
	if a == b {
		return true
	}
	x, okx := new(big.Rat).SetString(string(a))
	y, oky := new(big.Rat).SetString(string(b))
	return okx && oky && x.Cmp(y) == 0
}

// put returns the decoded object doc with value at path. It copies the
// objects on the way and creates the missing ones; doc itself stays as it
// is.
func put(doc any, path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	old, _ := doc.(map[string]any)
	out := maps.Clone(old)
	if out == nil {
		out = map[string]any{}
	}
	out[path[0]] = put(old[path[0]], path[1:], value)
	return out
}
