package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSkill is a stand-in for the embedded SKILL.md: front matter, then the
// marker line, then a body.
func claudeTestSkill() []byte {
	return []byte("---\nname: evren-workers\ndescription: x\n---\n" +
		"<!-- installed by ulgen setup; \"ulgen setup claude\" rewrites this file -->\n\n# EVREN workers\n\nbody\n")
}

// claudeSkillPathIn returns the path the plan writes the skill to under home.
func claudeSkillPathIn(home string) string {
	return filepath.Join(home, ".claude", "skills", "evren-workers", "SKILL.md")
}

// The adapter finds the binary, reports the tool as set up when the binary
// is installed and the skill is there with Ülgen's marker, writes the skill
// into the request's text, hands the arguments through, and says how to
// start the tool. A request no adapter can use is refused the same way here.
func TestClaudeAdapter(t *testing.T) {
	if got := (Claude{}).ID(); got != "claude" {
		t.Errorf("ID = %q", got)
	}
	if got := (Claude{}).Name(); got != "Claude Code" {
		t.Errorf("Name = %q", got)
	}
	if !(Claude{}).Proxied() {
		t.Error("Proxied() = false")
	}

	// Not installed: no binary, so not set up and no skill.
	empty := testEnv(t, nil, nil)
	if found := (Claude{}).Detect(context.Background(), empty); found.Installed {
		t.Errorf("Detect on a machine without claude = %+v", found)
	}
	if ok, err := (Claude{}).Configured(empty); ok || err != nil {
		t.Errorf("Configured without the tool = %v, %v", ok, err)
	}
	req := testRequest()
	req.ClaudeSkill = claudeTestSkill()
	p, err := (Claude{}).Plan(empty, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := planned(t, p, claudeSkillPathIn(empty.Home)); got != string(req.ClaudeSkill) {
		t.Errorf("the planned skill is not the request's text:\n%s", got)
	}
	if !hasNote(p, "setup.note.claude_not_supported") {
		t.Errorf("Plan notes = %v", p.Notes)
	}

	// Installed: the binary and its version are found, but the skill is not
	// there yet, so the tool is not set up. A build without the skill text
	// writes nothing.
	env := testEnv(t, nil, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	found := (Claude{}).Detect(context.Background(), env)
	if !found.Installed || found.Binary != testBin("claude") || found.Version != "1.0.3" {
		t.Errorf("Detect = %+v", found)
	}
	if ok, err := (Claude{}).Configured(env); ok || err != nil {
		t.Errorf("Configured without the skill = %v, %v", ok, err)
	}
	if hint := (Claude{}).InstallHint(env); hint.Key != "setup.install.claude" {
		t.Errorf("InstallHint = %+v", hint)
	}
	if p, err := (Claude{}).Plan(env, testRequest()); err != nil || len(p.Files) != 0 {
		t.Errorf("Plan without a skill text = %v, files %v", err, paths(p))
	}
	// A request no adapter can use is refused, with no note and no file.
	bad := testRequest()
	bad.Models = nil
	var re *RequestError
	if _, err := (Claude{}).Plan(env, bad); !errors.As(err, &re) {
		t.Errorf("Plan with a bad request: %v", err)
	}

	// The arguments reach the tool unchanged.
	for _, args := range [][]string{nil, {"-p", "hi"}, {"--", "-x"}, {"--model", "x"}} {
		got := (Claude{}).Launch(env, args)
		if len(got) != len(args) {
			t.Errorf("Launch(%q) = %q", args, got)
			continue
		}
		for i := range args {
			if got[i] != args[i] {
				t.Errorf("Launch(%q) = %q", args, got)
				break
			}
		}
	}
}

// The skill file is created when it is absent, a second plan changes
// nothing, a changed text updates the file, and a SKILL.md that is not
// Ülgen's is left alone with a note.
func TestClaudeSkillPlan(t *testing.T) {
	env := testEnv(t, nil, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	req := testRequest()
	req.ClaudeSkill = claudeTestSkill()
	path := claudeSkillPathIn(env.Home)

	p, err := (Claude{}).Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 || p.Files[0].Old != nil || p.Files[0].Path != path {
		t.Fatalf("first plan = %v, old %v", paths(p), p.Files[0].Old)
	}

	// Apply, plan again: nothing left to write, and the tool is set up.
	if err := Apply(p, "claude", filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	again, err := (Claude{}).Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 {
		t.Errorf("a second plan still changes %v", paths(again))
	}
	if ok, err := (Claude{}).Configured(env); !ok || err != nil {
		t.Errorf("Configured after the skill is written = %v, %v", ok, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(req.ClaudeSkill) {
		t.Errorf("the skill on disk is not the request's text: %v\n%s", err, got)
	}

	// A changed text updates the file with the marker still on it.
	req.ClaudeSkill = []byte(strings.Replace(string(req.ClaudeSkill), "body", "new body", 1))
	p, err = (Claude{}).Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := planned(t, p, path); !strings.Contains(got, "new body") {
		t.Errorf("the update does not carry the new text:\n%s", got)
	}

	// A user's own SKILL.md (no marker) is left alone, with a note.
	own := testEnv(t, nil, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	write(t, claudeSkillPathIn(own.Home), "# my own skill\n")
	p, err = (Claude{}).Plan(own, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 0 || !hasNote(p, "setup.note.skill_foreign") {
		t.Errorf("a foreign skill: files %v, notes %v", paths(p), p.Notes)
	}
	if ok, err := (Claude{}).Configured(own); ok || err != nil {
		t.Errorf("Configured with a foreign skill = %v, %v", ok, err)
	}
}

// The Claude directory follows CLAUDE_CONFIG_DIR when it is set and is
// absolute, and a relative one is refused like the other tools' variables.
func TestClaudeDir(t *testing.T) {
	other := t.TempDir()
	env := testEnv(t, map[string]string{"CLAUDE_CONFIG_DIR": other}, nil)
	req := testRequest()
	req.ClaudeSkill = claudeTestSkill()
	p, err := (Claude{}).Plan(env, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(p); len(got) != 1 || got[0] != filepath.Join(other, "skills", "evren-workers", "SKILL.md") {
		t.Errorf("with CLAUDE_CONFIG_DIR the plan writes %v", got)
	}

	rel := testEnv(t, map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join("rel", "claude")}, map[string]string{"claude": "1.0.3\n"})
	var rpe *RelativePathError
	if ok, err := (Claude{}).Configured(rel); ok || !errors.As(err, &rpe) || rpe.Var != "CLAUDE_CONFIG_DIR" {
		t.Errorf("a relative CLAUDE_CONFIG_DIR: Configured = %v, %v", ok, err)
	}
	if p, err := (Claude{}).Plan(rel, req); !errors.As(err, &rpe) || len(p.Files) != 0 {
		t.Errorf("a relative CLAUDE_CONFIG_DIR: Plan = %v, files %v", err, paths(p))
	}
}

// Configured needs the marker, not merely a file: a SKILL.md without the
// phrase, or one whose phrase is not an HTML comment, is not Ülgen's.
func TestClaudeOwnSkill(t *testing.T) {
	for content, want := range map[string]bool{
		string(claudeTestSkill()): true,
		"\xef\xbb\xbf---\nname: x\n---\n<!-- installed by ulgen setup; keep -->\n": true,
		"# my own skill\n":           false,
		"installed by ulgen setup\n": false, // not a comment
		"":                           false,
	} {
		if got := claudeOwnSkill([]byte(content)); got != want {
			t.Errorf("claudeOwnSkill(%q) = %v, want %v", content, got, want)
		}
	}
}
