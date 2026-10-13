package jsonc

import (
	"maps"
	"slices"

	"github.com/tailscale/hujson"
)

// Delete removes the member at a path of object keys, together with one
// comma. Everything else stays byte for byte: the comments in front of the
// member, inside it and behind it stay where it was, and the member's line
// goes only when nothing else is on it. A path that leads to no member
// changes nothing. The errors of Set about the keys on the way apply, and
// after an error the document is as it was.
func (d *Doc) Delete(path ...string) error {
	w, s, err := d.open(path)
	if err != nil {
		return err
	}
	if s.member < 0 || s.depth != len(path)-1 {
		return nil
	}
	w.removeMember(s)
	return d.commit(w, without(w.decoded, path))
}

// removeMember takes the member s points at out of its object, with one
// comma. It is remove for an object: the comments around the member and
// inside it stay where it was.
func (w *draft) removeMember(s spot) {
	obj, i := s.obj, s.member
	m, last := obj.Members[i], len(obj.Members)-1
	left := scan(m.Name.BeforeExtra)
	_, ownLine := indentOf(left)
	// The comments between the name and the value go the way of the ones
	// inside the value.
	inner := hujson.Value{BeforeExtra: join(scan(m.Name.AfterExtra), scan(m.Value.BeforeExtra)), Value: m.Value.Value}
	left = append(left, w.doc.lift(commentsIn(&inner), s.child, ownLine)...)
	if i < last {
		// The comma behind it goes; what stood around it leads up to the next member.
		next := &obj.Members[i+1].Name.BeforeExtra
		*next = join(closeGap(left, append(scan(m.Value.AfterExtra), scan(*next)...)))
	} else {
		// The comma in front of it goes, or its own when the object ends with one.
		right := append(scan(m.Value.AfterExtra), scan(obj.AfterExtra)...)
		obj.AfterExtra = join(handOverMember(obj.Members[:i], m.Value.AfterExtra != nil), leave(left, right))
	}
	obj.Members = slices.Delete(obj.Members, i, i+1)
	if len(obj.Members) == 0 && !slices.ContainsFunc(scan(obj.AfterExtra), token.comment) {
		obj.AfterExtra = nil // nothing left: "{}"
	}
}

// handOverMember is handOver for an object: the last of rest becomes the
// last member after the one behind it is gone.
func handOverMember(rest []hujson.ObjectMember, trailingComma bool) []token {
	if len(rest) == 0 {
		return nil
	}
	prev := &rest[len(rest)-1].Value
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

// without returns the decoded object doc without the key at path. It copies
// the objects on the way; doc itself stays as it is.
func without(doc any, path []string) any {
	old, ok := doc.(map[string]any)
	if !ok || len(path) == 0 {
		return doc
	}
	out := maps.Clone(old)
	if len(path) == 1 {
		delete(out, path[0])
		return out
	}
	if _, has := old[path[0]]; has {
		out[path[0]] = without(old[path[0]], path[1:])
	}
	return out
}
