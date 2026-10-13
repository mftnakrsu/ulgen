package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testKey and testToken are invented values; no real key is used in tests.
const (
	testKey   = "evren_llm_TESTONLY0000"
	testToken = "tok-TESTONLY"
)

// stub is a fake EVREN: it records the requests it received and answers
// through a handler. Nothing here touches the network.
type stub struct {
	*httptest.Server

	mu       sync.Mutex
	requests []recorded
}

// recorded is one request the stub saw.
type recorded struct {
	path   string
	header http.Header
	body   map[string]any
}

// newStub starts a stub provider, closed when the test ends.
func newStub(t *testing.T, handler http.HandlerFunc) *stub {
	t.Helper()
	s := &stub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{path: r.URL.Path, header: r.Header.Clone()}
		_ = json.Unmarshal(raw, &rec.body)
		s.mu.Lock()
		s.requests = append(s.requests, rec)
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// last returns the most recent request the stub saw.
func (s *stub) last(t *testing.T) recorded {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("the stub received no request")
	}
	return s.requests[len(s.requests)-1]
}

// newServer builds a proxy server pointed at the stub.
func newServer(t *testing.T, base string) *Server {
	t.Helper()
	s, err := New(Config{
		BaseURL: base,
		Key:     testKey,
		Token:   testToken,
		Models:  []string{"evren-large", "deepseek-v4-flash"},
		Default: "evren-large",
		Small:   "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// post sends a request to the server handler with the given headers.
func do(t *testing.T, s *Server, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestNewRefusesBadConfig covers the design's New rules.
func TestNewRefusesBadConfig(t *testing.T) {
	good := Config{BaseURL: "http://x", Key: "k", Token: "t", Models: []string{"m"}, Default: "m"}
	tests := []struct {
		name string
		cfg  Config
	}{
		{"empty key", Config{BaseURL: "http://x", Token: "t", Models: []string{"m"}, Default: "m"}},
		{"empty token", Config{BaseURL: "http://x", Key: "k", Models: []string{"m"}, Default: "m"}},
		{"empty default", Config{BaseURL: "http://x", Key: "k", Token: "t", Models: []string{"m"}}},
		{"default not in models", Config{BaseURL: "http://x", Key: "k", Token: "t", Models: []string{"m"}, Default: "n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil {
				t.Fatalf("New(%+v) must fail", tt.cfg)
			}
		})
	}
	if _, err := New(good); err != nil {
		t.Fatalf("New(good) = %v", err)
	}
}

// TestTokenCheck covers the token in either header, missing and wrong.
func TestTokenCheck(t *testing.T) {
	s := newServer(t, "http://127.0.0.1:0")
	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"bearer", map[string]string{"Authorization": "Bearer " + testToken}, http.StatusNotFound},
		{"x-api-key", map[string]string{"x-api-key": testToken}, http.StatusNotFound},
		{"missing", nil, http.StatusUnauthorized},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"wrong x-api-key", map[string]string{"x-api-key": "nope"}, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, s, "/v1/nothing", "", tt.headers)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestRoutes covers the 404 for everything but the two known routes.
func TestRoutes(t *testing.T) {
	s := newServer(t, "http://127.0.0.1:0")
	auth := map[string]string{"Authorization": "Bearer " + testToken}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		req := httptest.NewRequest(method, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /v1/models = %d, want 404", method, rec.Code)
		}
	}
	rec := do(t, s, "/v1/messages", `{}`, auth)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("POST /v1/messages must not be 404")
	}
}

