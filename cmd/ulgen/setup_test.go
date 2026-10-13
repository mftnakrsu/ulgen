package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// testBin is where the test machine keeps its programs: an absolute path on
// the system the tests run on (/usr/bin, or C:\usr\bin on Windows), because
// a tool is only detected at an absolute path.
func testBin(name string) string {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	return filepath.Join(root, "usr", "bin", name)
}

// seeded remembers, per test, the files a test put into the home directory
// itself and what it put there (see seedFile).
var seeded = map[*testing.T]map[string]string{}

// keepKey records, per test, files that are allowed to end a run holding the
// key: the key was put there by the user on purpose and Ülgen does not remove
// it. Only the test that checks what is said about such a file uses it.
var keepKey = map[*testing.T]map[string]bool{}

// allowKey exempts path from the shared check that no file keeps the key.
func allowKey(t *testing.T, path string) {
	t.Helper()
	if keepKey[t] == nil {
		keepKey[t] = map[string]bool{}
	}
	keepKey[t][path] = true
}

// seedFile writes a file as the user would have left it. Such a file may
// hold a key in clear text; the check for leaked keys skips it for as long
// as Ülgen has not rewritten it.
func seedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if seeded[t] == nil {
		seeded[t] = map[string]string{}
	}
	seeded[t][path] = content
}

// useSetupEnv gives the setup commands a machine with an empty home
// directory. bins maps a binary name to what "<binary> --version" prints.
// The working directory is <home>/work, so nothing looks at a directory of
// the real machine. When the test ends, no file under the home directory may
// hold the key (the shared check, assertNoKey), except a file the test
// seeded itself and Ülgen did not rewrite.
func useSetupEnv(t *testing.T, bins map[string]string) setup.Env {
	t.Helper()
	env := setup.Env{
		GOOS: "linux",
		Home: t.TempDir(),
		LookPath: func(name string) (string, error) {
			if _, ok := bins[name]; ok {
				return testBin(name), nil
			}
			return "", errors.New("not found")
		},
		Version: func(_ context.Context, binary string) (string, error) {
			return bins[filepath.Base(binary)], nil
		},
	}
	work := filepath.Join(env.Home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	useEnv(t, env)
	savedDir := workingDir
	workingDir = func() (string, error) { return work, nil }
	t.Cleanup(func() { workingDir = savedDir })
	t.Cleanup(func() {
		assertNoKeyInFiles(t, env.Home)
		delete(seeded, t)
	})
	return env
}

// useEnv makes env the machine the setup commands see. The EVREN workers skill stays the real embedded text,
// so "ulgen setup claude" writes a file a test can look at.
func useEnv(t *testing.T, env setup.Env) {
	t.Helper()
	savedEnv := setupEnv
	setupEnv = func(getenv func(string) string) setup.Env {
		env.Getenv = getenv
		return env
	}
	t.Cleanup(func() { setupEnv = savedEnv })
}

// assertNoKeyInFiles applies the shared key check to every file under dir.
func assertNoKeyInFiles(t *testing.T, dir string) {
	t.Helper()
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil // a test made it unreadable on purpose
		}
		if content, ok := seeded[t][path]; ok && content == string(b) {
			return nil
		}
		if keepKey[t][path] {
			return nil
		}
		assertNoKey(t, "file "+path, string(b), testKey)
		return nil
	})
}

// signedIn stores the test key and returns the variables every setup test
// passes: the directory Ülgen keeps its backups in.
func signedIn(t *testing.T) map[string]string {
	t.Helper()
	if err := useFileStore(t).Set("evren", testKey); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"ULGEN_CONFIG_DIR": t.TempDir()}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupList(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"codex": "codex-cli 0.162.1\n"})
	seedFile(t, filepath.Join(env.Home, ".qwen", "settings.json"), "{ broken")
	code, stdout, stderr := exec(t, context.Background(), nil, "setup")
	want := "Tool       Installed   EVREN\n" +
		"qwen       not found   its file cannot be read\n" +
		"opencode   not found   not set up\n" +
		"codex      0.162.1     not set up\n" +
		"claude     not found   not set up\n" +
		"\nSet a tool up with:  ulgen setup <tool>\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	_, stdout, _ = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "setup")
	for _, w := range []string{"Araç       Kurulu       EVREN", "opencode   bulunamadı   ayarlı değil", "Bir aracı ayarlamak için:  ulgen setup <araç>"} {
		if !strings.Contains(stdout, w) {
			t.Errorf("Turkish list lacks %q:\n%s", w, stdout)
		}
	}
}

