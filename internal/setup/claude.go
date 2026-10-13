package setup

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
)

// Claude sets up Claude Code (github.com/anthropics/claude-code).
//
// Claude Code speaks only the Anthropic Messages API and reads nothing that
// could point it at EVREN, so there is no provider file to write. It is
// instead started by "ulgen run claude", which runs Ülgen's translator on
// this machine and gives the tool its local address and a token of its own
// (see the Proxied interface and cmd/ulgen/child.go). The key stays in
// Ülgen: the tool never sees it.
//
// The one file Ülgen writes for Claude Code is the EVREN workers skill,
// <Claude directory>/skills/evren-workers/SKILL.md, which teaches the
// orchestrating session when and how to hand a task to a worker. Its text
// comes in through the request (Request.ClaudeSkill).
type Claude struct{}

const (
	// claudeSkillPhrase marks a SKILL.md as one Ülgen installed. It stands in
	// an HTML comment among the file's first lines.
	claudeSkillPhrase = "installed by ulgen setup"

	// claudeSkillHeadLines is how many lines of a SKILL.md, blank ones not
	// counted, are searched for claudeSkillPhrase. The file begins with YAML
	// front matter, so the marker is not the very first line.
	claudeSkillHeadLines = 10
)

// claudeDir is Claude Code's directory: $CLAUDE_CONFIG_DIR when set, else
// ~/.claude. It is always an absolute path: an unknown home directory is a
// *HomeError, and a CLAUDE_CONFIG_DIR that is relative is a
// *RelativePathError (as the other tools' variables are; see dirVars).
func claudeDir(env Env) (string, error) {
	if d := env.getenv("CLAUDE_CONFIG_DIR"); d != "" {
		if !filepath.IsAbs(d) {
			return "", &RelativePathError{Var: "CLAUDE_CONFIG_DIR", Value: d}
		}
		return d, nil
	}
	home, err := env.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// claudeSkillPath is the file "ulgen setup claude" writes: the skill in
// <Claude directory>/skills/evren-workers.
func claudeSkillPath(dir string) string {
	return filepath.Join(dir, "skills", "evren-workers", "SKILL.md")
}

// claudeOwnSkill reports whether a SKILL.md is one Ülgen installed: among
// its first lines is an HTML comment with claudeSkillPhrase. A byte-order
// mark and blank lines in front do not matter.
func claudeOwnSkill(content []byte) bool {
	head := bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	seen := 0
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if seen++; seen > claudeSkillHeadLines {
			break
		}
		if strings.HasPrefix(line, "<!--") && strings.Contains(line, claudeSkillPhrase) {
			return true
		}
	}
	return false
}

// ID is the name the tool has on Ülgen's command line: "ulgen setup claude".
func (Claude) ID() string { return "claude" }

// Name is the tool's name as it is shown to the user.
func (Claude) Name() string { return "Claude Code" }

// Detect looks for the binary "claude" on PATH and asks it for its version.
func (Claude) Detect(ctx context.Context, env Env) Detection { return detect(ctx, env, "claude") }

// Configured reports whether Claude Code is installed and its skill file is
// there and carries Ülgen's marker. The marker is as much as Configured can
// know: the installed text is the build's, not the file's, and comparing it
// would mean the adapter holding the skill, which does not have it. So the
// list shows "set up" once "ulgen setup claude" has written the skill and
// the binary is found. A file without the marker is the user's own and is
// not counted. (Claude Code's own Configured reads the binary too, because
// the design's list counts it only when both are there.)
func (Claude) Configured(env Env) (bool, error) {
	if !(Claude{}).Detect(context.Background(), env).Installed {
		return false, nil
	}
	return claudeSkillConfigured(env)
}

// claudeSkillConfigured reports whether the skill file is there and carries
// Ülgen's marker, without looking for the binary (Plan uses the same rule).
func claudeSkillConfigured(env Env) (bool, error) {
	dir, err := claudeDir(env)
	if err != nil {
		return false, err
	}
	content, err := readRegular(claudeSkillPath(dir))
	if err != nil {
		return false, err
	}
	return len(content) > 0 && claudeOwnSkill(content), nil
}

// Plan returns the skill file to write, and the notes the user should know:
// that Anthropic does not support running Claude Code this way, and how to
// start it. The skill text comes in through the request. A SKILL.md that is
// there and is not Ülgen's (claudeOwnSkill) is the user's own and is left
// alone, with a note. A build without the skill text changes nothing. The
// request is checked as every adapter's is, so a request no adapter can use
// is refused in the same way here.
func (Claude) Plan(env Env, req Request) (Plan, error) {
	var plan Plan
	if err := req.check(); err != nil {
		return plan, err
	}
	if err := claudeInstallSkill(&plan, env, req.ClaudeSkill); err != nil {
		return plan, err
	}
	// The note is the design's step-6 notice; "setup.start" (added by the
	// command) gives the "Start it with:  ulgen run claude" line, so there is
	// no separate note for it here.
	plan.note("setup.note.claude_not_supported")
	return plan, nil
}

// Remove removes the skill file when it carries Ülgen's marker. A SKILL.md
// without it is the user's own and stays, with a note.
func (Claude) Remove(env Env) (Plan, error) {
	var plan Plan
	dir, err := claudeDir(env)
	if err != nil {
		return plan, err
	}
	skill := claudeSkillPath(dir)
	old, err := readRegular(skill)
	switch {
	case err != nil || len(old) == 0:
		return plan, err
	case !claudeOwnSkill(old):
		plan.note("setup.note.skill_foreign", Path(skill))
	default:
		plan.remove(skill, old)
	}
	return plan, nil
}

// claudeInstallSkill plans <Claude directory>/skills/evren-workers/SKILL.md.
// A SKILL.md that is there and is not Ülgen's is the user's own: it is left
// alone, with a note. The text is not changed (not even whitespace): a file
// that already holds it is not written again. A build that carries no skill
// (an empty text) writes nothing: it cannot know what to put there.
func claudeInstallSkill(plan *Plan, env Env, text []byte) error {
	if len(bytes.TrimSpace(text)) == 0 {
		return nil // this build carries no skill
	}
	dir, err := claudeDir(env)
	if err != nil {
		return err
	}
	skill := claudeSkillPath(dir)
	if err := wantDir(filepath.Join(dir, "skills")); err != nil {
		return err
	}
	if err := wantDir(dir); err != nil {
		return err
	}
	old, err := readRegular(skill)
	if err != nil {
		return err
	}
	if len(old) > 0 && !claudeOwnSkill(old) {
		plan.note("setup.note.skill_foreign", Path(skill))
		return nil
	}
	plan.change(skill, old, text)
	return nil
}

// Launch returns the arguments unchanged: "ulgen run" gives Claude Code its
// environment, not its command line.
func (Claude) Launch(_ Env, args []string) []string { return args }

// InstallHint tells the user how to install Claude Code.
func (Claude) InstallHint(Env) Note { return Note{Key: "setup.install.claude"} }

// Proxied reports that "ulgen run" starts Claude Code through the
// translator.
func (Claude) Proxied() bool { return true }
