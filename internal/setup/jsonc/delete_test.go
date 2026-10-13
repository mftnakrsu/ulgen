package jsonc

import (
	"bytes"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestDelete(t *testing.T) {
	cases := []struct{ name, src, path, want string }{
		{"middle", "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n", "b", "{\n  \"a\": 1,\n  \"c\": 3\n}\n"},
		{"first", "{\n  \"a\": 1,\n  \"b\": 2\n}\n", "a", "{\n  \"b\": 2\n}\n"},
		{"last", "{\n  \"a\": 1,\n  \"b\": 2\n}\n", "b", "{\n  \"a\": 1\n}\n"},
		{"last with a trailing comma", "{\n  \"a\": 1,\n  \"b\": 2,\n}\n", "b", "{\n  \"a\": 1,\n}\n"},
		{"the only member", "{\n  \"a\": 1\n}\n", "a", "{}\n"},
		{"middle of one line", `{"a": 1, "b": 2, "c": 3}`, "b", `{"a": 1, "c": 3}`},
		{"last of one line", `{"a": 1, "b": 2}`, "b", `{"a": 1}`},
		{"nested", "{\n  \"p\": {\n    \"evren\": {\"x\": 1},\n    \"other\": {}\n  },\n  \"m\": \"k\"\n}\n", "p.evren", "{\n  \"p\": {\n    \"other\": {}\n  },\n  \"m\": \"k\"\n}\n"},
		{"nested, the only member", "{\n  \"p\": {\n    \"evren\": {\"x\": 1}\n  },\n  \"m\": \"k\"\n}\n", "p.evren", "{\n  \"p\": {},\n  \"m\": \"k\"\n}\n"},
		{"a comment above stays", "{\n  \"a\": 1,\n  // about b\n  \"b\": 2,\n  \"c\": 3\n}\n", "b", "{\n  \"a\": 1,\n  // about b\n  \"c\": 3\n}\n"},
		{"a comment behind stays", "{\n  \"a\": 1,\n  \"b\": 2, // b here\n  \"c\": 3\n}\n", "b", "{\n  \"a\": 1,\n  // b here\n  \"c\": 3\n}\n"},
		{"a comment inside stays", "{\n  \"a\": 1,\n  \"b\": {\n    // inside\n    \"x\": 1\n  },\n  \"c\": 3\n}\n", "b", "{\n  \"a\": 1,\n  // inside\n  \"c\": 3\n}\n"},
		{"the last member with comments", "{\n  \"a\": 1, // keep a\n  // about b\n  \"b\": 2 // b here\n}\n", "b", "{\n  \"a\": 1 // keep a\n  // about b\n  // b here\n}\n"},
		{"CRLF line breaks", "{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n", "b", "{\r\n  \"a\": 1\r\n}\r\n"},
		{"a key that is not there", "{\n  \"a\": 1\n}\n", "zz", "{\n  \"a\": 1\n}\n"},
		{"a path that is not there", "{\n  \"a\": 1\n}\n", "p.q", "{\n  \"a\": 1\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := mustParse(t, c.src)
			if err := d.Delete(strings.Split(c.path, ".")...); err != nil {
				t.Fatal(err)
			}
			if got := string(d.Bytes()); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
			// A second call finds nothing and changes no byte.
			if err := d.Delete(strings.Split(c.path, ".")...); err != nil || string(d.Bytes()) != c.want {
				t.Errorf("second call: err %v, text %q", err, d.Bytes())
			}
		})
	}
}

func TestDeleteErrors(t *testing.T) {
	for _, c := range []struct{ src, path, want string }{
		{`{"a": 1}`, "a.b", "a is not an object"},
		{`{"a": 1, "a": 2}`, "a", "a appears more than once"},
	} {
		d := mustParse(t, c.src)
		err := d.Delete(strings.Split(c.path, ".")...)
		if err == nil || err.Error() != c.want || string(d.Bytes()) != c.src {
			t.Errorf("%s: err %v, text %q", c.src, err, d.Bytes())
		}
	}
	if err := mustParse(t, `{}`).Delete(); err == nil {
		t.Error("an empty path was accepted")
	}
}

// memberPaths lists the path of every object member of a decoded document.
func memberPaths(v any, path []string, out *[][]string) {
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		p := append(slices.Clone(path), k)
		*out = append(*out, p)
		memberPaths(obj[k], p, out)
	}
}

// Whatever the document looks like, a delete leaves a text that parses,
// decodes to the document without that member and holds every comment the
// document had; a second delete changes nothing.
func TestRandomDeletes(t *testing.T) {
	deleted := 0
	for seed := int64(1); seed <= 3000; seed++ {
		r := rand.New(rand.NewSource(seed))
		src := randomDocument(r)
		d, err := Parse([]byte(src))
		if err != nil {
			continue
		}
		before, ok := d.Get()
		if !ok {
			continue
		}
		var paths [][]string
		memberPaths(before, nil, &paths)
		if len(paths) == 0 {
			continue
		}
		path := paths[r.Intn(len(paths))]
		root, _ := hujson.Parse([]byte(src))
		had := comments(&root)
		if err := d.Delete(path...); err != nil {
			// The generator writes a key twice now and then; that is refused.
			if !strings.HasSuffix(err.Error(), "appears more than once") {
				t.Fatalf("seed %d: Delete(%v): %v\n%s", seed, path, err, src)
			}
			if string(d.Bytes()) != src {
				t.Fatalf("seed %d: a refused delete changed the text", seed)
			}
			continue
		}
		deleted++
		out := d.Bytes()
		again, err := Parse(out)
		if err != nil {
			t.Fatalf("seed %d: the result does not parse: %v\n%s\n---\n%s", seed, err, src, out)
		}
		got, _ := again.Get()
		if !equal(got, without(before, path)) {
			t.Fatalf("seed %d: Delete(%v) left another value\n%s\n---\n%s", seed, path, src, out)
		}
		after, _ := hujson.Parse(out)
		if !slices.Equal(comments(&after), had) {
			t.Fatalf("seed %d: Delete(%v) lost or repeated a comment\n%s\n---\n%s", seed, path, src, out)
		}
		if err := d.Delete(path...); err != nil || !bytes.Equal(d.Bytes(), out) {
			t.Fatalf("seed %d: a second Delete(%v) changed the text (err %v)", seed, path, err)
		}
	}
	if deleted < 1000 {
		t.Errorf("only %d deletes were tried", deleted)
	}
}
