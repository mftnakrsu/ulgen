package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// stub is a fake server. It records every request and
// lets the test decide the answer for the n-th call (1-based).
type stub struct {
	t       *testing.T
	mu      sync.Mutex
	bodies  []map[string]any
	headers []http.Header
	paths   []string
	srv     *httptest.Server
}

func newStub(t *testing.T, handler func(n int, w http.ResponseWriter, r *http.Request)) *stub {
	s := &stub{t: t}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("stub: request body is not JSON: %v", err)
			}
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.headers = append(s.headers, r.Header.Clone())
		s.paths = append(s.paths, r.URL.Path)
		n := len(s.bodies)
		s.mu.Unlock()
		handler(n, w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) client(p Preset, key string) *Client {
	c := NewClient(p, key)
	c.BaseURL = s.srv.URL
	c.RetryBase = time.Millisecond // retry pauses are not what these tests measure
	return c
}

func (s *stub) body(i int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

func (s *stub) header(i int) http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers[i]
}

func (s *stub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *stub) path(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paths[i]
}

func TestAuthHeader(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	if _, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client(EVREN, "").JSON(context.Background(), http.MethodGet, "/quota", nil, nil); err != nil {
		t.Fatal(err)
	}
	if h := s.header(0); h.Get("Authorization") != "Bearer k" || h.Get("X-API-Key") != "" || h.Get("User-Agent") != "ulgen" {
		t.Errorf("headers with a key: %v", h)
	}
	if h := s.header(1); h.Get("Authorization") != "" || h.Get("X-API-Key") != "" {
		t.Errorf("auth headers sent without a key: %v", h)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		check      func(t *testing.T, e *APIError)
	}{
		{"openai shape", 401, `{"error":{"message":"bad key","type":"invalid_request_error","code":"invalid_api_key"}}`, "",
			func(t *testing.T, e *APIError) {
				if !e.IsUnauthorized() || e.Code != "invalid_api_key" || e.Message != "bad key" || e.Error() != "HTTP 401: bad key" {
					t.Errorf("%+v", e)
				}
			}},
		{"evren terms gate", 403, `{"code":"terms_not_accepted","message":"accept the terms","required_version":3}`, "",
			func(t *testing.T, e *APIError) {
				if !e.IsTermsNotAccepted() || e.TermsVersion != "3" || e.Message != "accept the terms" {
					t.Errorf("%+v", e)
				}
			}},
		{"terms gate nested", 403, `{"error":{"code":"terms_not_accepted","message":"x","required_version":"4"}}`, "",
			func(t *testing.T, e *APIError) {
				if !e.IsTermsNotAccepted() || e.TermsVersion != "4" {
					t.Errorf("%+v", e)
				}
			}},
		{"quota with retry-after", 429, `{"error":{"message":"quota exceeded"}}`, "7",
			func(t *testing.T, e *APIError) {
				if !e.IsQuotaExceeded() || e.RetryAfter != 7*time.Second || e.Message != "quota exceeded" {
					t.Errorf("%+v", e)
				}
			}},
		{"busy fastapi shape", 503, `{"detail":"busy"}`, "",
			func(t *testing.T, e *APIError) {
				if !e.IsBusy() || e.Message != "busy" {
					t.Errorf("%+v", e)
				}
			}},
		{"plain text body", 500, `oops`, "",
			func(t *testing.T, e *APIError) {
				if e.Message != "oops" || e.Code != "" || e.Status != 500 {
					t.Errorf("%+v", e)
				}
			}},
		{"error as string", 400, `{"error":"plain string error"}`, "",
			func(t *testing.T, e *APIError) {
				if e.Message != "plain string error" {
					t.Errorf("%+v", e)
				}
			}},
		{"empty body", 502, ``, "",
			func(t *testing.T, e *APIError) {
				if e.Error() != "HTTP 502" {
					t.Errorf("%+v", e)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			// A distinctive key, not a single letter: redaction replaces every
			// occurrence, and a one-character key would mangle ordinary words.
			c := s.client(EVREN, "evren_llm_mapping_key")
			c.MaxRetries = -1 // this test is about error mapping, not retrying
			_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Status != tt.status {
				t.Errorf("status = %d, want %d", apiErr.Status, tt.status)
			}
			tt.check(t, apiErr)
		})
	}
}

// TestErrorRedactsTheKey covers a server or proxy that repeats the
// Authorization value in its error body: the key must not survive in the
// message, the body or the code.
func TestErrorRedactsTheKey(t *testing.T) {
	const key = "evren_llm_secret_key"
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"bad request for %s","code":"%s"}}`, key, key)
	})
	c := s.client(EVREN, key)
	c.MaxRetries = -1
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	for _, got := range []string{apiErr.Error(), apiErr.Message, apiErr.Body, apiErr.Code} {
		if strings.Contains(got, key) {
			t.Errorf("%q still contains the key", got)
		}
		if !strings.Contains(got, "[key]") {
			t.Errorf("%q does not contain [key]", got)
		}
	}
}

// redactKey is long enough to be treated as a real key, and distinctive so a
// redaction test can look for any piece of it.
const redactKey = "evren_llm_abcdefghijkl"

// noKeyPiece fails when any five-character run of key appears in s. A key that
// was redacted only in part, or only after being cut, leaves such a run behind.
func noKeyPiece(t *testing.T, key, s string) {
	t.Helper()
	for i := 0; i+5 <= len(key); i++ {
		if strings.Contains(s, key[i:i+5]) {
			t.Errorf("output still contains %q from the key: %q", key[i:i+5], s)
			return
		}
	}
}

// TestRedactAcrossBodyCut covers the key lying across the 500-byte cut of the
// message: redaction runs first, so the cut-off piece is not printed.
func TestRedactAcrossBodyCut(t *testing.T) {
	body := []byte(strings.Repeat("x", 490) + redactKey + strings.Repeat("y", 600-490-len(redactKey)))
	e := newAPIErrorKey(500, body, "", redactKey)
	for _, s := range []string{e.Error(), e.Message, e.Body, e.Code} {
		noKeyPiece(t, redactKey, s)
	}
	if !strings.Contains(e.Message, "[key]") && !strings.Contains(e.Body, "[key]") {
		t.Errorf("no redaction marker at all: message %q body %q", e.Message, e.Body)
	}
}

// TestRedactAcrossBodyFieldCut covers the key across the 2000-byte cut of the
// Body field.
func TestRedactAcrossBodyFieldCut(t *testing.T) {
	body := []byte(strings.Repeat("x", 1990) + redactKey + strings.Repeat("y", 2100-1990-len(redactKey)))
	e := newAPIErrorKey(500, body, "", redactKey)
	for _, s := range []string{e.Error(), e.Message, e.Body, e.Code} {
		noKeyPiece(t, redactKey, s)
	}
	if !strings.Contains(e.Body, "[key]") {
		t.Errorf("Body has no redaction marker: %q", e.Body)
	}
}

// TestRedactJSONError covers a non-2xx answer on the JSON path.
func TestRedactJSONError(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"boom `+redactKey+`","code":"x"}}`)
	})
	c := s.client(EVREN, redactKey)
	c.MaxRetries = -1
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	for _, s := range []string{apiErr.Error(), apiErr.Message, apiErr.Body, apiErr.Code} {
		noKeyPiece(t, redactKey, s)
	}
}