// A tool whose files are there has one of four states in the list: set up,
// not set up, a file that cannot be read, and a file that can be read but
// that setup would refuse as it is. For the last one the list says "needs
// attention", and "ulgen setup <tool>" has the full message.
func TestSetupListStates(t *testing.T) {
	env := useSetupEnv(t, nil)
	qwen := filepath.Join(env.Home, ".qwen", "settings.json")
	opencode := filepath.Join(env.Home, ".config", "opencode", "opencode.json")
	codex := filepath.Join(env.Home, ".codex", "config.toml")
	list := func(lang string) string {
		t.Helper()
		code, stdout, stderr := exec(t, context.Background(), map[string]string{"LANG": lang}, "setup")
		if code != 0 || stderr != "" {
			t.Fatalf("code %d stderr %q", code, stderr)
		}
		return stdout
	}
	// A provider of the user's own under EVREN's name, a settings file in
	// the older format, a profile selected in the old way.
	seedFile(t, qwen, `{"theme": "Dracula"}`)
	seedFile(t, opencode, `{"provider": {"evren": {"name": "Evren Corp gateway"}}}`)
	seedFile(t, codex, "profile = \"work\"\n")
	want := "Tool       Installed   EVREN\n" +
		"qwen       not found   needs attention\n" +
		"opencode   not found   needs attention\n" +
		"codex      not found   needs attention\n" +
		"claude     not found   not set up\n" +
		"\nSet a tool up with:  ulgen setup <tool>\n"
	if got := list(""); got != want {
		t.Errorf("stdout:\n%s", got)
	}
	if got := list("tr_TR.UTF-8"); strings.Count(got, "bulunamadı   dikkat gerekiyor\n") != 3 {
		t.Errorf("Turkish:\n%s", got)
	}
	// The other three states, one tool each: set up, not set up (an entry of
	// the user's own that only has EVREN's address), unreadable.
	seedFile(t, qwen, `{"modelProviders": {"openai": [{"id": "m", "envKey": "MY_KEY", "baseUrl": "https://evren-llmapi.ssyz.org.tr/v1"}]}}`)
	seedFile(t, opencode, `{"provider": {"evren": {"options": {"apiKey": "{env:EVREN_API_KEY}"}}}}`)
	seedFile(t, codex, "profile = ")
	want = "Tool       Installed   EVREN\n" +
		"qwen       not found   not set up\n" +
		"opencode   not found   set up\n" +
		"codex      not found   its file cannot be read\n" +
		"claude     not found   not set up\n" +
		"\nSet a tool up with:  ulgen setup <tool>\n"
	if got := list(""); got != want {
		t.Errorf("stdout:\n%s", got)
	}
	// A directory in the file's place is a file that cannot be read.
	os.Remove(opencode)
	if err := os.MkdirAll(filepath.Join(opencode, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := list(""); !strings.Contains(got, "opencode   not found   its file cannot be read\n") {
		t.Errorf("stdout:\n%s", got)
	}
	for err, want := range map[error]string{
		&setup.ForeignEntryError{}:  "setup.list.attention",
		&setup.LegacyProfileError{}: "setup.list.attention",
		&setup.ShapeError{}:         "setup.list.attention",
		&setup.ParseError{}:         "setup.list.unreadable",
		&setup.KindError{}:          "setup.list.unreadable",
		os.ErrPermission:            "setup.list.unreadable",
	} {
		if got := listState(false, err); got != want {
			t.Errorf("listState(%T) = %s, want %s", err, got, want)
		}
	}
	if listState(true, nil) != "setup.list.set_up" || listState(false, nil) != "setup.list.not_set_up" {
		t.Error("listState without an error")
	}
}

// What the list calls "needs attention" is told in full by setup and by
// run: here a profile selected the old way in Codex's config.toml. Setup
// ends with exit code 1 and writes nothing, in either language.
func TestSetupCodexRefusesALegacyProfile(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	config := filepath.Join(env.Home, ".codex", "config.toml")
	const text = "model = \"gpt-6\"\nprofile = \"work\"\n\n[profiles.work]\nmodel = \"gpt-6\"\n"
	seedFile(t, config, text)
	shown := "~" + string(filepath.Separator) + filepath.Join(".codex", "config.toml")
	for lang, want := range map[string]string{
		"":            "ulgen: " + shown + " has profile = \"work\", the old way to select or define a profile, and Codex cannot use a profile file such as evren.config.toml while it is there; remove it (settings you want to keep go into a profile file of their own, NAME.config.toml, which codex --profile NAME uses), then run ulgen setup codex again\n",
		"tr_TR.UTF-8": "ulgen: " + shown + " dosyasında profile = \"work\" var; bu, profil seçmenin ya da tanımlamanın eski yoludur ve orada durdukça Codex evren.config.toml gibi bir profil dosyasını kullanamaz; kaldırın (korumak istediğiniz ayarlar kendi profil dosyasına, AD.config.toml dosyasına taşınır ve codex --profile AD ile kullanılır), sonra ulgen setup codex komutunu yeniden çalıştırın\n",
	} {
		vars["LANG"] = lang
		for _, args := range [][]string{{"setup", "codex", "--base-url", srv.URL}, {"setup", "codex", "--dry-run", "--default", "--base-url", srv.URL}, {"run", "codex", "hello"}} {
			code, stdout, stderr := exec(t, context.Background(), vars, args...)
			if code != 1 || stderr != want || strings.Contains(stdout, "created") || strings.Contains(stdout, "oluşturuldu") {
				t.Errorf("%v in %q: code %d stdout %q stderr %q", args, lang, code, stdout, stderr)
			}
		}
	}
	if got := readFile(t, config); got != text {
		t.Errorf("config.toml was changed:\n%s", got)
	}
	if got := entries(t, filepath.Join(env.Home, ".codex")); got != "config.toml" {
		t.Errorf("the Codex directory holds: %s", got)
	}
	if _, err := os.Stat(filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a backup directory was created: %v", err)
	}
}

// "ulgen setup claude" writes the EVREN workers skill into the Claude
// directory: created the first time, unchanged the second, and the notice
// that Anthropic does not support this use stays.
func TestSetupClaudeWritesTheSkill(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "claude", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	skill := filepath.Join(env.Home, ".claude", "skills", "evren-workers", "SKILL.md")
	for _, w := range []string{
		"Claude Code 1.0.3\n",
		"  ~" + string(filepath.Separator) + filepath.Join(".claude", "skills", "evren-workers", "SKILL.md") + "  created\n",
		"  Anthropic does not support running Claude Code against another provider",
		"  Start it with:  ulgen run claude\n",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	content := readFile(t, skill)
	if !strings.HasPrefix(content, "---\nname: evren-workers\n") ||
		!strings.Contains(content, `<!-- installed by ulgen setup; "ulgen setup claude" rewrites this file -->`) {
		t.Errorf("the skill is not the embedded text:\n%s", content)
	}

	// A second run changes nothing.
	code, stdout, stderr = exec(t, context.Background(), vars, "setup", "claude", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("second run: code %d stderr %q stdout %q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "  already set up; nothing to change\n") {
		t.Errorf("the second run did not say nothing changed:\n%s", stdout)
	}
	if strings.Contains(stdout, "created") || strings.Contains(stdout, "updated") {
		t.Errorf("the second run wrote a file:\n%s", stdout)
	}
}

// A user's own SKILL.md in the Claude directory is left alone, with a note;
// the list then shows Claude Code as not set up.
func TestSetupClaudeLeavesAUsersOwnSkill(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	skill := filepath.Join(env.Home, ".claude", "skills", "evren-workers", "SKILL.md")
	const own = "# my own skill\n"
	seedFile(t, skill, own)
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "claude", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "  ~"+string(filepath.Separator)+filepath.Join(".claude", "skills", "evren-workers", "SKILL.md")+" is your own skill; it was left as it is.") {
		t.Errorf("stdout lacks the foreign-skill note:\n%s", stdout)
	}
	if got := readFile(t, skill); got != own {
		t.Errorf("the user's skill was changed:\n%s", got)
	}
	_, stdout, _ = exec(t, context.Background(), nil, "setup")
	if !strings.Contains(outRow(stdout, "claude"), "1.0.3") || !strings.HasSuffix(outRow(stdout, "claude"), "not set up") {
		t.Errorf("the list does not show Claude Code as not set up:\n%s", stdout)
	}
}

// outRow returns the line of a tool's row in the output of "ulgen setup",
// with runs of spaces collapsed, or "" when there is none.
func outRow(stdout, tool string) string {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == tool {
			return strings.Join(fields, " ")
		}
	}
	return ""
}

// "ulgen setup all" includes Claude Code: installed, it is set up beside the
// other tools even though it writes no file.
func TestSetupAllIncludesClaude(t *testing.T) {
	useSetupEnv(t, map[string]string{"claude": "1.0.3 (Claude Code)\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "all", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Claude Code 1.0.3\n") || !strings.Contains(stdout, "  Start it with:  ulgen run claude\n") {
		t.Errorf("all did not include Claude Code:\n%s", stdout)
	}
}

func TestSetupWritesAToolsFile(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, rec := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	settings := readFile(t, filepath.Join(env.Home, ".qwen", "settings.json"))
	for _, w := range []string{`"id": "deepseek-v4.1-flash"`, `"id": "glm-5.3"`, `"baseUrl": "` + srv.URL + `"`, `"name": "deepseek-v4.1-flash"`, `"contextWindowSize": 524288`} {
		if !strings.Contains(settings, w) {
			t.Errorf("settings.json lacks %q:\n%s", w, settings)
		}
	}
	if strings.Contains(settings, "dots-ocr") || strings.Contains(settings, "language") {
		t.Errorf("settings.json has a model without tools or a language:\n%s", settings)
	}
	sep := string(filepath.Separator)
	for _, w := range []string{
		"Qwen Code (not installed)\n",
		"  ~" + sep + ".qwen" + sep + "settings.json  created\n",
		"  Usage statistics were switched off",
		"  Qwen Code needs Node.js 22 or newer; none was found.\n",
		"  Install it with:  npm install -g @qwen-code/qwen-code\n",
		"  To start it by its own name, put the key in your shell first:  export EVREN_API_KEY=\"$(ulgen token)\"\n",
		"  Start it with:  ulgen run qwen\n",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if got := strings.Join(rec.paths(), " "); got != "/models" {
		t.Errorf("requests = %q", got)
	}

	// A second run finds nothing to change.
	code, stdout, _ = exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "  already set up; nothing to change\n") || strings.Contains(stdout, "created") {
		t.Errorf("second run: code %d stdout:\n%s", code, stdout)
	}

	// A change to an existing file is backed up first. The old file held no
	// key, so nothing is said about one.
	code, stdout, _ = exec(t, context.Background(), vars, "setup", "--model", "glm-5.3", "qwen", "--base-url", srv.URL)
	backup := filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups", "20261012-004800", "qwen", "settings.json")
	if code != 0 || !strings.Contains(stdout, "settings.json  updated (+1 -1)\n") || !strings.Contains(stdout, "    backup: "+backup+"\n") || strings.Contains(stdout, "clear text") {
		t.Errorf("update: code %d stdout:\n%s", code, stdout)
	}
	if got := readFile(t, backup); got != settings {
		t.Errorf("the backup is not the previous file:\n%s", got)
	}
	if got := readFile(t, filepath.Join(env.Home, ".qwen", "settings.json")); !strings.Contains(got, `"name": "glm-5.3"`) {
		t.Errorf("settings.json after --model:\n%s", got)
	}
}

func TestSetupDryRun(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"opencode": "1.18.35\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "opencode", "--dry-run", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	sep := string(filepath.Separator)
	name := "~" + sep + filepath.Join(".config", "opencode", "opencode.json")
	for _, w := range []string{"opencode 1.18.35\n", "--- " + name + "\n+++ " + name + "\n", "+    \"evren\": {\n", "+        \"apiKey\": \"{env:EVREN_API_KEY}\"\n", "  nothing was written (--dry-run)\n"} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".config")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--dry-run wrote something: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--dry-run created a backup directory: %v", err)
	}
}

// What a dry run prints about things that did not happen: nothing. The
// tool cannot be started yet, and the statistics were not switched off.
func TestSetupDryRunDescribesAPlan(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	_, stdout, _ := exec(t, context.Background(), vars, "setup", "qwen", "codex", "--dry-run", "--base-url", srv.URL)
	for _, w := range []string{
		"  nothing was written (--dry-run)\n",
		"  Usage statistics would be switched off (privacy.usageStatisticsEnabled); Qwen Code sends them to a third party.\n",
		"  Qwen Code needs Node.js 22 or newer; none was found.\n",
		"  Install it with:  npm install -g @qwen-code/qwen-code\n",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	for _, w := range []string{"were switched off", "Start it with", "  To start it by its own name", "  EVREN is a profile in Codex"} {
		if strings.Contains(stdout, w) {
			t.Errorf("--dry-run says %q:\n%s", w, stdout)
		}
	}
	// What Ülgen itself writes is shown in full: none of its own lines looks
	// like a secret.
	if strings.Contains(stdout, "[hidden]") || strings.Contains(stdout, "[key]") {
		t.Errorf("a line of ulgen's own is hidden:\n%s", stdout)
	}
	for _, w := range []string{`+        "envKey": "EVREN_API_KEY",`, `+env_key = "EVREN_API_KEY"`, `export EVREN_API_KEY=\"$(ulgen token)\""`} {
		if !strings.Contains(stdout, w) {
			t.Errorf("the diff lacks %q:\n%s", w, stdout)
		}
	}

	// Once the tool is set up, a dry run changes nothing and says how to
	// start it, like a real run.
	if code, _, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL); code != 0 {
		t.Fatalf("setup: code %d stderr %q", code, stderr)
	}
	_, stdout, _ = exec(t, context.Background(), vars, "setup", "qwen", "--dry-run", "--base-url", srv.URL)
	if !strings.Contains(stdout, "  already set up; nothing to change\n") || !strings.Contains(stdout, "  Start it with:  ulgen run qwen\n") || strings.Contains(stdout, "--dry-run)") {
		t.Errorf("dry run of a finished setup:\n%s", stdout)
	}
}

// userOpencode is a configuration as a careful user made it by hand before
// Ülgen existed: the EVREN key in clear text, another provider's key on a
// one-line neighbour, and a comment with escape sequences in it. The entry
// names the variable of the key in its env list, which makes it one Ülgen
// may write into; without that it would be the user's own and be refused.
func userOpencode() string {
	return "{\n" +
		"  \"provider\": {\n" +
		"    \"evren\": {\n" +
		"      \"env\": [\"EVREN_API_KEY\"],\n" +
		"      // my provider \x1b]0;TITLE\x07\x1b[2Jand more\n" +
		"      \"options\": {\n" +
		"        \"baseURL\": \"https://old.example/v1\",\n" +
		"        \"apiKey\": \"" + testKey + "\"\n" +
		"      }\n" +
		"    },\n" +
		"    \"openrouter\": {\"options\": {\"apiKey\": \"sk-or-NEIGHBOUR\"}}\n" +
		"  },\n" +
		"  \"model\": \"evren/glm-5.3\"\n" +
		"}\n"
}

// When the new file still holds the key in clear text (under a name Ülgen
// does not touch, because it belongs to the user), the note says so and names
// the lines, and does not claim the key is only in the backup.
func TestSetupSaysWhenTheKeyStaysInTheFile(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"opencode": "1.18.35\n"})
	// This test leaves the key in the tool's own file on purpose, to see what
	// Ülgen says about it; only that file is exempt from the shared check.
	// Everything else Ülgen writes is still checked.
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	path := filepath.Join(env.Home, ".config", "opencode", "opencode.json")
	allowKey(t, path)
	// The key in a field Ülgen leaves alone: another provider's own key,
	// beside the EVREN entry it rewrites.
	seedFile(t, path, "{\n"+
		"  \"provider\": {\n"+
		"    \"evren\": {\"env\": [\"EVREN_API_KEY\"], \"options\": {}},\n"+
		"    \"mine\": {\"options\": {\"apiKey\": \""+testKey+"\"}}\n"+
		"  }\n"+
		"}\n")
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	want := "    your API key is still in the new file, in clear text, on line 4; Ülgen does not remove it\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
	if strings.Contains(stdout, "still holds it") {
		t.Errorf("stdout claims the key is only in the backup:\n%s", stdout)
	}
	// A second run finds nothing to change, so the note is not printed again.
	code, stdout, _ = exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL)
	if code != 0 || strings.Contains(stdout, "still in the new file") {
		t.Errorf("second run: code %d stdout:\n%s", code, stdout)
	}

	// Two lines: both numbers are listed.
	seedFile(t, path, "{\n"+
		"  \"mine\": \""+testKey+"\",\n"+
		"  \"other\": \""+testKey+"\",\n"+
		"  \"provider\": {\"evren\": {\"env\": [\"EVREN_API_KEY\"], \"options\": {}}}\n"+
		"}\n")
	_, stdout, _ = exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL)
	want = "    your API key is still in the new file, in clear text, on lines 2, 3; Ülgen does not remove it\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("two lines: stdout lacks %q:\n%s", want, stdout)
	}

	// Turkish.
	seedFile(t, path, "{\n"+
		"  \"mine\": \""+testKey+"\",\n"+
		"  \"provider\": {\"evren\": {\"env\": [\"EVREN_API_KEY\"], \"options\": {}}}\n"+
		"}\n")
	vars["LANG"] = "tr_TR.UTF-8"
	_, stdout, _ = exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL)
	if !strings.Contains(stdout, "API anahtarınız yeni dosyada hâlâ açık metin olarak, 2. satırda duruyor; Ülgen onu silmez\n") {
		t.Errorf("Turkish: stdout:\n%s", stdout)
	}
}