// TestBodyLimit covers the 413 for a body over the limit.
func TestBodyLimit(t *testing.T) {
	s := newServer(t, "http://127.0.0.1:0")
	big := strings.Repeat("x", maxBodyBytes+1)
	rec := do(t, s, "/v1/messages", big, map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if kind := errorKindOf(t, rec.Body.Bytes()); kind != "request_too_large" {
		t.Fatalf("error kind = %q, want request_too_large", kind)
	}
}

// TestProviderStatusMapping covers the design's status table: the provider's
// status and message pass through, mapped to an Anthropic error kind. A
// refused key (401) and a refused request (403) are handled apart: they map
// to 400 invalid_request_error, so Claude Code does not tell the user to sign
// in to Anthropic.
func TestProviderStatusMapping(t *testing.T) {
	tests := []struct {
		status   int
		wantKind string
	}{
		{404, "not_found_error"},
		{413, "request_too_large"},
		{429, "rate_limit_error"},
		{400, "invalid_request_error"},
		{503, "overloaded_error"},
		{500, "api_error"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"error":{"message":"provider said no"}}`)
			})
			s := newServer(t, st.URL)
			rec := do(t, s, "/v1/messages", `{"model":"evren-large","messages":[{"role":"user","content":"hi"}]}`,
				map[string]string{"Authorization": "Bearer " + testToken})
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			kind, msg := errorOf(t, rec.Body.Bytes())
			if kind != tt.wantKind {
				t.Fatalf("error kind = %q, want %q", kind, tt.wantKind)
			}
			if msg != "provider said no" {
				t.Fatalf("message = %q, want provider said no", msg)
			}
		})
	}
}

// TestProviderAuthFailureIsInvalidRequest covers the fixed messages for a
// refused key (401) and a refused request (403): status 400,
// invalid_request_error, and the same English text in either language.
func TestProviderAuthFailureIsInvalidRequest(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{401, "EVREN did not accept the key; leave Claude Code and run: ulgen login"},
		{403, "EVREN refused the request (terms not accepted or access removed); leave Claude Code and run: ulgen login"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"error":{"message":"provider said no"}}`)
			})
			s := newServer(t, st.URL)
			rec := do(t, s, "/v1/messages", `{"model":"evren-large","messages":[{"role":"user","content":"hi"}]}`,
				map[string]string{"Authorization": "Bearer " + testToken})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			kind, msg := errorOf(t, rec.Body.Bytes())
			if kind != "invalid_request_error" {
				t.Fatalf("error kind = %q, want invalid_request_error", kind)
			}
			if msg != tt.want {
				t.Fatalf("message = %q, want %q", msg, tt.want)
			}
		})
	}
}

// TestProviderNetworkErrorIsBadGateway covers a provider that cannot be
// reached: 502 and api_error.
func TestProviderNetworkErrorIsBadGateway(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {})
	s := newServer(t, st.URL)
	st.Close() // now nothing listens on that address
	rec := do(t, s, "/v1/messages", `{"model":"evren-large","messages":[]}`,
		map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestNoKeyInErrorBody covers that the key never appears in an error body,
// even when the provider echoes it.
func TestNoKeyInErrorBody(t *testing.T) {
	tests := []struct {
		name string
		stub http.HandlerFunc
		// replaced is true when the body carries EVREN's message, so the key
		// must appear there as [key].
		replaced bool
	}{
		{"echoed in the message", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"bad key `+testKey+`"}}`)
		}, true},
		{"echoed in the raw body", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `not json `+testKey+` here`)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStub(t, tt.stub)
			s := newServer(t, st.URL)
			rec := do(t, s, "/v1/messages", `{"model":"evren-large","messages":[]}`,
				map[string]string{"Authorization": "Bearer " + testToken})
			if strings.Contains(rec.Body.String(), testKey) {
				t.Fatalf("the error body contains the key: %s", rec.Body.String())
			}
			if tt.replaced && !strings.Contains(rec.Body.String(), "[key]") {
				t.Fatalf("the error body does not replace the key: %s", rec.Body.String())
			}
		})
	}
}

// TestNonStreamingEndToEnd covers a full non-streaming turn through the
// server to the stub.
func TestNonStreamingEndToEnd(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"chatcmpl-1","model":"evren-large","choices":[{"message":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	})
	s := newServer(t, st.URL)
	rec := do(t, s, "/v1/messages", `{"model":"claude-opus-4","messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeResp(t, rec.Body.Bytes())
	if got["model"] != "evren-large" {
		t.Fatalf("model = %v, want evren-large", got["model"])
	}
	if got["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason = %v, want end_turn", got["stop_reason"])
	}
}

// TestProviderRequest covers what the provider received: the path, the bearer
// key, no client header passed on, and stream_options when streaming.
func TestProviderRequest(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	s := newServer(t, st.URL)
	rec := do(t, s, "/v1/messages", `{"model":"evren-large","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{
			"Authorization":     "Bearer " + testToken,
			"anthropic-version": "2023-06-01",
			"x-client-header":   "must-not-pass",
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := st.last(t)
	if got.path != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", got.path)
	}
	if got.header.Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("Authorization = %q, want the EVREN bearer key", got.header.Get("Authorization"))
	}
	if v := got.header.Get("X-Client-Header"); v != "" {
		t.Fatalf("a client header was passed on: X-Client-Header=%q", v)
	}
	if v := got.header.Get("Anthropic-Version"); v != "" {
		t.Fatalf("a client header was passed on: Anthropic-Version=%q", v)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got.header.Get("Content-Type"))
	}
	if got.body["stream"] != true {
		t.Fatalf("stream = %v, want true", got.body["stream"])
	}
	so, _ := got.body["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Fatalf("stream_options = %v, want include_usage true", got.body["stream_options"])
	}
}

// TestStreamResponse covers the streaming turn: SSE headers and the event
// sequence the client sees.
func TestStreamResponse(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	s := newServer(t, st.URL)
	rec := do(t, s, "/v1/messages", `{"model":"evren-large","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	names := sseNames(t, rec.Body.String())
	if len(names) == 0 || names[0] != "message_start" || names[len(names)-1] != "message_stop" {
		t.Fatalf("event names = %v, want message_start first and message_stop last", names)
	}
	for _, want := range []string{"content_block_start", "content_block_delta", "content_block_stop", "message_delta"} {
		if !contains(names, want) {
			t.Fatalf("event names %v lack %q", names, want)
		}
	}
	if strings.Contains(rec.Body.String(), "ping") {
		t.Fatalf("a ping was sent while events were flowing")
	}
}

// TestStreamPing covers a ping while the provider stays silent.
func TestStreamPing(t *testing.T) {
	release := make(chan struct{})
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-release // stay silent until the test lets go
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	s := newServer(t, st.URL)
	s.pingInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"evren-large","stream":true,"messages":[]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := &syncBuffer{}
	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	waitFor(t, rec, "ping")
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not finish")
	}
	if !strings.Contains(rec.String(), "event: ping") {
		t.Fatalf("no ping event in %q", rec.String())
	}
}

// TestClientDisconnectCancelsProvider covers that a client that goes away
// makes the provider see its request cancelled.
func TestClientDisconnectCancelsProvider(t *testing.T) {
	seen := make(chan struct{})
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // the proxy must cancel us when the client leaves
		close(seen)
	})
	s := newServer(t, st.URL)
	s.pingInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"evren-large","stream":true,"messages":[]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := &syncBuffer{}
	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	waitFor(t, rec, "ping")
	cancel() // the client goes away
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider never saw the request cancelled")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after the client left")
	}
}