// TestRedactDetailArray covers a FastAPI "detail" array whose message falls
// back to the body.
func TestRedactDetailArray(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"detail":[{"loc":["body","model"],"msg":"field `+redactKey+` required"}]}`)
	})
	c := s.client(EVREN, redactKey)
	c.MaxRetries = -1
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	for _, s := range []string{apiErr.Error(), apiErr.Message, apiErr.Body, apiErr.Code} {
		noKeyPiece(t, redactKey, s)
	}
}

// TestRedactErrorAfterRetry covers the error kept inside do when a long
// Retry-After stops the retry loop.
func TestRedactErrorAfterRetry(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600") // longer than maxRetryWait: the error is returned
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"message":"busy for `+redactKey+`"}}`)
	})
	c := s.client(EVREN, redactKey)
	c.RetryBase = time.Millisecond
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	for _, s := range []string{apiErr.Error(), apiErr.Message, apiErr.Body, apiErr.Code} {
		noKeyPiece(t, redactKey, s)
	}
}

// TestRedactLeavesShortKey covers a key shorter than minRedactLen: it is a
// placeholder, so nothing is replaced and the code still reads as EVREN's.
func TestRedactLeavesShortKey(t *testing.T) {
	// "e" occurs in the code and in the message: replacing it would break both.
	e := newAPIErrorKey(403, []byte(`{"code":"terms_not_accepted","message":"say e to continue"}`), "", "e")
	if !e.IsTermsNotAccepted() || e.Code != "terms_not_accepted" {
		t.Errorf("a one-letter key mangled the code: %+v", e)
	}
	if e.Message != "say e to continue" {
		t.Errorf("a one-letter key was redacted: %q", e.Message)
	}
	if redact("hello", "l") != "hello" || redact("hello", "") != "hello" {
		t.Error("redact changed text for a short or empty key")
	}
	// The limit itself: seven characters are left alone, eight are replaced.
	if got := redact("x abcdefg y", "abcdefg"); got != "x abcdefg y" {
		t.Errorf("a seven-character key was redacted: %q", got)
	}
	if got := redact("x abcdefgh y", "abcdefgh"); got != "x [key] y" {
		t.Errorf("an eight-character key was not redacted: %q", got)
	}
}