func TestSetupDryRunHidesSecretsAndEscapes(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"opencode": "1.18.35\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	path := filepath.Join(env.Home, ".config", "opencode", "opencode.json")
	seedFile(t, path, userOpencode())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "opencode", "--dry-run", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	// The key check on the output is in exec. The rest:
	for _, bad := range []string{"\x1b", "\x07", "TITLE", "[2J", "sk-or-NEIGHBOUR"} {
		if strings.Contains(stdout, bad) {
			t.Errorf("stdout contains %q:\n%q", bad, stdout)
		}
	}
	// It is the diff of an update: old lines go, new lines come, and the
	// lines around them are there as context.
	for _, w := range []string{
		"       // my provider and more\n", // the escape sequences are gone, the text stays
		"     \"openrouter\": {\"options\": {\"apiKey\": [hidden]}}\n",
		"-        \"baseURL\": \"https://old.example/v1\",\n",
		"-        \"apiKey\": [hidden]\n",
		"+        \"baseURL\": \"" + srv.URL + "\",\n",
		"+        \"apiKey\": \"{env:EVREN_API_KEY}\"\n",
		"  nothing was written (--dry-run)\n",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	if got := readFile(t, path); got != userOpencode() {
		t.Errorf("--dry-run changed the file:\n%s", got)
	}

	// The real run writes Ülgen's keys into the entry. The old file, key
	// included, is now the backup, and one line says so.
	code, stdout, stderr = exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL)
	backup := filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups", "20261012-004800", "opencode", "opencode.json")
	want := "    backup: " + backup + "\n    the old file held your API key in clear text; the backup at " + backup + " still holds it\n"
	if code != 0 || stderr != "" || !strings.Contains(stdout, want) {
		t.Errorf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	if got := readFile(t, backup); got != userOpencode() {
		t.Errorf("the backup is not the old file:\n%s", got)
	}
	if got := readFile(t, path); strings.Contains(got, testKey) || !strings.Contains(got, "sk-or-NEIGHBOUR") {
		t.Errorf("the new file:\n%s", got)
	}
}

// A file can hold bytes that no parser accepts; the diff must still not
// pass them on to the terminal.
func TestDiffOfCleansWhatItShows(t *testing.T) {
	sep := string(filepath.Separator)
	home := sep + "h"
	s := &setupRun{env: setup.Env{Home: home}, tr: i18n.New("en"), key: testKey}
	// Windows line endings, a byte that is not UTF-8, an escape sequence, a
	// NUL, the key, the key with an escape sequence in the middle of it (a
	// terminal would show the key), and the key as a secret's value.
	rest := "# \x9b31m \x1b]0;T\x07 \x00 " + testKey + "\r\n" +
		"# split " + testKey[:10] + "\x1b[0m" + testKey[10:] + "\r\n" +
		"api_key = \"" + testKey + "\"\r\n"
	change := setup.FileChange{
		Path: filepath.Join(home, "dir\x1b[2J", "config.toml"),
		Old:  []byte("a = 1\r\n" + rest),
		New:  []byte("a = 2\r\n" + rest),
	}
	got := s.diffOf(change)
	name := "~" + sep + filepath.Join("dir", "config.toml")
	want := "--- " + name + "\n+++ " + name + "\n@@ -1,4 +1,4 @@\n-a = 1\n+a = 2\n # \ufffd31m   [key]\n # split [key]\n api_key = [hidden]\n"
	if got != want {
		t.Errorf("diffOf:\n got  %q\n want %q", got, want)
	}
	// A change that lies only in what is hidden is still announced.
	change.Old = []byte("api_key = \"old-secret\"\n")
	change.New = []byte("api_key = \"new-secret\"\n")
	want = "--- " + name + "\n+++ " + name + "\n  the change is in a value that is not shown\n"
	if got := s.diffOf(change); got != want {
		t.Errorf("a hidden change:\n got  %q\n want %q", got, want)
	}
	// A new file.
	change.Old, change.New = nil, []byte("token = \"t\"\nmodel = \"m\"\n")
	want = "--- " + name + "\n+++ " + name + "\n@@ -0,0 +1,2 @@\n+token = [hidden]\n+model = \"m\"\n"
	if got := s.diffOf(change); got != want {
		t.Errorf("a new file:\n got  %q\n want %q", got, want)
	}
}

func TestSetupAllSkipsWhatIsNotInstalled(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "all", "--default", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	for _, w := range []string{
		"Qwen Code: not installed, skipped\n",
		"opencode: not installed, skipped\n",
		"Codex CLI 0.162.1\n",
		"evren.config.toml  created\n",
		"  To start it by its own name, put the key in your shell first:  export EVREN_API_KEY=\"$(ulgen token)\"\n",
		"  EVREN is a profile in Codex; by its own name it starts with:  codex --profile evren\n",
		"  --default does not apply to Codex",
	} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout lacks %q:\n%s", w, stdout)
		}
	}
	profile := readFile(t, filepath.Join(env.Home, ".codex", "evren.config.toml"))
	if !strings.Contains(profile, `model = "deepseek-v4.1-flash"`) || !strings.Contains(profile, "[model_providers.evren]\n") {
		t.Errorf("profile:\n%s", profile)
	}
	// Codex's own config.toml is never written, so it is not in the report.
	if _, err := os.Stat(filepath.Join(env.Home, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) || strings.Count(stdout, "config.toml") != 1 {
		t.Errorf("config.toml was written or reported (%v):\n%s", err, stdout)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qwen")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a skipped tool was written: %v", err)
	}
}

