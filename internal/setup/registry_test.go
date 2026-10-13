package setup

import (
	"reflect"
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	if got := strings.Join(IDs(), " "); got != "qwen opencode codex claude" {
		t.Errorf("IDs = %q", got)
	}
	if tool, ok := Lookup("codex"); !ok || tool.Name() != "Codex CLI" {
		t.Errorf("Lookup(codex) = %v, %v", tool, ok)
	}
	for _, id := range []string{"cursor", "", "Codex", " codex", "codex "} {
		if tool, ok := Lookup(id); ok || tool != nil {
			t.Errorf("Lookup(%q) = %v, %v", id, tool, ok)
		}
	}
}

// Every tool has an id of its own, Lookup finds each tool by its id, and IDs
// lists the ids in the order of Tools.
func TestRegistryIsConsistent(t *testing.T) {
	tools := Tools()
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
	seen := map[string]bool{}
	var ids []string
	for _, tool := range tools {
		id := tool.ID()
		if id == "" || strings.TrimSpace(id) != id || strings.ToLower(id) != id {
			t.Errorf("%T has the id %q", tool, id)
		}
		if seen[id] {
			t.Errorf("the id %q is used twice", id)
		}
		seen[id] = true
		ids = append(ids, id)
		if tool.Name() == "" {
			t.Errorf("%s has no name", id)
		}
		found, ok := Lookup(id)
		if !ok || found != tool {
			t.Errorf("Lookup(%q) = %v, %v; want %T", id, found, ok, tool)
		}
	}
	if got := IDs(); !reflect.DeepEqual(got, ids) {
		t.Errorf("IDs() = %v; Tools() are %v", got, ids)
	}
}