// TestRedactEscapedKeyInJSON covers a body that carries the key in an escaped
// form: the raw text does not hold the key, the decoded message and code do.
func TestRedactEscapedKeyInJSON(t *testing.T) {
	escaped := `\u0065` + redactKey[1:] // \u0065 is "e", the key's first letter
	if redactKey[0] != 'e' {
		t.Fatal("the test key must start with e")
	}
	body := `{"error":{"message":"bad key ` + escaped + ` sent","code":"` + escaped + `"}}`
	if strings.Contains(body, redactKey) {
		t.Fatal("the body must not hold the key literally")
	}
	e := newAPIErrorKey(400, []byte(body), "", redactKey)
	for _, s := range []string{e.Error(), e.Message, e.Code} {
		noKeyPiece(t, redactKey, s)
	}
	if !strings.Contains(e.Message, "[key]") {
		t.Errorf("message = %q", e.Message)
	}
	// EVREN's own shape, with the key where the terms version is read from.
	e = newAPIErrorKey(403, []byte(`{"code":"terms_not_accepted","message":"x","required_version":"`+escaped+`"}`), "", redactKey)
	noKeyPiece(t, redactKey, e.TermsVersion)
}

func TestJSONPostEncodesAndDecodes(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"accepted_version":3}`)
	})
	var out struct {
		Version int `json:"accepted_version"`
	}
	h, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodPost, "/terms/accept", map[string]int{"version": 3}, &out)
	if err != nil {
		t.Fatal(err)
	}
	gotType := s.header(0).Get("Content-Type")
	if out.Version != 3 || s.body(0)["version"] != 3.0 || gotType != "application/json" {
		t.Errorf("out = %+v body = %v type = %q", out, s.body(0), gotType)
	}
	if s.path(0) != "/terms/accept" || s.header(0).Get("Authorization") != "Bearer k" || h.Get("Content-Type") != "application/json" {
		t.Errorf("path = %q headers = %v answer headers = %v", s.path(0), s.header(0), h)
	}
}

func TestJSONGetSendsNoBody(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET carried a body: length %d type %q", r.ContentLength, r.Header.Get("Content-Type"))
		}
		io.WriteString(w, `{"accepted":true}`)
	})
	var out struct {
		Accepted bool `json:"accepted"`
	}
	if _, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/terms/status", nil, &out); err != nil || !out.Accepted {
		t.Errorf("err = %v out = %+v", err, out)
	}
}

func TestJSONNilOutIgnoresBody(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `not json at all`)
	})
	if _, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Errorf("err = %v, want nil when out is nil", err)
	}
}

func TestJSONErrorIsAPIError(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Geçersiz API anahtarı","type":"authentication_error","code":"unauthorized"}}`)
	})
	_, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.IsUnauthorized() || apiErr.Code != "unauthorized" {
		t.Errorf("error = %#v", err)
	}
}