// TestStreamProviderErrorMidStream covers an error after the first byte: one
// error event, no key.
func TestStreamProviderErrorMidStream(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		io.WriteString(w, "data: {\"error\":{\"message\":\"boom "+testKey+"\"}}\n\n")
	})
	s := newServer(t, st.URL)
	rec := do(t, s, "/v1/messages", `{"model":"evren-large","stream":true,"messages":[]}`,
		map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (headers already sent)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("no error event in %q", body)
	}
	if strings.Contains(body, testKey) {
		t.Fatalf("the error event contains the key: %q", body)
	}
	names := sseNames(t, body)
	if count(names, "error") != 1 {
		t.Fatalf("error events = %d, want 1: %v", count(names, "error"), names)
	}
}

// TestStreamNonStreamBodyToStreamRequest covers finding 3: a 200 answer that is
// not a stream (JSON or HTML) gives one error event and no message_start.
func TestStreamNonStreamBodyToStreamRequest(t *testing.T) {
	for name, body := range map[string]string{
		"json": `{"id":"x","choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`,
		"html": `<html>gateway</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, body)
			})
			s := newServer(t, st.URL)
			rec := do(t, s, "/v1/messages", `{"model":"evren-large","stream":true,"messages":[{"role":"user","content":"x"}]}`,
				map[string]string{"Authorization": "Bearer " + testToken})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			names := sseNames(t, rec.Body.String())
			if len(names) != 1 || names[0] != "error" {
				t.Fatalf("event names = %v, want one error event: %s", names, rec.Body.String())
			}
			kind, msg := errorOf(t, []byte(sseData(t, rec.Body.String())))
			if kind != "api_error" || msg != "the provider did not answer with a stream" {
				t.Fatalf("error = %q %q", kind, msg)
			}
		})
	}
}

// TestNonStreamAnswerTooLarge covers finding 8: an answer larger than 64 MB is
// a 502 api_error with a fixed message, not "not JSON".
func TestNonStreamAnswerTooLarge(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"x","choices":[{"message":{"content":"`)
		io.WriteString(w, strings.Repeat("y", maxProviderAnswer))
		io.WriteString(w, `"},"finish_reason":"stop"}]}`)
	})
	s := newServer(t, st.URL)
	rec := do(t, s, "/v1/messages", `{"model":"evren-large","messages":[{"role":"user","content":"x"}]}`,
		map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	kind, msg := errorOf(t, rec.Body.Bytes())
	if kind != "api_error" || msg != "the provider's answer is too large" {
		t.Fatalf("error = %q %q", kind, msg)
	}
}

// TestServerTimeouts covers finding 7: the server has a read-header and idle
// timeout and no write timeout.
func TestServerTimeouts(t *testing.T) {
	s, _ := New(Config{BaseURL: "http://127.0.0.1:1", Key: testKey, Token: testToken, Models: []string{"m"}, Default: "m"})
	if s.httpSrv.ReadHeaderTimeout != readHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", s.httpSrv.ReadHeaderTimeout, readHeaderTimeout)
	}
	if s.httpSrv.IdleTimeout != idleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", s.httpSrv.IdleTimeout, idleTimeout)
	}
	if s.httpSrv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (streams are long)", s.httpSrv.WriteTimeout)
	}
}

