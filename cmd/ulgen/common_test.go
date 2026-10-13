package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/i18n"
)

// TestSubUsageAlignsTurkish checks that the option descriptions of a Turkish
// usage start in the same column. The byte length of "-x metin" and "-n sayı"
// is five, but their rune length is not equal, so a byte-based width would
// leave the two descriptions at different columns.
func TestSubUsageAlignsTurkish(t *testing.T) {
	tr := i18n.New("tr")
	fs := flag.NewFlagSet("ulgen status", flag.ContinueOnError)
	fs.String("x", "", "ortak açıklama")
	fs.Int("n", 0, "ortak açıklama")
	lines := strings.Split(strings.TrimRight(subUsage(tr, "status", fs), "\n"), "\n")
	var cols []int
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimLeft(line, " "), "-") {
			continue
		}
		i := strings.Index(line, "ortak açıklama")
		if i < 0 {
			t.Fatalf("line %q has no description", line)
		}
		cols = append(cols, utf8.RuneCountInString(line[:i]))
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("descriptions start at columns %v, want two equal columns:\n%s", cols, subUsage(tr, "status", fs))
	}
}

// Recorded EVREN answers (2026-10-11).
const (
	fxStatusAccepted    = `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":true}`
	fxStatusNotAccepted = `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":false}`
	fxTermsText         = `{"version":1,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","content":"# EVREN LLM Gateway — Kullanım Şartları\n\nTERMS BODY LINE"}`
	fxAccept            = `{"accepted_version":1,"accepted_at":"2026-10-11T18:08:07.696671+00:00"}`
	fxQuota             = `{"used_tokens":10,"cap":2500000,"window_minutes":5,"reset_at":"2026-10-11T18:09:00+00:00","usage_ratio":4e-06,"level":"ok","held_cr":"0","remaining_cr":"1009.9898","daily_used_tokens":4080245,"daily_limit_tokens":15000000,"daily_remaining_tokens":10919755,"daily_reset_at":"2026-10-12T00:00:00Z"}`
	fxModels            = `{"object":"list","data":[` +
		`{"id":"dots-ocr","object":"model","task":"ocr","context_length":131072,"default_max_output_tokens":null,"capabilities":{"tools":false,"thinking":false,"vision":false},"pricing":{"free_until":"2026-11-01"}},` +
		`{"id":"deepseek-v4.1-flash","object":"model","task":"chat","context_length":1048576,"default_max_output_tokens":65536,"capabilities":{"tools":true,"thinking":true,"vision":true},"pricing":{"free_until":"2026-11-01"}},` +
		`{"id":"glm-5.3","object":"model","task":"chat","context_length":524288,"default_max_output_tokens":null,"capabilities":{"tools":true,"thinking":true,"vision":false},"pricing":{"free_until":"2026-11-01"}}]}`
	fxUnauthorized = `{"error":{"message":"Geçersiz API anahtarı","type":"authentication_error","param":null,"code":"unauthorized"}}`
	fxTermsGate    = `{"error":{"message":"Kullanım koşulları kabul edilmedi","type":"permission_error","code":"terms_not_accepted","required_version":1}}`
)

type answer struct {
	status int
	body   string
}

type recorded struct {
	Method, Path, Auth, Body string
}

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.reqs...)
}

// paths returns the request paths in order.
func (r *recorder) paths() []string {
	var out []string
	for _, q := range r.all() {
		out = append(out, q.Path)
	}
	return out
}