// "all" means the installed tools. With none installed there is nothing to
// set up, so the key is not sent anywhere and not even read.
func TestSetupAllWithNothingInstalledSendsNothing(t *testing.T) {
	env := useSetupEnv(t, nil)
	store := &countingStore{Store: useFileStore(t)}
	store.Set("evren", testKey)
	useStore(t, store)
	vars := map[string]string{"ULGEN_CONFIG_DIR": t.TempDir()}
	srv, rec := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "all", "--base-url", srv.URL)
	want := "ulgen: none of the supported tools is installed (qwen, opencode, codex, claude); install one, or name a tool to set it up before it is installed\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if len(rec.paths()) != 0 || store.gets != 0 {
		t.Errorf("requests %q, the key was read %d times", rec.paths(), store.gets)
	}
	if entries, _ := os.ReadDir(env.Home); len(entries) != 1 { // only the working directory of the test
		t.Errorf("the home directory has %d entries", len(entries))
	}
	_, _, stderr = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "setup", "all")
	if stderr != "ulgen: desteklenen araçların hiçbiri kurulu değil (qwen, opencode, codex, claude); birini kurun ya da kurmadan önce ayarlamak için aracın adını verin\n" {
		t.Errorf("Turkish: %q", stderr)
	}
}

func TestSetupOneToolFailsTheOtherIsStillSetUp(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	broken := filepath.Join(env.Home, ".qwen", "settings.json")
	seedFile(t, broken, "{\n  \"model\": \n}\n")
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "qwen", "codex", "qwen", "--base-url", srv.URL)
	if code != 1 {
		t.Errorf("code = %d", code)
	}
	// The path is shown as in the report lines.
	shown := "~" + string(filepath.Separator) + filepath.Join(".qwen", "settings.json")
	if want := "ulgen: " + shown + " is not valid JSON (line 3, column 1: invalid character '}' at start of value); fix it or move it away, then run ulgen setup again\n"; stderr != want {
		t.Errorf("stderr = %q", stderr)
	}
	if strings.Count(stdout, "Qwen Code (not installed)") != 1 || !strings.Contains(stdout, "evren.config.toml  created") {
		t.Errorf("stdout:\n%s", stdout)
	}
	if got := readFile(t, broken); got != "{\n  \"model\": \n}\n" {
		t.Errorf("the unreadable file was changed: %q", got)
	}
}

func TestSetupUsageErrors(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	needTool := "ulgen: options were given without a tool; name the tools to set up (qwen, opencode, codex, claude) or use \"all\"\n"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"setup", "cursor"}, "ulgen: cannot set up cursor; supported: qwen, opencode, codex, claude\n"},
		{[]string{"setup", "all", "qwen"}, "ulgen: \"all\" cannot be combined with tool names\n"},
		{[]string{"setup", "qwen", "--base-url", "http://example.com/v1"}, "ulgen: --base-url must use https"},
		{[]string{"setup", "qwen", "--model", "dots-ocr", "--base-url", srv.URL}, "ulgen: EVREN has no tool-capable chat model named dots-ocr; choose one of: deepseek-v4.1-flash, glm-5.3\n"},
		{[]string{"setup", "qwen", "--nope"}, "ulgen: unknown option: -nope\n"},
		{[]string{"setup", "qwen", "--model"}, "ulgen: option -model needs a value\n"},
		// An option without a tool does nothing, so it is a mistake, and
		// "--model qwen" takes the tool's name for the model.
		{[]string{"setup", "--dry-run"}, needTool},
		{[]string{"setup", "--default", "--model", "glm-5.3"}, needTool},
		{[]string{"setup", "--model", "qwen"}, needTool},
		{[]string{"setup", "--base-url", "https://mirror.example/v1"}, needTool},
		// A key pasted in the wrong place is not printed back.
		{[]string{"setup", "evren_llm_PASTED"}, "ulgen: cannot set up [key]; supported: qwen, opencode, codex, claude\n"},
		{[]string{"setup", "qwen", "--model", "evren_llm_PASTED", "--base-url", srv.URL}, "ulgen: EVREN has no tool-capable chat model named [key]; choose one of: deepseek-v4.1-flash, glm-5.3\n"},
		{[]string{"setup", "qwen", "--evren_llm_PASTED"}, "ulgen: unknown option: -[key]\n"},
	} {
		code, stdout, stderr := exec(t, context.Background(), vars, tc.args...)
		if code != 2 || !strings.HasPrefix(stderr, tc.want) || !strings.Contains(stderr, "ulgen setup <tool>... [options]") || stdout != "" {
			t.Errorf("%v: code %d stdout %q stderr %q", tc.args, code, stdout, stderr)
		}
		if strings.Contains(stderr, "PASTED") {
			t.Errorf("%v prints the pasted word back: %q", tc.args, stderr)
		}
	}
	code, stdout, stderr := exec(t, context.Background(), nil, "setup", "-h")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "-dry-run") || !strings.Contains(stdout, "(qwen, opencode, codex, claude)") {
		t.Errorf("help: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	_, _, stderr = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "setup", "--dry-run")
	if !strings.HasPrefix(stderr, "ulgen: seçenekler verildi ama araç adı yok; ayarlanacak araçların adını (qwen, opencode, codex, claude) ya da \"all\" yazın\n") {
		t.Errorf("Turkish: %q", stderr)
	}
	_, _, stderr = exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "setup", "cursor")
	if !strings.HasPrefix(stderr, "ulgen: cursor aracı ayarlanamıyor; desteklenenler: qwen, opencode, codex, claude\n") {
		t.Errorf("Turkish: %q", stderr)
	}
}

func TestParseInterleaved(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		words string
		model string
		dry   bool
		err   string
	}{
		{args: []string{"qwen", "--dry-run"}, words: "qwen", dry: true},
		{args: []string{"--dry-run", "qwen", "--model", "x", "codex"}, words: "qwen codex", model: "x", dry: true},
		{args: []string{"qwen", "codex", "-model=y"}, words: "qwen codex", model: "y"},
		{args: []string{"--dry-run=false", "qwen", "--dry-run"}, words: "qwen", dry: true},
		{args: nil, words: ""},
		// "--" ends the options once: what follows is a word, also when it
		// starts with a hyphen.
		{args: []string{"--", "qwen"}, words: "qwen"},
		{args: []string{"qwen", "--", "--dry-run"}, words: "qwen --dry-run"},
		// A single hyphen is a word.
		{args: []string{"qwen", "-"}, words: "qwen -"},
		// A missing value and an unknown option are errors.
		{args: []string{"qwen", "--model"}, err: "flag needs an argument: -model"},
		{args: []string{"qwen", "--nope"}, err: "flag provided but not defined: -nope"},
		{args: []string{"-h"}, err: "flag: help requested"},
	} {
		var opts setupOptions
		fs := setupFlags(&opts, i18n.New("en"))
		words, err := parseInterleaved(fs, tc.args)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("%q: err %v, want %q", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil || strings.Join(words, " ") != tc.words || opts.model != tc.model || opts.dryRun != tc.dry {
			t.Errorf("%q: words %q model %q dry %v err %v", tc.args, words, opts.model, opts.dryRun, err)
		}
	}
}

func TestSetupNeedsAKeyAndAWorkingEVREN(t *testing.T) {
	env := useSetupEnv(t, nil)
	useFileStore(t)
	code, _, stderr := exec(t, context.Background(), nil, "setup", "qwen")
	if code != 1 || stderr != "ulgen: not signed in; run ulgen login\n" {
		t.Errorf("no key: code %d stderr %q", code, stderr)
	}

	vars := signedIn(t)
	routes := healthy()
	routes["/models"] = answer{status: 401, body: fxUnauthorized}
	srv, _ := evrenServer(t, routes)
	code, _, stderr = exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 1 || stderr != "ulgen: the stored key is no longer valid; run ulgen login\n" {
		t.Errorf("401: code %d stderr %q", code, stderr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv, _ = evrenServer(t, healthy())
	code, _, stderr = exec(t, ctx, vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 130 || stderr != "ulgen: cancelled\n" {
		t.Errorf("cancelled: code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qwen")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed setup wrote something: %v", err)
	}
}

// modelJSON is one entry of EVREN's model list: a chat model with tool
// support. context and output are written as given ("null", "-5", ...);
// an empty context leaves the field out.
func modelJSON(id, context, output string) string {
	ctx := `"context_length":` + context + `,`
	if context == "" {
		ctx = ""
	}
	return `{"id":"` + id + `","object":"model","task":"chat",` + ctx + `"default_max_output_tokens":` + output + `,"capabilities":{"tools":true,"thinking":false,"vision":false}}`
}

func TestSetupLeavesOutModelsItCannotWrite(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	routes := healthy()
	routes["/models"] = answer{body: `{"object":"list","data":[` + strings.Join([]string{
		modelJSON("good-model_1.5:x/y", "1000", "null"),
		modelJSON(`bad\"\nname = 1`, "1000", "null"),        // unusual characters
		modelJSON(strings.Repeat("a", 101), "1000", "null"), // too long
		modelJSON("zero", "0", "null"),
		modelJSON("negative", "-5", "null"),
		modelJSON("missing", "", "null"),
		modelJSON("dup", "2000", "-1"),
		modelJSON("dup", "3000", "null"),
	}, ",") + `]}`}
	srv, _ := evrenServer(t, routes)
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "qwen", "opencode", "--base-url", srv.URL)
	if code != 0 || stderr != "ulgen: 6 models were left out (unusual name, no context size, or a repeated id)\n" {
		t.Fatalf("code %d stderr %q stdout %q", code, stderr, stdout)
	}
	settings := readFile(t, filepath.Join(env.Home, ".qwen", "settings.json"))
	config := readFile(t, filepath.Join(env.Home, ".config", "opencode", "opencode.json"))
	for name, text := range map[string]string{"settings.json": settings, "opencode.json": config} {
		for _, bad := range []string{"name = 1", strings.Repeat("a", 101), "zero", "negative", "missing", "-5", "-1", "3000", ": 0"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains %q:\n%s", name, bad, text)
			}
		}
	}
	if strings.Count(settings, `"id": "dup"`) != 1 || !strings.Contains(settings, `"id": "good-model_1.5:x/y"`) || !strings.Contains(settings, `"contextWindowSize": 2000`) {
		t.Errorf("settings.json:\n%s", settings)
	}
	if strings.Count(config, `"dup": {`) != 1 || !strings.Contains(config, `"output": 8192`) {
		t.Errorf("opencode.json:\n%s", config)
	}

	// One model left out: the sentence is in the singular. Nothing usable
	// left: no file is written.
	env = useSetupEnv(t, nil)
	routes["/models"] = answer{body: `{"object":"list","data":[` + modelJSON(`a b`, "1000", "null") + `]}`}
	srv, _ = evrenServer(t, routes)
	code, _, stderr = exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 1 || stderr != "ulgen: 1 model was left out (unusual name, no context size, or a repeated id)\nulgen: EVREN lists no chat model with tool support\n" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qwen")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed setup wrote something: %v", err)
	}
	vars["LANG"] = "tr_TR.UTF-8"
	_, _, stderr = exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if !strings.HasPrefix(stderr, "ulgen: 1 model listeye alınmadı (alışılmadık ad, eksik bağlam boyutu ya da yinelenen kimlik)\n") {
		t.Errorf("Turkish: %q", stderr)
	}
}

