package evrenworkers

import (
	"strings"
	"testing"
)

// marker is the line "ulgen setup claude" writes as the first line after the
// front matter of the SKILL.md it installs; it marks the file as Ülgen's.
const marker = `<!-- installed by ulgen setup; "ulgen setup claude" rewrites this file -->`

// The skill is a Markdown file with front matter: "name" and "description"
// between the opening and closing "---", and the marker line immediately
// after the closing "---". A SKILL.md that does not carry the marker is the
// user's own and setup leaves it alone, so the marker must be exactly there.
func TestSkillFrontMatterAndMarker(t *testing.T) {
	lines := strings.Split(strings.TrimRight(string(Skill()), "\n"), "\n")
	if len(lines) < 4 || lines[0] != "---" {
		t.Fatalf("the skill does not start with front matter: %q", first(lines, 3))
	}
	close := -1
	var name, description string
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			close = i
			break
		}
		if key, value, ok := strings.Cut(lines[i], ":"); ok {
			switch strings.TrimSpace(key) {
			case "name":
				name = strings.TrimSpace(value)
			case "description":
				description = strings.TrimSpace(value)
			}
		}
	}
	if close < 0 {
		t.Fatal("the front matter is not closed with ---")
	}
	if name != "evren-workers" {
		t.Errorf("front matter name = %q, want %q", name, "evren-workers")
	}
	if description == "" {
		t.Error("the front matter has no description")
	}
	if got := lines[close+1]; got != marker {
		t.Errorf("the line after the front matter is %q, want the marker %q", got, marker)
	}
}

// first returns up to n lines for an error message.
func first(lines []string, n int) []string {
	if len(lines) < n {
		return lines
	}
	return lines[:n]
}