func TestJSONRetriesBusy(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"message":"busy"}}`)
			return
		}
		io.WriteString(w, `{"ok":true}`)
	})
	c := s.client(EVREN, "k")
	c.RetryBase = time.Millisecond
	var out struct {
		OK bool `json:"ok"`
	}
	if _, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, &out); err != nil || !out.OK {
		t.Errorf("err = %v out = %+v", err, out)
	}
}

func TestJSONBadAnswer(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"accepted":`)
	})
	var out map[string]any
	_, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/terms/status", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "/terms/status") {
		t.Errorf("err = %v, want a decoding error naming the path", err)
	}
}

func TestErrorDetailArrayFallsBackToBody(t *testing.T) {
	e := buildAPIError(422, []byte(`{"detail":[{"loc":["body","model"],"msg":"field required"}]}`), "")
	if !strings.Contains(e.Message, "field required") || e.Error() == "HTTP 422" {
		t.Errorf("error = %+v", e)
	}
}

func TestHeaderTimeout(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	c := s.client(EVREN, "k")
	c.HeaderTimeout = 200 * time.Millisecond
	c.MaxRetries = 0
	start := time.Now()
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	calls := 0
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/elsewhere" {
			t.Error("the redirect target was requested")
		}
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	})
	_, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusFound || calls != 1 {
		t.Errorf("err = %v calls = %d", err, calls)
	}
}

func TestRetriesAreBounded(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"message":"busy"}}`)
	})
	c := s.client(EVREN, "k")
	_, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.IsBusy() || s.count() != 1+DefaultMaxRetries {
		t.Errorf("err = %v attempts = %d", err, s.count())
	}
	// A long Retry-After is not waited for.
	s2 := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"quota"}}`)
	})
	start := time.Now()
	_, err = s2.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	if !errors.As(err, &apiErr) || !apiErr.IsQuotaExceeded() || s2.count() != 1 || time.Since(start) > 2*time.Second {
		t.Errorf("err = %v attempts = %d after %s", err, s2.count(), time.Since(start))
	}
}

func TestNoRetryOnOtherErrors(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"boom"}}`)
	})
	_, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, nil)
	if err == nil || s.count() != 1 {
		t.Errorf("err = %v attempts = %d", err, s.count())
	}
}

func TestNoBaseURL(t *testing.T) {
	c := NewClient(Preset{ID: "empty"}, "k")
	if _, err := c.JSON(context.Background(), http.MethodGet, "/quota", nil, nil); err == nil || !strings.Contains(err.Error(), "no base URL") {
		t.Errorf("error = %v", err)
	}
}

func TestEVRENPreset(t *testing.T) {
	p := EVREN
	if p.ID != "evren" || p.BaseURL != "https://evren-llmapi.ssyz.org.tr/v1" || p.KeyEnv != "EVREN_API_KEY" || p.DefaultModel == "" {
		t.Errorf("evren preset = %+v", p)
	}
}

func TestRetryOnServerBusy(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"message":"busy"}}`)
			return
		}
		io.WriteString(w, `{"ok":true}`)
	})
	var out struct {
		OK bool `json:"ok"`
	}
	if _, err := s.client(EVREN, "k").JSON(context.Background(), http.MethodGet, "/quota", nil, &out); err != nil || !out.OK || s.count() != 3 {
		t.Errorf("err = %v out = %+v attempts = %d", err, out, s.count())
	}
}

func TestCancelledRequest(t *testing.T) {
	s := newStub(t, func(n int, w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.client(EVREN, "k").JSON(ctx, http.MethodGet, "/quota", nil, nil)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second {
		t.Errorf("err = %v after %s, want context.Canceled", err, time.Since(start))
	}
}