func TestSetupSaysWhenTheToolsPointElsewhere(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	// A cancelled context ends the run at the first request, so nothing
	// leaves the machine; the note must already be there.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, stderr := exec(t, ctx, vars, "setup", "qwen", "--base-url", "https://mirror.example/v1")
	want := "note: the tools are pointed at mirror.example, not at EVREN; your key will be sent there\nulgen: cancelled\n"
	if code != 130 || stderr != want {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestSetupNeverReusesABackupDirectory(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	if code, _, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL); code != 0 {
		t.Fatalf("first run: code %d stderr %q", code, stderr)
	}
	// An earlier run in the same second left its directory behind.
	taken := filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups", "20261012-004800")
	if err := os.MkdirAll(filepath.Join(taken, "qwen"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taken, "qwen", "settings.json"), []byte("the earlier run's backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(env.Home, ".qwen", "settings.json"))
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "--model", "glm-5.3", "qwen", "--base-url", srv.URL)
	backup := filepath.Join(taken+"-2", "qwen", "settings.json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "    backup: "+backup+"\n") {
		t.Fatalf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	if got := readFile(t, backup); got != before {
		t.Errorf("the backup is not the previous file:\n%s", got)
	}
	if got := readFile(t, filepath.Join(taken, "qwen", "settings.json")); got != "the earlier run's backup\n" {
		t.Errorf("the earlier backup was touched: %q", got)
	}
}

func TestSetupLeavesAReadOnlyFileAlone(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	if code, _, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL); code != 0 {
		t.Fatalf("first run: code %d stderr %q", code, stderr)
	}
	settings := filepath.Join(env.Home, ".qwen", "settings.json")
	before := readFile(t, settings)
	if err := os.Chmod(settings, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settings, 0o644) })
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "--model", "glm-5.3", "qwen", "--base-url", srv.URL)
	shown := "~" + string(filepath.Separator) + filepath.Join(".qwen", "settings.json")
	want := "ulgen: " + shown + " is read-only and was left as it is; make it writable, then run ulgen setup again\n"
	if code != 1 || stderr != want || strings.Contains(stdout, "updated") {
		t.Errorf("code %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
	if got := readFile(t, settings); got != before {
		t.Errorf("a read-only file was rewritten:\n%s", got)
	}
}

// A4: a dry run predicts the refusals of a real run. A read-only settings
// file exits 1 with the translated sentence, and nothing is written.
func TestSetupDryRunRefusesAReadOnlyFile(t *testing.T) {
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	settings := filepath.Join(env.Home, ".qwen", "settings.json")
	seedFile(t, settings, "{}\n")
	before := readFile(t, settings)
	if err := os.Chmod(settings, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settings, 0o644) })
	code, _, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--dry-run", "--base-url", srv.URL)
	shown := "~" + string(filepath.Separator) + filepath.Join(".qwen", "settings.json")
	want := "ulgen: " + shown + " is read-only and was left as it is; make it writable, then run ulgen setup again\n"
	if code != 1 || stderr != want {
		t.Errorf("code %d stderr %q", code, stderr)
	}
	if got := readFile(t, settings); got != before {
		t.Errorf("the dry run wrote something:\n%s", got)
	}
	if entries, err := os.ReadDir(filepath.Join(env.Home, ".qwen", "locales")); err == nil && len(entries) != 0 {
		t.Errorf("the dry run wrote files: %v", entries)
	}
}

