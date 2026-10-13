// Package evrenworkers carries the Claude Code skill for EVREN workers:
// skills/evren-workers/SKILL.md, the text "ulgen setup claude" installs into
// the user's Claude directory and embeds it into the Ülgen binary.
package evrenworkers

import (
	"bytes"
	_ "embed"
)

//go:embed skills/evren-workers/SKILL.md
var skill []byte

// Skill returns the SKILL.md text as it is embedded, or nil when this build
// carries none (an empty file). The bytes are the caller's own copy.
func Skill() []byte {
	if len(skill) == 0 {
		return nil
	}
	return bytes.Clone(skill)
}
