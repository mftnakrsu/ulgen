package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// What Claude Code asks for (thinking, output_config.effort) becomes EVREN's
// reasoning_effort for a model that takes one.
func TestReasoningEffort(t *testing.T) {
	all := []string(nil) // a model that lists no values
	mimo := []string{"none", "minimal", "low", "medium", "high", "xhigh"}
	for _, tc := range []struct {
		name, body string
		values     []string
		want       string
	}{
		{"Claude Code's default", `{"thinking":{"type":"adaptive","display":"omitted"},"output_config":{"effort":"high"}}`, all, "high"},
		{"low", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`, all, "low"},
		{"medium", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`, all, "medium"},
		{"xhigh", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`, all, "xhigh"},
		// Not every EVREN model takes "max"; all of them take "xhigh".
		{"max", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`, all, "xhigh"},
		{"max for a model with a list", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`, mimo, "xhigh"},
		// Thinking left out, as Claude Code does when it is turned off, or
		// turned off by name: the model is asked not to reason.
		{"thinking left out", `{"output_config":{"effort":"high"}}`, all, "none"},
		{"thinking disabled", `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`, all, "none"},
		{"no thinking, no effort", `{}`, all, "none"},
		// Thinking on without an effort: the model's own default.
		{"thinking with a budget", `{"thinking":{"type":"enabled","budget_tokens":4096}}`, all, ""},
		{"adaptive without an effort", `{"thinking":{"type":"adaptive"}}`, all, ""},
		{"an effort EVREN does not know", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"extreme"}}`, all, ""},
		{"an effort in another case", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"LOW"}}`, all, ""},
		// A value the model's own list leaves out is not sent.
		{"none missing from the list", `{}`, []string{"low", "medium", "high"}, ""},
		{"xhigh missing from the list", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`, []string{"low", "medium", "high"}, ""},
		{"in the list", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`, mimo, "medium"},
	} {
		var req anthropicRequest
		if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := reasoningEffort(req, tc.values); got != tc.want {
			t.Errorf("%s: reasoningEffort = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The translator sends reasoning_effort only to a model that takes one: the
// default model here, not the small one.
func TestMessagesSendReasoningEffort(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c1","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	s, err := New(Config{
		BaseURL: st.URL,
		Key:     testKey,
		Token:   testToken,
		Models:  []string{"evren-large", "deepseek-v4-flash"},
		Default: "evren-large",
		Small:   "deepseek-v4-flash",
		Efforts: map[string][]string{"evren-large": nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := map[string]string{"Authorization": "Bearer " + testToken}
	for _, tc := range []struct {
		name, body string
		want       any
	}{
		{"low", `{"model":"claude-sonnet-4","thinking":{"type":"adaptive"},"output_config":{"effort":"low"},"messages":[{"role":"user","content":"hi"}]}`, "low"},
		{"thinking off", `{"model":"claude-sonnet-4","output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`, "none"},
		{"no effort", `{"model":"claude-sonnet-4","thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hi"}]}`, nil},
		{"a model that takes none", `{"model":"claude-haiku-4","thinking":{"type":"adaptive"},"output_config":{"effort":"low"},"messages":[{"role":"user","content":"hi"}]}`, nil},
	} {
		if rec := do(t, s, "/v1/messages", tc.body, auth); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		got, present := st.last(t).body["reasoning_effort"]
		if tc.want == nil && present || tc.want != nil && got != tc.want {
			t.Errorf("%s: reasoning_effort = %v (present %v), want %v", tc.name, got, present, tc.want)
		}
	}
}