// A file that cannot be opened is reported in the user's language, with the
// path as the report shows it; the system's own words are only the detail.
func TestSetupReportsAnUnreadableFileInTurkish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a file that cannot be read")
	}
	env := useSetupEnv(t, nil)
	vars := signedIn(t)
	vars["LANG"] = "tr_TR.UTF-8"
	srv, _ := evrenServer(t, healthy())
	settings := filepath.Join(env.Home, ".qwen", "settings.json")
	seedFile(t, settings, "{}\n")
	if err := os.Chmod(settings, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settings, 0o600) })
	if _, err := os.ReadFile(settings); err == nil {
		t.Skip("this user can read a file without permissions")
	}
	code, _, stderr := exec(t, context.Background(), vars, "setup", "qwen", "--base-url", srv.URL)
	if code != 1 || stderr != "ulgen: ~/.qwen/settings.json okunamadı: permission denied\n" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestDescribeSetupError(t *testing.T) {
	sep := string(filepath.Separator)
	home := sep + "h"
	in := func(name string) string { return filepath.Join(home, name) }
	shown := func(name string) string { return "~" + sep + name }
	// kept is where a backup of the file would be, with this system's separators.
	kept := func(name string) string { return filepath.Join("backups", "tool", name) }
	en := i18n.New("en")
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"changed, after another file": {
			&setup.WriteError{Path: in("a.json"), Err: &setup.ChangedError{Path: in("a.json")}, Done: []string{in("first.json"), in("second.json")}},
			shown("a.json") + " changed while setup was working; nothing was written to it, run ulgen setup again (already written: " + shown("first.json") + ", " + shown("second.json") + ")",
		},
		"read-only": {
			&setup.WriteError{Path: in("b.json"), Err: &setup.ReadOnlyError{Path: in("b.json")}},
			shown("b.json") + " is read-only and was left as it is; make it writable, then run ulgen setup again",
		},
		"a link that leads nowhere": {
			&setup.WriteError{Path: in("link.json"), Err: &setup.LinkError{Path: in("link.json")}},
			"the symbolic link at " + shown("link.json") + " points to a file that does not exist; repair or remove the link, then run ulgen setup again",
		},
		"a backup that is in the way": {
			&setup.WriteError{Path: in("c.json"), Err: &setup.BackupExistsError{Path: in("c.json"), Backup: in(kept("c.json"))}},
			"a backup of " + shown("c.json") + " from an earlier run is already at " + shown(kept("c.json")) + " and differs; nothing was written to " + shown("c.json") + ", and running ulgen setup again a moment later uses a new backup directory",
		},
		"a failure after a backup was made": {
			&setup.WriteError{Path: in("d.json"), Err: errors.New("disk full"), Backup: in(kept("d.json"))},
			"could not write " + shown("d.json") + ": disk full; the file as it was is saved at " + shown(kept("d.json")),
		},
		"another write failure": {
			&setup.WriteError{Path: in("c.json"), Err: errors.New("disk\x1b[2J full\nulgen: all is well")},
			"could not write " + shown("c.json") + ": disk full ulgen: all is well",
		},
		// What a parser says about a file comes from the file.
		"parse": {
			&setup.ParseError{Path: in("d.json"), Format: "JSON", Detail: "line 1: bad \x1b]0;title\x07 thing\nulgen: fine"},
			shown("d.json") + " is not valid JSON (line 1: bad  thing ulgen: fine); fix it or move it away, then run ulgen setup again",
		},
		"shape": {
			&setup.ShapeError{Path: in("e.json"), Detail: "mo\x1b[2Jdel is not an object"},
			shown("e.json") + " is not laid out as expected (model is not an object); fix it or move it away, then run ulgen setup again",
		},
		"an entry of the user's own, Codex": {
			&setup.ForeignEntryError{Path: in("config.toml"), Entry: "model_providers.evren"},
			shown("config.toml") + " defines model_providers.evren, an entry that is not ulgen's; ulgen does not overwrite it and cannot set EVREN up under the same name next to it; remove or rename it, then run ulgen setup again",
		},
		"an entry of the user's own, opencode": {
			&setup.ForeignEntryError{Path: in("opencode.jsonc"), Entry: "provider.evren\x1b[2J"},
			shown("opencode.jsonc") + " defines provider.evren, an entry that is not ulgen's; ulgen does not overwrite it and cannot set EVREN up under the same name next to it; remove or rename it, then run ulgen setup again",
		},
		"a default ulgen's entry would take over": {
			&setup.DefaultClashError{Path: in("settings.json"), Model: "deepseek-v4-flash"},
			shown("settings.json") + " names \"deepseek-v4-flash\" as Qwen Code's default model, and no entry under modelProviders has that id. EVREN offers a model of that name, so once EVREN is set up Qwen Code would start on EVREN's entry instead of yours. To make EVREN the default, run ulgen setup qwen --default; to keep your own, add an entry for your model under modelProviders.openai first",
		},
		// The default can also stand in a variable; its name is shown as it is.
		"a default in the environment ulgen's entry would take over": {
			&setup.DefaultClashError{Var: "OPENAI_MODEL", Model: "deepseek-v4-flash"},
			"OPENAI_MODEL names \"deepseek-v4-flash\" as Qwen Code's default model, and no entry under modelProviders has that id. EVREN offers a model of that name, so once EVREN is set up Qwen Code would start on EVREN's entry instead of yours. To make EVREN the default, run ulgen setup qwen --default; to keep your own, add an entry for your model under modelProviders.openai first",
		},
		"a directory where a file belongs": {
			&setup.KindError{Path: in("settings.json")},
			shown("settings.json") + " is a directory where a file is expected; move it away, then run ulgen setup again",
		},
		"a file where a directory belongs": {
			&setup.KindError{Path: in("locales"), WantDir: true},
			shown("locales") + " is not a directory, and a directory is expected there; move it away, then run ulgen setup again",
		},
		"a variable that names a file": {
			&setup.KindError{Path: in("a-file"), WantDir: true, Var: "CODEX_HOME"},
			"CODEX_HOME points to " + shown("a-file") + ", which is not a directory",
		},
		"a request no adapter can use": {
			&setup.RequestError{Problem: "the model \"m\x1b[2J\" is listed twice"},
			"the setup request is not usable: the model \"m\" is listed twice",
		},
		"legacy profile": {
			&setup.LegacyProfileError{Path: in("config.toml"), Key: `profile = "work"`},
			shown("config.toml") + " has profile = \"work\", the old way to select or define a profile, and Codex cannot use a profile file such as evren.config.toml while it is there; remove it (settings you want to keep go into a profile file of their own, NAME.config.toml, which codex --profile NAME uses), then run ulgen setup codex again",
		},
		"missing directory": {
			&setup.MissingDirError{Var: "CODEX_HOME", Path: in("nope")},
			"CODEX_HOME points to " + shown("nope") + ", which does not exist",
		},
		"a file that cannot be read": {
			&os.PathError{Op: "open", Path: in("f.json"), Err: os.ErrPermission},
			shown("f.json") + " could not be read: permission denied",
		},
		"no home":         {&setup.HomeError{}, "the home directory is not known; set HOME (USERPROFILE on Windows) to its absolute path"},
		"relative home":   {&setup.HomeError{Home: "rel"}, "the home directory rel is not an absolute path; set HOME (USERPROFILE on Windows) to its absolute path"},
		"relative folder": {&setup.RelativePathError{Var: "CODEX_HOME", Value: "evren_llm_OOPS\x1b[2J"}, "CODEX_HOME is set to [key], which is not an absolute path; give the full path or unset the variable"},
		"anything else":   {errors.New("odd\nthing"), "the setup failed: odd thing"},
	} {
		if got := describeSetupError(tc.err, home, en); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", name, got, tc.want)
		}
	}
	tr := i18n.New("tr")
	changed := &setup.WriteError{Path: in("a.json"), Err: &setup.ChangedError{Path: in("a.json")}}
	if got, want := describeSetupError(changed, home, tr), shown("a.json")+", ulgen setup çalışırken değişti; bu dosyaya hiçbir şey yazılmadı, ulgen setup komutunu yeniden çalıştırın"; got != want {
		t.Errorf("Turkish:\n got  %q\n want %q", got, want)
	}
	// The two refusals of A2 and the backup of A3, in Turkish.
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"a link that leads nowhere": {
			&setup.WriteError{Path: in("link.json"), Err: &setup.LinkError{Path: in("link.json")}},
			shown("link.json") + " yolundaki sembolik bağ, var olmayan bir dosyayı gösteriyor; bağı onarın ya da kaldırın, sonra ulgen setup komutunu yeniden çalıştırın",
		},
		"a backup that is in the way": {
			&setup.WriteError{Path: in("c.json"), Err: &setup.BackupExistsError{Path: in("c.json"), Backup: in(kept("c.json"))}},
			shown("c.json") + " dosyası için daha önceki bir çalışmadan kalan bir yedek " + shown(kept("c.json")) + " konumunda duruyor ve içeriği farklı; " + shown("c.json") + " dosyasına hiçbir şey yazılmadı, ulgen setup komutunu biraz sonra yeniden çalıştırırsanız yeni bir yedek dizini kullanılır",
		},
		"a failure after a backup was made": {
			&setup.WriteError{Path: in("d.json"), Err: errors.New("disk full"), Backup: in(kept("d.json"))},
			shown("d.json") + " yazılamadı: disk full; dosyanın önceki hâli " + shown(kept("d.json")) + " konumunda saklandı",
		},
	} {
		if got := describeSetupError(tc.err, home, tr); got != tc.want {
			t.Errorf("%s (tr):\n got  %q\n want %q", name, got, tc.want)
		}
	}
	if got := describeSetupError(errors.New("odd"), home, tr); got != "ayarlama başarısız oldu: odd" {
		t.Errorf("Turkish fall-through: %q", got)
	}
}

// countingStore counts how often the key is read.
type countingStore struct {
	credential.Store
	gets int
}

func (s *countingStore) Get(account string) (string, error) {
	s.gets++
	return s.Store.Get(account)
}

// useStore makes the commands use store. Call it after useFileStore, which
// provides the configuration directory.
func useStore(t *testing.T, store credential.Store) {
	t.Helper()
	saved := openStore
	openStore = func(string, func(string) string) (credential.Store, error) { return store, nil }
	t.Cleanup(func() { openStore = saved })
}

// probes makes the version probe of the test machine run fn first and
// counts the probes.
func probes(t *testing.T, env setup.Env, fn func(binary string)) *int {
	t.Helper()
	n := 0
	version := env.Version
	env.Version = func(ctx context.Context, binary string) (string, error) {
		n++
		if fn != nil {
			fn(binary)
		}
		return version(ctx, binary)
	}
	useEnv(t, env)
	return &n
}

// One Ctrl+C ends the command: each test cancels the context at another
// point and expects exit code 130, "cancelled", and nothing written.
func TestSetupListStopsWhenCancelled(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "codex": "codex-cli 0.162.1\n"})
	// Cancelled before it starts: no tool is asked for its version.
	asked := probes(t, env, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, stdout, stderr := exec(t, ctx, nil, "setup")
	if code != 130 || stdout != "" || stderr != "ulgen: cancelled\n" || *asked != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d probes", code, stdout, stderr, *asked)
	}
	// Cancelled while the first tool answers: no table of guesses, and the
	// other tools are not asked.
	ctx, cancel = context.WithCancel(context.Background())
	asked = probes(t, env, func(string) { cancel() })
	code, stdout, stderr = exec(t, ctx, map[string]string{"LANG": "tr_TR.UTF-8"}, "setup")
	if code != 130 || stdout != "" || stderr != "ulgen: iptal edildi\n" || *asked != 1 {
		t.Errorf("mid-way: code %d stdout %q stderr %q, %d probes", code, stdout, stderr, *asked)
	}
}

func TestSetupStopsBeforeTheNextToolWhenCancelled(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	// The model list has arrived; the interrupt lands while the backup
	// directory is chosen, which is the last step before the first tool.
	ctx, cancel := context.WithCancel(context.Background())
	asked := probes(t, env, nil)
	fixed := now
	now = func() time.Time {
		cancel()
		return fixed()
	}
	t.Cleanup(func() { now = fixed })
	code, stdout, stderr := exec(t, ctx, vars, "setup", "qwen", "codex", "--base-url", srv.URL)
	if code != 130 || stdout != "" || stderr != "ulgen: cancelled\n" || *asked != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d probes", code, stdout, stderr, *asked)
	}
	assertNothingSetUp(t, env, vars)
}

func TestSetupDoesNotWriteAfterCancel(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	srv, rec := evrenServer(t, healthy())
	// The interrupt lands while the first tool is asked for its version,
	// after the model list arrived: its header is not printed and its plan
	// is made, and not written.
	ctx, cancel := context.WithCancel(context.Background())
	asked := probes(t, env, func(string) { cancel() })
	code, stdout, stderr := exec(t, ctx, vars, "setup", "qwen", "codex", "--base-url", srv.URL)
	if code != 130 || stdout != "" || stderr != "ulgen: cancelled\n" || *asked != 1 {
		t.Errorf("code %d stdout %q stderr %q, %d probes", code, stdout, stderr, *asked)
	}
	if got := strings.Join(rec.paths(), " "); got != "/models" {
		t.Errorf("requests = %q", got)
	}
	assertNothingSetUp(t, env, vars)

	// The same during the second tool: the first one is set up, the second
	// is not touched, and the exit code still says "interrupted".
	ctx, cancel = context.WithCancel(context.Background())
	probes(t, env, func(binary string) {
		if filepath.Base(binary) == "codex" {
			cancel()
		}
	})
	code, _, stderr = exec(t, ctx, vars, "setup", "qwen", "codex", "--base-url", srv.URL)
	if code != 130 || stderr != "ulgen: cancelled\n" {
		t.Errorf("second tool: code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qwen", "settings.json")); err != nil {
		t.Errorf("the first tool was not set up: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".codex")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the second tool was written after the interrupt: %v", err)
	}
}