// evrenServer serves canned answers per path and records every request.
func evrenServer(t *testing.T, routes map[string]answer) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	mux := http.NewServeMux()
	for path, a := range routes {
		a := a
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			rec.mu.Lock()
			rec.reqs = append(rec.reqs, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), string(b)})
			rec.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if a.status != 0 {
				w.WriteHeader(a.status)
			}
			io.WriteString(w, a.body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

// healthy is the route set of an account that is signed in with the terms accepted.
func healthy() map[string]answer {
	return map[string]answer{
		"/terms/status": {body: fxStatusAccepted},
		"/terms/text":   {body: fxTermsText},
		"/terms/accept": {body: fxAccept},
		"/quota":        {body: fxQuota},
		"/models":       {body: fxModels},
	}
}

// useFileStore makes the command use a file store in a temporary directory
// (never the real keyring), makes that directory Ülgen's configuration
// directory unless the test sets ULGEN_CONFIG_DIR, and pins the clock and
// time zone. It returns the store so tests can seed or inspect it.
func useFileStore(t *testing.T) credential.Store {
	t.Helper()
	dir := t.TempDir()
	store := credential.NewFileStore(dir)
	savedOpen, savedDir, savedNow, savedZone := openStore, defaultConfigDir, now, localZone
	openStore = func(string, func(string) string) (credential.Store, error) { return store, nil }
	defaultConfigDir = func(getenv func(string) string) (string, error) {
		if v := strings.TrimSpace(getenv(configDirVar)); v != "" {
			return v, nil
		}
		return dir, nil
	}
	now = func() time.Time { return time.Date(2026, 10, 11, 21, 48, 0, 0, time.UTC) } // 00:48 Istanbul; reset at 03:00 is 2 h 12 min away
	localZone = func() *time.Location { return time.FixedZone("TRT", 3*60*60) }
	t.Cleanup(func() { openStore, defaultConfigDir, now, localZone = savedOpen, savedDir, savedNow, savedZone })
	return store
}

// The API root is written into other programs' configuration files, so a
// value with a control character in it, or one that is not text at all, is
// refused even where the address would be allowed.
func TestCheckBaseURLRefusesWhatCannotBeWritten(t *testing.T) {
	for name, raw := range map[string]string{
		"a line break":       "https://x.example/v1\n",
		"a tab":              "https://x.example/\tv1",
		"an escape":          "https://x.example/\x1b[2Jv1",
		"DEL":                "https://x.example/v1\x7f",
		"a C1 control":       "https://x.example/\u0085v1",
		"a C1 escape":        "https://x.example/\u009b31mv1",
		"a byte, not text":   "https://x.example/\xff",
		"half a character":   "https://x.example/\xc3",
		"on this machine":    "http://127.0.0.1:4141/\u0085v1",
		"in the host itself": "https://x.\u0085example/v1",
	} {
		if err := checkBaseURL(raw); !errors.Is(err, errUnsafeBaseURL) {
			t.Errorf("%s (%q): %v, want errUnsafeBaseURL", name, raw, err)
		}
	}
	// Other text than ASCII is fine: a root is text, not only an address.
	for _, raw := range []string{"https://örnek.example/v1", "https://x.example/yol/ş", "http://localhost:4141/v1"} {
		if err := checkBaseURL(raw); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
	// The command says so in the user's language and sends nothing.
	useFileStore(t)
	for _, args := range [][]string{{"status"}, {"setup", "qwen"}} {
		code, stdout, stderr := exec(t, context.Background(), map[string]string{"LANG": "tr_TR.UTF-8"}, append(args, "--base-url", "https://x.example/\u0085v1")...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "ulgen: --base-url https kullanmalı ya da yalnızca bu makinede (localhost) http olmalı\n") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestSafeArg(t *testing.T) {
	for in, want := range map[string]string{
		"evren_llm_abc123DEF":         "[key]",
		"--model=evren_llm_abc":       "--model=[key]",
		"a evren_llm_x b evren_llm_y": "a [key] b [key]",
		"evren_llm_":                  "[key]",
		"evren_llm_\x1b[2Jabc\ndef":   "[key] def", // the escape sequence does not split the key
		"plain":                       "plain",
		"evren":                       "evren",
		"two\nlines\x1b]0;title\x07":  "two lines",
		"":                            "",
	} {
		if got := safeArg(in); got != want {
			t.Errorf("safeArg(%q) = %q, want %q", in, got, want)
		}
	}
}

// A word that is not a command is printed back, but not when it looks like
// a key: someone pasted it in the wrong place.
func TestAPastedKeyIsNotPrintedBack(t *testing.T) {
	for _, args := range [][]string{{"evren_llm_PASTED"}, {"-evren_llm_PASTED"}, {"status", "evren_llm_PASTED"}, {"logout", "--evren_llm_PASTED"}} {
		code, stdout, stderr := exec(t, context.Background(), nil, args...)
		if code != exitUsage || stdout != "" || strings.Contains(stderr, "PASTED") || !strings.Contains(stderr, "[key]") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}
