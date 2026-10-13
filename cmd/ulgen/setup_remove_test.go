package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRemove(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	settings := filepath.Join(env.Home, ".qwen", "settings.json")
	before := readFile(t, settings)

	// A dry run shows the change and writes nothing.
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "--remove", "qwen", "--dry-run")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `-        "envKey": "EVREN_API_KEY"`) || !strings.Contains(stdout, "nothing was written") {
		t.Fatalf("dry run: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if readFile(t, settings) != before {
		t.Fatal("the dry run wrote the file")
	}

	// The removal needs no key and asks EVREN nothing: there is no server here.
	useFileStore(t) // an empty store: signed out
	code, stdout, stderr = exec(t, context.Background(), vars, "setup", "--remove", "qwen")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "updated (+0 -") || !strings.Contains(stdout, "backup: ") || !strings.Contains(stdout, "EVREN is no longer set up in Qwen Code.") || strings.Contains(stdout, "Start it with") {
		t.Errorf("stdout = %q", stdout)
	}
	if after := readFile(t, settings); strings.Contains(after, "EVREN") {
		t.Errorf("settings still name EVREN:\n%s", after)
	}

	// A second run finds nothing.
	code, stdout, _ = exec(t, context.Background(), vars, "setup", "--remove", "qwen")
	if code != 0 || !strings.Contains(stdout, "nothing of Ülgen's to remove") {
		t.Errorf("second run: code %d stdout %q", code, stdout)
	}
}

func TestSetupRemoveAllCoversToolsThatAreNotInstalled(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"codex": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "codex")
	profile := filepath.Join(env.Home, ".codex", "evren.config.toml")
	// Codex is uninstalled; its file is still there.
	env.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	useEnv(t, env)
	code, stdout, stderr := exec(t, context.Background(), vars, "setup", "--remove", "all")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("the profile is still there: %v", err)
	}
	if !strings.Contains(stdout, "removed") || strings.Contains(stdout, "npm install") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSetupRemoveForgetsTheGuidesTool(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	setUp(t, vars, "opencode")
	useFakeExec(t, 0, nil)
	useGuide(t, "", false)
	exec(t, context.Background(), vars, "qwen") // remembered
	getenv := func(k string) string { return vars[k] }
	if readGuideTool(getenv) != "qwen" {
		t.Fatal("the guide did not remember the tool")
	}
	exec(t, context.Background(), vars, "setup", "--remove", "opencode")
	if readGuideTool(getenv) != "qwen" {
		t.Error("removing another tool forgot the guide's tool")
	}
	exec(t, context.Background(), vars, "setup", "--remove", "qwen")
	if readGuideTool(getenv) != "" {
		t.Error("the guide still remembers a tool that is no longer set up")
	}
}

func TestSetupRemoveUsage(t *testing.T) {
	useSetupEnv(t, nil)
	for _, args := range [][]string{
		{"setup", "--remove"},
		{"setup", "--remove", "qwen", "--model", "x"},
		{"setup", "--remove", "qwen", "--default"},
		{"setup", "--remove", "nope"},
	} {
		if code, _, stderr := exec(t, context.Background(), nil, args...); code != exitUsage || !strings.Contains(stderr, "Usage:") {
			t.Errorf("%v: code %d stderr %q", args, code, stderr)
		}
	}
}