func TestSetupAllStopsWhileLookingForTools(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "codex": "codex-cli 0.162.1\n"})
	vars := signedIn(t)
	srv, rec := evrenServer(t, healthy())
	ctx, cancel := context.WithCancel(context.Background())
	asked := probes(t, env, func(string) { cancel() })
	code, stdout, stderr := exec(t, ctx, vars, "setup", "all", "--base-url", srv.URL)
	if code != 130 || stdout != "" || stderr != "ulgen: cancelled\n" || *asked != 1 || len(rec.paths()) != 0 {
		t.Errorf("code %d stdout %q stderr %q, %d probes, requests %q", code, stdout, stderr, *asked, rec.paths())
	}
	assertNothingSetUp(t, env, vars)
}

// assertNothingSetUp fails when a tool's directory or a backup exists.
func assertNothingSetUp(t *testing.T, env setup.Env, vars map[string]string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(env.Home, ".qwen"), filepath.Join(env.Home, ".config"), filepath.Join(env.Home, ".codex"),
		filepath.Join(vars["ULGEN_CONFIG_DIR"], "backups"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after a cancelled setup (%v)", path, err)
		}
	}
}

// plantTools fills dir with what a cloned repository could carry to be
// picked up by a program that uses paths relative to the current directory:
// an executable where opencode's installer would put it, and tool files.
func plantTools(t *testing.T, dir string) {
	t.Helper()
	for path, mode := range map[string]os.FileMode{
		filepath.Join(".opencode", "bin", "opencode"):     0o700,
		filepath.Join(".opencode", "bin", "opencode.cmd"): 0o700,
		filepath.Join(".qwen", "settings.json"):           0o600,
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("#!/bin/sh\necho planted\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
}

// entries lists the files and directories under dir.
func entries(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if rel, _ := filepath.Rel(dir, path); rel != "." {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return strings.Join(out, " ")
}

// Without a home directory every path the adapters build would be relative
// to the current directory. The real environment of the machine is used
// here (setupEnv is not replaced), with the home variables emptied; the
// store, the working directory and the tool start are still the guards from
// TestMain, so reaching any of them fails the test.
func TestNothingIsReadOrStartedWithoutAHomeDirectory(t *testing.T) {
	saved := setupEnv
	setupEnv = realSetupEnv
	t.Cleanup(func() { setupEnv = saved })
	dir := t.TempDir()
	plantTools(t, dir)
	t.Chdir(dir)
	before := entries(t, dir)
	for home, want := range map[string]string{
		"":        "ulgen: the home directory is not known; set HOME (USERPROFILE on Windows) to its absolute path\n",
		"relhome": "ulgen: the home directory relhome is not an absolute path; set HOME (USERPROFILE on Windows) to its absolute path\n",
	} {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if env, err := checkedEnv(func(string) string { return "" }); err == nil {
			t.Fatalf("home %q: the environment was accepted: %+v", home, env.Home)
		}
		for _, args := range [][]string{{"setup"}, {"setup", "qwen", "opencode"}, {"setup", "all"}, {"setup", "opencode", "--dry-run"}, {"run", "opencode", "hello"}, {"run", "--trust-project", "qwen"}} {
			code, stdout, stderr := exec(t, context.Background(), nil, args...)
			if code != 1 || stdout != "" || stderr != want {
				t.Errorf("home %q, %v: code %d stdout %q stderr %q", home, args, code, stdout, stderr)
			}
		}
	}
	if after := entries(t, dir); after != before {
		t.Errorf("the current directory changed:\n before %s\n after  %s", before, after)
	}
	_, _, stderr := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, "setup")
	if !strings.HasPrefix(stderr, "ulgen: ev dizini relhome tam yol değil; ") && !strings.HasPrefix(stderr, "ulgen: ev dizini bilinmiyor; ") {
		t.Errorf("Turkish: %q", stderr)
	}
}

// A variable that moves a directory counts only when it is absolute.
func TestRelativeDirectoryVariablesAreRefused(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	dir := t.TempDir()
	plantTools(t, dir)
	t.Chdir(dir)
	before := entries(t, dir)
	srv, rec := evrenServer(t, healthy())
	started := useFakeExec(t, 0, nil)
	for _, name := range []string{"ULGEN_CONFIG_DIR", "CODEX_HOME", "QWEN_HOME", "OPENCODE_CONFIG_DIR"} {
		vars := signedIn(t)
		vars[name] = filepath.Join("rel", "cfg")
		want := "ulgen: " + name + " is set to " + filepath.Join("rel", "cfg") + ", which is not an absolute path; give the full path or unset the variable\n"
		for _, args := range [][]string{{"setup", "qwen", "codex", "--base-url", srv.URL}, {"run", "qwen"}} {
			code, stdout, stderr := exec(t, context.Background(), vars, args...)
			if code != 1 || stdout != "" || stderr != want {
				t.Errorf("%s, %v: code %d stdout %q stderr %q", name, args, code, stdout, stderr)
			}
		}
	}
	// The directory of Ülgen's own files is refused by every command that
	// would read the key from it.
	vars := map[string]string{"ULGEN_CONFIG_DIR": "relcfg"}
	useTerminalStdout(t, false)
	for _, args := range [][]string{{"token"}, {"status"}, {"logout"}} {
		code, stdout, stderr := exec(t, context.Background(), vars, args...)
		if code != 1 || stdout != "" || stderr != "ulgen: ULGEN_CONFIG_DIR is set to relcfg, which is not an absolute path; give the full path or unset the variable\n" {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
	vars["LANG"] = "tr_TR.UTF-8"
	if _, _, stderr := exec(t, context.Background(), vars, "token"); stderr != "ulgen: ULGEN_CONFIG_DIR ortam değişkeni relcfg olarak ayarlı, bu tam yol değil; tam yolu verin ya da değişkeni kaldırın\n" {
		t.Errorf("Turkish: %q", stderr)
	}
	if len(rec.paths()) != 0 || started.binary != "" {
		t.Errorf("requests %q, started %q", rec.paths(), started.binary)
	}
	if after := entries(t, dir); after != before {
		t.Errorf("the current directory changed:\n before %s\n after  %s", before, after)
	}
	if got := entries(t, env.Home); got != "work" {
		t.Errorf("the home directory was written to: %s", got)
	}

	// A relative XDG_CONFIG_HOME is not an error: by the XDG rule it counts
	// as not set, so the file under the home directory is used.
	vars = signedIn(t)
	vars["XDG_CONFIG_HOME"] = "xdgrel"
	if code, _, stderr := exec(t, context.Background(), vars, "setup", "opencode", "--base-url", srv.URL); code != 0 {
		t.Fatalf("XDG_CONFIG_HOME: code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".config", "opencode", "opencode.json")); err != nil {
		t.Errorf("the file under the home directory was not written: %v", err)
	}
	if after := entries(t, dir); after != before {
		t.Errorf("XDG_CONFIG_HOME: the current directory changed:\n before %s\n after  %s", before, after)
	}
}

func TestConfigDir(t *testing.T) {
	abs := t.TempDir()
	saved := defaultConfigDir
	t.Cleanup(func() { defaultConfigDir = saved })
	fromOS := ""
	defaultConfigDir = func(getenv func(string) string) (string, error) {
		if v := getenv("ULGEN_CONFIG_DIR"); v != "" {
			return v, nil
		}
		return fromOS, nil
	}
	env := func(v string) func(string) string {
		return func(k string) string { return map[string]string{"ULGEN_CONFIG_DIR": v}[k] }
	}
	if dir, err := configDir(env(abs)); err != nil || dir != abs {
		t.Errorf("absolute variable: %q, %v", dir, err)
	}
	var rel *setup.RelativePathError
	if dir, err := configDir(env("relcfg")); !errors.As(err, &rel) || rel.Var != "ULGEN_CONFIG_DIR" || rel.Value != "relcfg" || dir != "" {
		t.Errorf("relative variable: %q, %v", dir, err)
	}
	fromOS = abs
	if dir, err := configDir(env("")); err != nil || dir != abs {
		t.Errorf("from the system: %q, %v", dir, err)
	}
	// HOME (or AppData) holds a relative path: the directory the system
	// derives from it is refused too.
	fromOS = filepath.Join("relhome", ".config", "ulgen")
	var cfg *configDirError
	if dir, err := configDir(env("")); !errors.As(err, &cfg) || dir != "" {
		t.Errorf("relative from the system: %q, %v", dir, err)
	}
	if msg, ok := describeEnvError(cfg, i18n.New("en")); !ok || msg != "the configuration directory "+fromOS+" is not an absolute path; set ULGEN_CONFIG_DIR to an absolute path" {
		t.Errorf("message: %q", msg)
	}
	fromOS = ""
	defaultConfigDir = func(func(string) string) (string, error) { return "", errors.New("no home") }
	if _, err := configDir(env("")); err == nil || err.Error() != "no home" {
		t.Errorf("an error of the system: %v", err)
	}

	// The operating system refuses a relative XDG_CONFIG_HOME (the XDG rule
	// is to treat it as not set), so on the system that reads the variable
	// there is no configuration directory. Ülgen falls back to the one under
	// the home directory rather than leaving the user without a place for the
	// key; the relative home directory in the message would not help.
	vars := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	home := t.TempDir()
	defaultConfigDir = func(getenv func(string) string) (string, error) {
		if x := getenv("XDG_CONFIG_HOME"); x != "" && !filepath.IsAbs(x) {
			return "", errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")
		}
		return abs, nil
	}
	if dir, err := configDir(vars(map[string]string{"XDG_CONFIG_HOME": "xdgrel", "HOME": home})); err != nil || dir != filepath.Join(home, ".config", "ulgen") {
		t.Errorf("a relative XDG_CONFIG_HOME: %q, %v", dir, err)
	}
	// Any other failure of the system stays the system's to report: a
	// relative or unknown home directory has no directory under it to fall
	// back to, and an absolute XDG_CONFIG_HOME is not the case at all.
	for _, m := range []map[string]string{
		{"XDG_CONFIG_HOME": "xdgrel"},
		{"XDG_CONFIG_HOME": "xdgrel", "HOME": "relhome"},
		{"XDG_CONFIG_HOME": "xdgrel", "HOME": ""},
	} {
		if _, err := configDir(vars(m)); err == nil {
			t.Errorf("%v: the error of the system was swallowed", m)
		}
	}
	if dir, err := configDir(vars(map[string]string{"XDG_CONFIG_HOME": home})); err != nil || dir != abs {
		t.Errorf("an absolute XDG_CONFIG_HOME: %q, %v", dir, err)
	}
}

// What a tool prints as its version goes on the screen, in the list and in
// the header of a setup. Only a version number of a sane length gets there.
func TestVersionTextIsCleaned(t *testing.T) {
	useSetupEnv(t, map[string]string{
		"codex": "codex 1.2.3\x1b[2J\x1b]0;owned\x07\n",
		"qwen":  "9.9" + strings.Repeat("9", 300) + "\n",
	})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	long := "9.9" + strings.Repeat("9", 29) // 32 characters, the most a version gets
	row := func(tool, installed, state string) string {
		return fmt.Sprintf("%-8s   %-32s   %s\n", tool, installed, state)
	}
	code, stdout, _ := exec(t, context.Background(), nil, "setup")
	want := row("Tool", "Installed", "EVREN") + row("qwen", long, "not set up") + row("opencode", "not found", "not set up") +
		row("codex", "1.2.3", "not set up") + row("claude", "not found", "not set up") + "\nSet a tool up with:  ulgen setup <tool>\n"
	if code != 0 || stdout != want {
		t.Errorf("list: code %d stdout:\n%q", code, stdout)
	}
	_, stdout, _ = exec(t, context.Background(), vars, "setup", "codex", "qwen", "--dry-run", "--base-url", srv.URL)
	if !strings.HasPrefix(stdout, "Codex CLI 1.2.3\n") || !strings.Contains(stdout, "\nQwen Code "+long+"\n") || strings.ContainsAny(stdout, "\x1b\x07") {
		t.Errorf("headers:\n%q", stdout)
	}
}

func TestProbeVersionDoesNotWaitForWhatTheToolLeavesBehind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in tools are shell scripts")
	}
	savedTimeout, savedGrace := versionProbeTimeout, versionProbeGrace
	t.Cleanup(func() { versionProbeTimeout, versionProbeGrace = savedTimeout, savedGrace })
	script := func(name, body string) string {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// The stand-ins start "sleep 4": long enough to tell a wait from no
	// wait, short enough to be gone soon after the test.
	const limit = 3 * time.Second

	// A wrapper answers and ends, but a child it left in the background
	// holds the output open. The answer counts and the wait is short.
	versionProbeTimeout, versionProbeGrace = 2*time.Second, 200*time.Millisecond
	start := time.Now()
	out, err := probeVersion(context.Background(), script("wrapper", "echo 'tool 1.2.3'\nsleep 4 &\n"))
	if elapsed := time.Since(start); err != nil || !strings.Contains(out, "1.2.3") || elapsed > limit {
		t.Errorf("wrapper: %q, %v, after %v", out, err, elapsed)
	}

	// A tool that does not answer is stopped at the limit. Its child, which
	// holds the output too, does not make the wait longer.
	versionProbeTimeout = 300 * time.Millisecond
	start = time.Now()
	out, err = probeVersion(context.Background(), script("hangs", "sleep 4\necho late\n"))
	if elapsed := time.Since(start); err == nil || strings.Contains(out, "late") || elapsed > limit {
		t.Errorf("hanging tool: %q, %v, after %v", out, err, elapsed)
	}

	// A cancelled context ends the probe at once.
	versionProbeTimeout = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	if _, err = probeVersion(ctx, script("never", "sleep 4\n")); err == nil || time.Since(start) > limit {
		t.Errorf("cancelled: %v after %v", err, time.Since(start))
	}

	// An ordinary tool.
	if out, err = probeVersion(context.Background(), script("plain", "echo 'plain 0.9.1'\n")); err != nil || out != "plain 0.9.1\n" {
		t.Errorf("plain: %q, %v", out, err)
	}
}

// The program search never returns a path relative to the current
// directory, whatever PATH says and however the name is written.
func TestLookPathAbsRefusesTheCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	name := "ulgen-test-tool"
	file := name
	if runtime.GOOS == "windows" {
		file += ".cmd"
		t.Setenv("PATHEXT", ".CMD")
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, path := range []string{".", "", "." + string(os.PathListSeparator) + filepath.Join(dir, "none")} {
		t.Setenv("PATH", path)
		for _, n := range []string{name, "." + string(filepath.Separator) + name, filepath.Join("..", filepath.Base(dir), name)} {
			if got, err := lookPathAbs(n); err == nil {
				t.Errorf("PATH %q: lookPathAbs(%q) = %q", path, n, got)
			}
		}
	}
	// The same file is found by its full path, and through an absolute PATH
	// entry.
	if got, err := lookPathAbs(filepath.Join(dir, name)); err != nil || !filepath.IsAbs(got) {
		t.Errorf("by full path: %q, %v", got, err)
	}
	t.Setenv("PATH", dir)
	if got, err := lookPathAbs(name); err != nil || !filepath.IsAbs(got) || filepath.Dir(got) != dir {
		t.Errorf("through PATH: %q, %v", got, err)
	}
}

// The line about the shell is written for the system's shell, for every
// tool: each adapter hands the command line over with its note.
func TestTheShellHintFollowsTheSystem(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "  To start it by its own name, put the key in your shell first:  $env:EVREN_API_KEY = (ulgen token)\n",
		"linux":   "  To start it by its own name, put the key in your shell first:  export EVREN_API_KEY=\"$(ulgen token)\"\n",
		"darwin":  "  To start it by its own name, put the key in your shell first:  export EVREN_API_KEY=\"$(ulgen token)\"\n",
	} {
		env := useSetupEnv(t, nil)
		env.GOOS = goos
		useEnv(t, env)
		vars := signedIn(t)
		srv, _ := evrenServer(t, healthy())
		code, stdout, stderr := exec(t, context.Background(), vars, "setup", "qwen", "opencode", "codex", "--base-url", srv.URL)
		if code != 0 || strings.Count(stdout, want) != 3 || strings.Contains(stdout, "%!") {
			t.Errorf("%s: code %d stderr %q stdout:\n%s", goos, code, stderr, stdout)
		}
	}
}

func TestShowPath(t *testing.T) {
	sep := string(filepath.Separator)
	root := func(parts ...string) string { return sep + filepath.Join(parts...) }
	home := root("home", "u")
	for _, tc := range []struct{ home, path, want string }{
		{home, filepath.Join(home, ".qwen", "settings.json"), "~" + sep + filepath.Join(".qwen", "settings.json")},
		{home + sep, filepath.Join(home, ".qwen", "settings.json"), "~" + sep + filepath.Join(".qwen", "settings.json")},
		// A name that only starts with two dots lies under the home
		// directory; "..", a sibling and the parent do not.
		{home, filepath.Join(home, "..cache", "x"), "~" + sep + filepath.Join("..cache", "x")},
		{home, filepath.Join(home, "..."), "~" + sep + "..."},
		{home, root("home", "u2", "x"), root("home", "u2", "x")},
		{home, root("home"), root("home")},
		{home, root("etc", "x"), root("etc", "x")},
		{home, home, home},
		{"", root("etc", "x"), root("etc", "x")},
		{sep, root("etc", "x"), "~" + sep + filepath.Join("etc", "x")},
		// A space cannot be pasted and "~" does not expand inside quotes: the
		// whole path, quoted.
		{home, filepath.Join(home, "Library", "Application Support", "ulgen", "x.json"), `"` + filepath.Join(home, "Library", "Application Support", "ulgen", "x.json") + `"`},
		{home, root("srv", "my files", "x"), `"` + root("srv", "my files", "x") + `"`},
		// The space is in the home directory itself: "~" hides it.
		{root("home", "Ayşe Yılmaz"), root("home", "Ayşe Yılmaz", ".qwen", "settings.json"), "~" + sep + filepath.Join(".qwen", "settings.json")},
		// A path is text from the machine: no escape sequence, one line.
		{home, filepath.Join(home, "a\x1b[2Jb", "c"), "~" + sep + filepath.Join("ab", "c")},
		{home, filepath.Join(home, "a\x1b]0;t\x07b", "c\nd"), `"` + filepath.Join(home, "ab", "c d") + `"`},
	} {
		if got := showPath(tc.home, tc.path); got != tc.want {
			t.Errorf("showPath(%q, %q) = %q, want %q", tc.home, tc.path, got, tc.want)
		}
	}
}