// TestCountTokens covers the token-counting route.
func TestCountTokens(t *testing.T) {
	s := newServer(t, "http://127.0.0.1:0")
	rec := do(t, s, "/v1/messages/count_tokens", `{"system":"abc","messages":[]}`,
		map[string]string{"x-api-key": testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.InputTokens != 1 {
		t.Fatalf("input_tokens = %d, want 1", got.InputTokens)
	}
}

// TestEndToEndToolTurn sends an Anthropic request with a tool definition and a
// tool result through the server to the stub and checks both the provider's
// request body and the events that come back.
func TestEndToEndToolTurn(t *testing.T) {
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\\\"Paris\\\"}\"}}]}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	s := newServer(t, st.URL)
	reqBody := `{
		"model":"claude-opus-4","stream":true,
		"tools":[{"name":"get_weather","description":"look it up","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
		"messages":[
			{"role":"user","content":"what is the weather"},
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Paris"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"sunny"}]}
		]
	}`
	rec := do(t, s, "/v1/messages", reqBody, map[string]string{"Authorization": "Bearer " + testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := st.last(t)
	tools, _ := got.body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("provider tools = %v, want one", got.body["tools"])
	}
	msgs, _ := got.body["messages"].([]any)
	// user, assistant with tool_calls, tool result
	if len(msgs) != 3 {
		t.Fatalf("provider messages = %d, want 3: %v", len(msgs), got.body["messages"])
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "sunny" {
		t.Fatalf("tool message = %v", toolMsg)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"name":"get_weather"`) {
		t.Fatalf("no tool_use block in %q", body)
	}
	if !strings.Contains(body, `"partial_json":"{\"city\":\"Paris\"}"`) {
		t.Fatalf("the tool arguments did not stream as input_json_delta: %q", body)
	}
	if !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("stop_reason is not tool_use: %q", body)
	}
	names := sseNames(t, body)
	if len(names) == 0 || names[len(names)-1] != "message_stop" {
		t.Fatalf("the stream did not end with message_stop: %v", names)
	}
}

// TestNothingWrittenToDisk checks that the translator writes no file: the
// working directory stays as it was.
func TestNothingWrittenToDisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	st := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c1","choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`)
	})
	s := newServer(t, st.URL)
	do(t, s, "/v1/messages", `{"model":"evren-large","messages":[]}`, map[string]string{"Authorization": "Bearer " + testToken})
	entries, err := readDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the translator wrote files: %v", entries)
	}
}

// --- helpers ---------------------------------------------------------------

// errorKindOf returns the error.type of an Anthropic error body.
func errorKindOf(t *testing.T, b []byte) string {
	t.Helper()
	kind, _ := errorOf(t, b)
	return kind
}

// errorOf returns the error.type and error.message of an Anthropic error body.
func errorOf(t *testing.T, b []byte) (string, string) {
	t.Helper()
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode error body %s: %v", b, err)
	}
	return body.Error.Type, body.Error.Message
}

// sseNames returns the event names in an SSE body, in order.
func sseNames(t *testing.T, body string) []string {
	t.Helper()
	var names []string
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			names = append(names, name)
		}
	}
	return names
}

// sseData returns the data payload of the first event in an SSE body.
func sseData(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			return data
		}
	}
	t.Fatalf("no data line in %q", body)
	return ""
}

// contains reports whether a list holds a value.
func contains(list []string, want string) bool { return count(list, want) > 0 }

// count reports how often a value appears in a list.
func count(list []string, want string) int {
	n := 0
	for _, v := range list {
		if v == want {
			n++
		}
	}
	return n
}

// waitFor waits until the body written so far contains want, or fails the
// test after a deadline.
func waitFor(t *testing.T, body *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(body.String(), want) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("did not see %q within the deadline, body is %q", want, body.String())
}

// syncBuffer is an http.ResponseWriter whose body can be read while a handler
// goroutine writes it, which httptest.ResponseRecorder alone does not allow.
type syncBuffer struct {
	mu     sync.Mutex
	header http.Header
	status int
	buf    bytes.Buffer
}

// Header returns the response headers.
func (b *syncBuffer) Header() http.Header {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.header == nil {
		b.header = http.Header{}
	}
	return b.header
}

// WriteHeader records the status.
func (b *syncBuffer) WriteHeader(status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status == 0 {
		b.status = status
	}
}

// Write appends to the body.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Flush does nothing; the buffer needs no flushing.
func (b *syncBuffer) Flush() {}

// String returns what has been written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// readDir lists a directory's entries.
func readDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
