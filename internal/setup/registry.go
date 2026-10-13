package setup

// Tools returns the supported tools in the order they are listed.
func Tools() []Tool { return []Tool{Qwen{}, Opencode{}, Codex{}, Claude{}} }

// Lookup returns the tool with the given id.
func Lookup(id string) (Tool, bool) {
	for _, t := range Tools() {
		if t.ID() == id {
			return t, true
		}
	}
	return nil, false
}

// IDs returns the ids of the supported tools.
func IDs() []string {
	var ids []string
	for _, t := range Tools() {
		ids = append(ids, t.ID())
	}
	return ids
}
