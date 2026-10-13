package evren

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mftnakrsu/ulgen/internal/provider"
)

// Recorded answers from EVREN on 2026-10-11.
const (
	statusNotAccepted = `{"current_version":1,"is_material":true,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","accepted":false}`
	termsText         = `{"version":1,"doc_path":"docs/legal/LLM_TERMS_OF_SERVICE_v1.md","content":"# EVREN LLM Gateway — Kullanım Şartları\n\nMetin."}`
	acceptAnswer      = `{"accepted_version":1,"accepted_at":"2026-10-11T18:08:07.696671+00:00"}`
	quotaAnswer       = `{"used_tokens":10,"cap":2500000,"window_minutes":5,"reset_at":"2026-10-11T18:09:00+00:00","usage_ratio":4e-06,"level":"ok","held_cr":"0","remaining_cr":"1009.9898","daily_used_tokens":4080245,"daily_limit_tokens":15000000,"daily_remaining_tokens":10919755,"daily_reset_at":"2026-10-12T00:00:00Z"}`
	modelsAnswer      = `{"object":"list","data":[` +
		`{"id":"dots-ocr","object":"model","task":"ocr","context_length":131072,"default_max_output_tokens":null,"capabilities":{"tools":false,"thinking":false,"vision":false},"pricing":{"prompt_token_price":0.0,"completion_token_price":0.0,"currency":"CR","free_until":"2026-11-01"}},` +
		`{"id":"deepseek-v4.1-flash","object":"model","task":"chat","context_length":1048576,"default_max_output_tokens":65536,"capabilities":{"tools":true,"thinking":true,"vision":true},"pricing":{"free_until":"2026-11-01"}},` +
		`{"id":"qwen3-vl-30b","object":"model","task":"vision_chat","context_length":262144,"default_max_output_tokens":16384,"capabilities":{"tools":true,"thinking":false,"vision":true},"pricing":{"free_until":"2026-11-01"}}]}`
	unauthorized = `{"error":{"message":"Geçersiz API anahtarı","type":"authentication_error","param":null,"code":"unauthorized"}}`
)

type answer struct {
	status int
	body   string
}

type recorded struct {
	Method, Path, Auth, ContentType, Body string
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

// newServer serves one canned answer per path and records every request.
func newServer(t *testing.T, routes map[string]answer) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	mux := http.NewServeMux()
	for path, a := range routes {
		a := a
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			rec.mu.Lock()
			rec.reqs = append(rec.reqs, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)})
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

func TestTermsStatus(t *testing.T) {
	srv, rec := newServer(t, map[string]answer{"/terms/status": {body: statusNotAccepted}})
	st, err := New("evren_llm_k", srv.URL, "ulgen-test").TermsStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentVersion != 1 || !st.IsMaterial || st.Accepted {
		t.Errorf("status = %+v", st)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != "GET" || reqs[0].Auth != "Bearer evren_llm_k" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestTermsText(t *testing.T) {
	srv, _ := newServer(t, map[string]answer{"/terms/text": {body: termsText}})
	terms, err := New("k", srv.URL, "").TermsText(context.Background())
	if err != nil || terms.Version != 1 || !strings.HasPrefix(terms.Content, "# EVREN LLM Gateway") {
		t.Errorf("terms = %+v, %v", terms, err)
	}
}

func TestAcceptTerms(t *testing.T) {
	srv, rec := newServer(t, map[string]answer{"/terms/accept": {body: acceptAnswer}})
	acc, err := New("k", srv.URL, "").AcceptTerms(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 11, 18, 8, 7, 696671000, time.UTC)
	if acc.Version != 1 || !acc.AcceptedAt.Equal(want) {
		t.Errorf("acceptance = %+v", acc)
	}
	reqs := rec.all()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Body != `{"version":1}` || reqs[0].ContentType != "application/json" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestQuota(t *testing.T) {
	srv, _ := newServer(t, map[string]answer{"/quota": {body: quotaAnswer}})
	q, err := New("k", srv.URL, "").Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q.DailyLimit != 15000000 || q.DailyRemaining != 10919755 || q.DailyUsed != 4080245 || q.WindowUsed != 10 || q.WindowCap != 2500000 || q.WindowMinutes != 5 || q.Level != "ok" {
		t.Errorf("quota = %+v", q)
	}
	if !q.DailyReset.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) || !q.WindowReset.Equal(time.Date(2026, 10, 11, 18, 9, 0, 0, time.UTC)) {
		t.Errorf("reset times = %v %v", q.DailyReset, q.WindowReset)
	}
	if q.CreditsRemaining != "1009.9898" || q.CreditsHeld != "0" {
		t.Errorf("credits = %q %q", q.CreditsRemaining, q.CreditsHeld)
	}
}

func TestQuotaCreditsAsNumbers(t *testing.T) {
	body := strings.Replace(strings.Replace(quotaAnswer, `"remaining_cr":"1009.9898"`, `"remaining_cr":1009.9898`, 1), `"held_cr":"0"`, `"held_cr":0`, 1)
	srv, _ := newServer(t, map[string]answer{"/quota": {body: body}})
	q, err := New("k", srv.URL, "").Quota(context.Background())
	if err != nil || q.CreditsRemaining != "1009.9898" || q.CreditsHeld != "0" {
		t.Errorf("quota = %+v, %v", q, err)
	}
}

func TestModels(t *testing.T) {
	srv, _ := newServer(t, map[string]answer{"/models": {body: modelsAnswer}})
	models, err := New("k", srv.URL, "").Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 {
		t.Fatalf("models = %+v", models)
	}
	ocr, ds, vl := models[0], models[1], models[2]
	if ocr.ID != "dots-ocr" || ocr.Task != "ocr" || ocr.MaxOutput != 0 || IsChat(ocr) {
		t.Errorf("ocr = %+v", ocr)
	}
	if ds.ID != "deepseek-v4.1-flash" || !IsChat(ds) || ds.ContextLength != 1048576 || ds.MaxOutput != 65536 || !ds.Tools || !ds.Thinking || !ds.Vision || ds.FreeUntil != "2026-11-01" {
		t.Errorf("deepseek = %+v", ds)
	}
	if vl.Task != "vision_chat" || !IsChat(vl) || vl.Thinking {
		t.Errorf("qwen3-vl = %+v", vl)
	}
}

func TestIsChat(t *testing.T) {
	cases := []struct {
		task string
		want bool
	}{
		{"chat", true},
		{"vision_chat", true},
		{"vision chat", true},
		{"ocr", false},
		{"embedding", false},
		{"decision", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsChat(Model{Task: c.task}); got != c.want {
			t.Errorf("IsChat(%q) = %v, want %v", c.task, got, c.want)
		}
	}
}

func TestUnauthorizedSurfacesAsAPIError(t *testing.T) {
	srv, _ := newServer(t, map[string]answer{"/terms/status": {status: 401, body: unauthorized}})
	_, err := New("bad", srv.URL, "").TermsStatus(context.Background())
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || !apiErr.IsUnauthorized() || apiErr.Code != "unauthorized" {
		t.Errorf("err = %#v", err)
	}
}

func TestNewUsesPreset(t *testing.T) {
	c := New("k", "", "ulgen/1.0")
	if c.Preset.ID != provider.EVREN.ID || c.BaseURL != "" || c.APIKey != "k" || c.UserAgent != "ulgen/1.0" {
		t.Errorf("client = %+v", c.Client)
	}
	if New("k", "", "").UserAgent == "" {
		t.Error("empty user agent should keep the provider default")
	}
}
