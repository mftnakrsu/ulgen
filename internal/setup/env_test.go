package setup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestEnvValidate(t *testing.T) {
	home := t.TempDir()
	abs := t.TempDir()
	env := func(home string, vars map[string]string) Env {
		return Env{Home: home, Getenv: func(k string) string { return vars[k] }}
	}
	for name, e := range map[string]Env{
		"plain":                   env(home, nil),
		"no Getenv":               {Home: home},
		"absolute variables":      env(home, map[string]string{"QWEN_HOME": abs, "OPENCODE_CONFIG_DIR": abs, "CODEX_HOME": abs}),
		"QWEN_HOME from home":     env(home, map[string]string{"QWEN_HOME": "~/qw"}),
		"QWEN_HOME is home":       env(home, map[string]string{"QWEN_HOME": "~"}),
		"relative XDG is unset":   env(home, map[string]string{"XDG_CONFIG_HOME": "xdgrel"}),
		"blank variable is unset": env(home, map[string]string{"CODEX_HOME": "  "}),
	} {
		if err := e.Validate(); err != nil {
			t.Errorf("%s: Validate = %v", name, err)
		}
	}

	for name, h := range map[string]string{"unknown": "", "relative": "home", "dot": "."} {
		var he *HomeError
		if err := env(h, nil).Validate(); !errors.As(err, &he) || he.Home != h {
			t.Errorf("%s home: Validate = %v", name, err)
		}
	}
	for _, name := range []string{"QWEN_HOME", "OPENCODE_CONFIG_DIR", "CODEX_HOME"} {
		var re *RelativePathError
		err := env(home, map[string]string{name: filepath.Join("rel", "dir")}).Validate()
		if !errors.As(err, &re) || re.Var != name || re.Value != filepath.Join("rel", "dir") {
			t.Errorf("relative %s: Validate = %v", name, err)
		}
	}
	// "~" starts at the home directory only in QWEN_HOME; the other tools
	// would read it as a directory named "~" in the current directory.
	var re *RelativePathError
	if err := env(home, map[string]string{"CODEX_HOME": "~/codex"}).Validate(); !errors.As(err, &re) {
		t.Errorf("CODEX_HOME=~/codex: Validate = %v", err)
	}
}

func TestShellHint(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "$env:EVREN_API_KEY = (ulgen token)",
		"linux":   `export EVREN_API_KEY="$(ulgen token)"`,
		"darwin":  `export EVREN_API_KEY="$(ulgen token)"`,
		"":        `export EVREN_API_KEY="$(ulgen token)"`,
	} {
		if got := ShellHint(goos); got != want {
			t.Errorf("ShellHint(%q) = %q, want %q", goos, got, want)
		}
	}
}

// exportHint returns the argument of the plan's note about the shell.
func exportHint(t *testing.T, p Plan) any {
	t.Helper()
	for _, n := range p.Notes {
		if n.Key == "setup.note.export" {
			if len(n.Args) != 1 {
				t.Fatalf("the note has %d arguments, want the hint", len(n.Args))
			}
			return n.Args[0]
		}
	}
	t.Fatal("the plan has no note about the shell")
	return nil
}

func TestTheShellNoteFollowsTheSystem(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "$env:EVREN_API_KEY = (ulgen token)",
		"linux":   `export EVREN_API_KEY="$(ulgen token)"`,
	} {
		env := testEnv(t, nil, nil)
		env.GOOS = goos
		for _, tool := range []Tool{Qwen{}, Opencode{}} {
			p, err := tool.Plan(env, testRequest())
			if err != nil {
				t.Fatal(err)
			}
			if got := exportHint(t, p); got != want {
				t.Errorf("%s on %s: hint %q, want %q", tool.ID(), goos, got, want)
			}
		}
	}
}

// relativeLookups counts the LookPath calls an adapter makes with a name
// that has a directory in it but is not absolute: such a name would be
// looked up in the current directory.
func relativeLookups(env *Env) *[]string {
	var bad []string
	env.LookPath = func(name string) (string, error) {
		if filepath.Base(name) != name && !filepath.IsAbs(name) {
			bad = append(bad, name)
		}
		return "", errors.New("not found")
	}
	return &bad
}

