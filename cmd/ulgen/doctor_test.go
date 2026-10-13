package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorAllWell(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n", "opencode": "1.0.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	srv, _ := evrenServer(t, healthy())
	code, stdout, stderr := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, want := range []string{
		"ok  Signed in",
		"ok  EVREN answers and accepts the key",
		"ok  Daily tokens: 10,919,755 of 15,000,000 left",
		"ok  Qwen Code 0.25.0: set up",
		"--  opencode 1.0.0: not set up",
		"ulgen setup opencode",
		"--  Codex CLI: not installed",
		"Everything needed is in place",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "!!") {
		t.Errorf("a problem was reported:\n%s", stdout)
	}
}

func TestDoctorNotSignedIn(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	useFileStore(t)
	srv, rec := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), nil, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  Not signed in") || !strings.Contains(stdout, "ulgen login") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
	if len(rec.all()) != 0 {
		t.Errorf("EVREN was asked without a key: %v", rec.paths())
	}
	// The tools are still looked at: they need no key.
	if !strings.Contains(stdout, "Qwen Code 0.25.0") {
		t.Errorf("the tools are missing:\n%s", stdout)
	}
}

func TestDoctorKeyRefused(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	routes := healthy()
	routes["/quota"] = answer{status: 401, body: fxUnauthorized}
	srv, _ := evrenServer(t, routes)
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  the stored key is no longer valid; run ulgen login") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorTermsNotAccepted(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	routes := healthy()
	routes["/quota"] = answer{status: 403, body: fxTermsGate}
	srv, _ := evrenServer(t, routes)
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  the terms are not accepted; run ulgen login") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorEvrenUnreachable(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	url := srv.URL
	srv.Close()
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", url)
	if code != exitError || !strings.Contains(stdout, "!!  ") || !strings.Contains(stdout, "internet connection") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorQuotaUsedUp(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	routes := healthy()
	routes["/quota"] = answer{body: strings.Replace(fxQuota, `"daily_remaining_tokens":10919755`, `"daily_remaining_tokens":0`, 1)}
	srv, _ := evrenServer(t, routes)
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  Today's tokens are used up") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorNoToolInstalled(t *testing.T) {
	useSetupEnv(t, nil)
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  No supported coding tool is installed") || !strings.Contains(stdout, "npm install -g @qwen-code/qwen-code") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorSettingsFileThatCannotBeRead(t *testing.T) {
	env := useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	seedFile(t, filepath.Join(env.Home, ".qwen", "settings.json"), "{ this is not JSON")
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != exitError || !strings.Contains(stdout, "!!  Qwen Code 0.25.0: its file cannot be read") || !strings.Contains(stdout, "ulgen setup qwen") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorClaudeNeedsNoSetup(t *testing.T) {
	useSetupEnv(t, map[string]string{"claude": "2.1.0\n"})
	vars := signedIn(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "ok  Claude Code 2.1.0: ready") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorJSON(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	useFileStore(t)
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), nil, "doctor", "--json", "--base-url", srv.URL)
	var got struct {
		OK     bool `json:"ok"`
		Checks []struct {
			ID, State, Message, Remedy string
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if code != exitError || got.OK || len(got.Checks) < 2 || got.Checks[0].ID != "key" || got.Checks[0].State != "problem" || got.Checks[0].Remedy == "" {
		t.Errorf("code %d report %+v", code, got)
	}
}

func TestDoctorInTurkish(t *testing.T) {
	useSetupEnv(t, map[string]string{"qwen": "0.25.0\n"})
	vars := signedIn(t)
	setUp(t, vars, "qwen")
	vars["LANG"] = "tr_TR.UTF-8"
	srv, _ := evrenServer(t, healthy())
	code, stdout, _ := exec(t, context.Background(), vars, "doctor", "--base-url", srv.URL)
	if code != 0 || !strings.Contains(stdout, "ok  Giriş yapılmış") || !strings.Contains(stdout, "Qwen Code 0.25.0: ayarlı") {
		t.Errorf("code %d stdout:\n%s", code, stdout)
	}
}

func TestDoctorUsage(t *testing.T) {
	code, _, stderr := exec(t, context.Background(), nil, "doctor", "extra")
	if code != exitUsage || !strings.Contains(stderr, "Usage:") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}
