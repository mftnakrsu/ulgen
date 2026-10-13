package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remove plans a tool's removal, applies it and checks that a second plan
// has nothing left to do. It returns the first plan.
func remove(t *testing.T, tool Tool, env Env) Plan {
	t.Helper()
	p, err := tool.Remove(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(p, tool.ID(), filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	again, err := tool.Remove(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 {
		t.Errorf("a second removal still changes %v", paths(again))
	}
	if ok, err := tool.Configured(env); ok || err != nil {
		t.Errorf("Configured after the removal = %v, %v", ok, err)
	}
	return p
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s is still there (%v)", path, err)
	}
}

func TestRemoveOnAMachineWithNothing(t *testing.T) {
	env := testEnv(t, nil, nil)
	for _, tool := range Tools() {
		p, err := tool.Remove(env)
		if err != nil || len(p.Files) != 0 {
			t.Errorf("%s: files %v, err %v", tool.ID(), paths(p), err)
		}
	}
	if entries, _ := os.ReadDir(env.Home); len(entries) != 0 {
		t.Errorf("the home directory is no longer empty: %v", entries)
	}
}

// The user's file comes back byte for byte: their entry, their default,
// their comments.
func TestQwenRemoveGivesTheUsersFileBack(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := write(t, qwenSettings(env), qwenExisting)
	setUp(t, Qwen{}, env, testRequest())
	if content(t, path) == qwenExisting {
		t.Fatal("setup changed nothing")
	}
	remove(t, Qwen{}, env)
	if got := content(t, path); got != qwenExisting {
		t.Errorf("settings.json after the removal:\n%s", got)
	}
}

func TestOpencodeRemove(t *testing.T) {
	env := testEnv(t, nil, nil)
	path := filepath.Join(env.Home, ".config", "opencode", "opencode.json")
	const users = `{
  // mine
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "local": { "npm": "@ai-sdk/openai-compatible" } // keep
  },
  "model": "local/llama"
}
`
	write(t, path, users)
	setUp(t, Opencode{}, env, testRequest())
	remove(t, Opencode{}, env)
	if got := content(t, path); got != users {
		t.Errorf("opencode.json after the removal:\n%s", got)
	}

	// A file Ülgen made: the provider and both defaults go.
	fresh := testEnv(t, nil, nil)
	freshPath := filepath.Join(fresh.Home, ".config", "opencode", "opencode.json")
	setUp(t, Opencode{}, fresh, testRequest())
	remove(t, Opencode{}, fresh)
	doc := decoded(t, content(t, freshPath))
	if doc["provider"] != nil || doc["model"] != nil || doc["small_model"] != nil || doc["$schema"] == nil {
		t.Errorf("opencode.json = %v", doc)
	}
}

func TestCodexRemove(t *testing.T) {
	env := testEnv(t, nil, nil)
	config := write(t, filepath.Join(env.Home, ".codex", "config.toml"), "model = \"gpt-5\"\n")
	setUp(t, Codex{}, env, testRequest())
	profile := filepath.Join(env.Home, ".codex", "evren.config.toml")
	p := remove(t, Codex{}, env)
	gone(t, profile)
	if content(t, config) != "model = \"gpt-5\"\n" {
		t.Error("config.toml was touched")
	}
	if len(p.Files) != 1 || !p.Files[0].Delete || p.Files[0].Old == nil {
		t.Errorf("plan = %+v", p.Files)
	}

	// A profile of that name that is not Ülgen's stays.
	other := testEnv(t, nil, nil)
	own := write(t, filepath.Join(other.Home, ".codex", "evren.config.toml"), "model = \"mine\"\n")
	if p, err := (Codex{}).Remove(other); err != nil || len(p.Files) != 0 {
		t.Errorf("files %v, err %v", paths(p), err)
	}
	if content(t, own) != "model = \"mine\"\n" {
		t.Error("the user's profile was touched")
	}
}

func TestClaudeRemove(t *testing.T) {
	env := testEnv(t, nil, map[string]string{"claude": "2.1.0\n"})
	req := testRequest()
	req.ClaudeSkill = []byte("---\nname: evren-workers\n---\n" + "<!-- " + claudeSkillPhrase + " -->\ntext\n")
	setUp(t, Claude{}, env, req)
	skill := claudeSkillPath(filepath.Join(env.Home, ".claude"))
	remove(t, Claude{}, env)
	gone(t, skill)
	gone(t, filepath.Dir(skill)) // skills/evren-workers held nothing else
	if _, err := os.Stat(filepath.Join(env.Home, ".claude", "skills")); err != nil {
		t.Errorf("the skills directory is gone: %v", err)
	}

	write(t, skill, "my own skill\n")
	p, err := (Claude{}).Remove(env)
	if err != nil || len(p.Files) != 0 || !hasNote(p, "setup.note.skill_foreign") {
		t.Errorf("files %v notes %v err %v", paths(p), p.Notes, err)
	}
}

// A file that is removed is backed up first, a symbolic link loses the link
// and keeps its target, and a file that changed after the plan was made
// stays.
func TestApplyDelete(t *testing.T) {
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	path := write(t, filepath.Join(dir, "sub", "file"), "old\n")
	plan := Plan{Files: []FileChange{{Path: path, Old: []byte("old\n"), Delete: true}}}
	if err := Apply(plan, "tool", backups); err != nil {
		t.Fatal(err)
	}
	gone(t, path)
	gone(t, filepath.Dir(path))
	if got := content(t, BackupPath(backups, "tool", path)); got != "old\n" {
		t.Errorf("backup = %q", got)
	}

	changed := write(t, filepath.Join(dir, "changed"), "new content\n")
	err := Apply(Plan{Files: []FileChange{{Path: changed, Old: []byte("old\n"), Delete: true}}}, "tool2", backups)
	var we *WriteError
	if !errors.As(err, &we) || content(t, changed) != "new content\n" {
		t.Errorf("err = %v", err)
	}

	target := write(t, filepath.Join(dir, "target"), "old\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symbolic links here")
	}
	if err := Apply(Plan{Files: []FileChange{{Path: link, Old: []byte("old\n"), Delete: true}}}, "tool3", backups); err != nil {
		t.Fatal(err)
	}
	gone(t, link)
	if content(t, target) != "old\n" {
		t.Error("the link's target was removed")
	}
}

// On a machine where Ülgen wrote everything, nothing of it is left: no
// entry, and no default that would name a model that is gone.
func TestQwenRemoveAfterAFreshSetup(t *testing.T) {
	env := testEnv(t, nil, nil)
	setUp(t, Qwen{}, env, testRequest())
	remove(t, Qwen{}, env)
	doc := decoded(t, content(t, qwenSettings(env)))
	for _, path := range [][]string{{"modelProviders"}, {"model"}, {"security"}} {
		if v := dig(doc, path...); v != nil {
			t.Errorf("%s is still there: %v", strings.Join(path, "."), v)
		}
	}
	// What cannot be told from the user's own choice stays.
	if dig(doc, "privacy", "usageStatisticsEnabled") != false || dig(doc, "$version") == nil {
		t.Errorf("settings = %v", doc)
	}
}

func TestQwenRemoveLeavesTheUsersDefault(t *testing.T) {
	env := testEnv(t, nil, nil)
	setUp(t, Qwen{}, env, testRequest())
	// The user moved on to a model of their own.
	settings := content(t, qwenSettings(env))
	write(t, qwenSettings(env), strings.Replace(settings, `"name": "deepseek-v4.1-flash"`, `"name": "my-model"`, 1))
	remove(t, Qwen{}, env)
	doc := decoded(t, content(t, qwenSettings(env)))
	if dig(doc, "model", "name") != "my-model" || dig(doc, "security", "auth", "selectedType") != "openai" {
		t.Errorf("the user's default was touched: %v", doc)
	}
	if dig(doc, "modelProviders") != nil {
		t.Errorf("entries are left: %v", dig(doc, "modelProviders"))
	}
}