func TestNoAdapterBuildsAPathFromAnUnknownHome(t *testing.T) {
	ctx := context.Background()
	for _, home := range []string{"", "relative-home"} {
		env := testEnv(t, nil, nil)
		env.Home = home
		bad := relativeLookups(&env)
		for _, tool := range []Tool{Qwen{}, Opencode{}} {
			if d := tool.Detect(ctx, env); d.Installed {
				t.Errorf("home %q: %s detected: %+v", home, tool.ID(), d)
			}
			var he *HomeError
			if ok, err := tool.Configured(env); ok || !errors.As(err, &he) {
				t.Errorf("home %q: %s Configured = %v, %v", home, tool.ID(), ok, err)
			}
			p, err := tool.Plan(env, testRequest())
			if !errors.As(err, &he) || len(p.Files) != 0 {
				t.Errorf("home %q: %s Plan = %v, files %v", home, tool.ID(), err, paths(p))
			}
			files, err := tool.(interface {
				UserFiles(Env) ([]string, error)
			}).UserFiles(env)
			if !errors.As(err, &he) || len(files) != 0 {
				t.Errorf("home %q: %s UserFiles = %v, %v", home, tool.ID(), files, err)
			}
		}
		if len(*bad) != 0 {
			t.Errorf("home %q: LookPath was asked for %q", home, *bad)
		}
	}
}

func TestARelativeDirectoryVariableIsNotUsed(t *testing.T) {
	rel := filepath.Join("rel", "dir")
	for _, tc := range []struct {
		tool Tool
		name string
	}{{Qwen{}, "QWEN_HOME"}, {Opencode{}, "OPENCODE_CONFIG_DIR"}} {
		env := testEnv(t, map[string]string{tc.name: rel}, nil)
		var re *RelativePathError
		if ok, err := tc.tool.Configured(env); ok || !errors.As(err, &re) || re.Var != tc.name {
			t.Errorf("%s: Configured = %v, %v", tc.name, ok, err)
		}
		p, err := tc.tool.Plan(env, testRequest())
		if !errors.As(err, &re) || re.Var != tc.name || len(p.Files) != 0 {
			t.Errorf("%s: Plan = %v, files %v", tc.name, err, paths(p))
		}
	}

	// A relative XDG_CONFIG_HOME counts as not set: the file is the one
	// under the home directory.
	env := testEnv(t, map[string]string{"XDG_CONFIG_HOME": "xdgrel"}, nil)
	p, err := Opencode{}.Plan(env, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	planned(t, p, filepath.Join(env.Home, ".config", "opencode", "opencode.json"))
	for _, f := range p.Files {
		if !filepath.IsAbs(f.Path) {
			t.Errorf("the plan has a relative path: %s", f.Path)
		}
	}
}

func TestDetectIgnoresARelativeLookPathResult(t *testing.T) {
	ctx := context.Background()
	for _, tool := range []Tool{Qwen{}, Opencode{}, Codex{}} {
		env := testEnv(t, nil, nil)
		asked := 0
		env.LookPath = func(name string) (string, error) {
			return filepath.Join("bin", filepath.Base(name)), nil // what a PATH entry "bin" would give
		}
		env.Version = func(context.Context, string) (string, error) {
			asked++
			return "1.2.3", nil
		}
		if d := tool.Detect(ctx, env); d.Installed || d.Binary != "" || asked != 0 {
			t.Errorf("%s: %+v, %d programs were run", tool.ID(), d, asked)
		}
	}
}

func TestUserFiles(t *testing.T) {
	env := testEnv(t, nil, nil)
	files, err := Qwen{}.UserFiles(env)
	if err != nil || len(files) != 1 || files[0] != filepath.Join(env.Home, ".qwen", "settings.json") {
		t.Errorf("qwen: %v, %v", files, err)
	}
	files, err = Opencode{}.UserFiles(env)
	dir := filepath.Join(env.Home, ".config", "opencode")
	if err != nil || len(files) != 2 || files[0] != filepath.Join(dir, "opencode.json") || files[1] != filepath.Join(dir, "opencode.jsonc") {
		t.Errorf("opencode: %v, %v", files, err)
	}
	other := t.TempDir()
	env = testEnv(t, map[string]string{"QWEN_HOME": other, "OPENCODE_CONFIG_DIR": other}, nil)
	if files, _ = (Qwen{}).UserFiles(env); len(files) != 1 || files[0] != filepath.Join(other, "settings.json") {
		t.Errorf("qwen with QWEN_HOME: %v", files)
	}
	if files, _ = (Opencode{}).UserFiles(env); len(files) != 2 || files[0] != filepath.Join(other, "opencode.json") {
		t.Errorf("opencode with OPENCODE_CONFIG_DIR: %v", files)
	}
}

func TestQwenNodeNoteIsCleaned(t *testing.T) {
	env := testEnv(t, nil, map[string]string{"node": "v18.20.4\x1b]0;owned\x07\x1b[2J\n"})
	d := Qwen{}.Detect(context.Background(), env)
	if len(d.Notes) != 1 || d.Notes[0].Key != "setup.note.node_old" || d.Notes[0].Args[0] != "v18.20.4" {
		t.Errorf("notes = %+v", d.Notes)
	}
}
