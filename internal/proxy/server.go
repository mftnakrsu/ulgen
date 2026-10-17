package proxy

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// maxBodyBytes refuses request bodies larger than 64 MB, as the design asks.
const maxBodyBytes = 64 << 20

// maxProviderErrorBody is how much of EVREN's error body is read for the
// message; the rest is dropped.
const maxProviderErrorBody = 64 << 10

// maxProviderAnswer is how large a non-streaming answer from EVREN may be. A
// larger one is refused rather than cut off, so a long answer is never
// reported as invalid JSON.
const maxProviderAnswer = 64 << 20

// The http.Server's timeouts. A stream stays open for as long as the model
// needs, so there is no write timeout.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// defaultPingInterval is how long the stream may stay silent before a ping is
// sent; it is a field on the server so a test can shorten it.
const defaultPingInterval = 15 * time.Second

// Config is everything the proxy server needs to run.
type Config struct {
	BaseURL    string       // EVREN API root, without a trailing slash
	Key        string       // EVREN API key; sent to BaseURL only
	Token      string       // what a client must present
	Models     []string     // EVREN model ids a request may name
	Default    string       // used when a request names another model
	Small      string       // used when a request names a model whose name contains "haiku"
	HTTPClient *http.Client // nil: a client without an overall timeout
}

// Server is the local translator: it speaks the Anthropic Messages API to the
// client and the OpenAI chat-completions API to EVREN, holding the EVREN key
// so the client never sees it.
type Server struct {
	cfg      Config
	upstream *upstreamClient
	// pingInterval is how long a stream may stay silent before a ping event.
	// Tests shorten it.
	pingInterval time.Duration
	httpSrv      *http.Server
}

// New builds a server. It refuses an empty key, token, default, or a default
// that is not one of the models.
func New(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.Key) == "" {
		return nil, errors.New("proxy: the EVREN key is empty")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("proxy: the client token is empty")
	}
	if strings.TrimSpace(cfg.Default) == "" {
		return nil, errors.New("proxy: the default model is empty")
	}
	found := false
	for _, m := range cfg.Models {
		if m == cfg.Default {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("proxy: the default model is not in the model list")
	}

	s := &Server{
		cfg:          cfg,
		upstream:     newUpstream(cfg.BaseURL, cfg.Key, cfg.HTTPClient),
		pingInterval: defaultPingInterval,
	}
	s.httpSrv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	return s, nil
}

// Handler returns the HTTP handler for the translator's routes.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

// Serve listens on l until Shutdown is called. It returns nil for the
// shutdown-induced error.
func (s *Server) Serve(l net.Listener) error {
	err := s.httpSrv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the server, waiting for running requests until ctx ends.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// serveHTTP checks the token, enforces the body limit and routes the request.
// Everything that is not one of the two known routes is 404.
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "authentication_error", "missing or invalid token")
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
		s.handleMessages(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/messages/count_tokens":
		s.handleCountTokens(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found_error", "not found")
	}
}

// authorized reports whether the request carries the token in either header.
// The comparison is in constant time.
func (s *Server) authorized(r *http.Request) bool {
	want := []byte(s.cfg.Token)
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		if subtle.ConstantTimeCompare([]byte(auth[7:]), want) == 1 {
			return true
		}
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("x-api-key")), want) == 1
}

// handleMessages translates one Messages request, streaming or not.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var req anthropicRequest
	if !decodeBody(w, r, &req) {
		return
	}

	model := pickModel(req.Model, s.cfg.Models, s.cfg.Default, s.cfg.Small)
	out, names, err := toOpenAI(req, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	body, err := json.Marshal(out)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_error", "could not encode the request")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	resp, err := s.upstream.chat(ctx, body)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		writeError(w, http.StatusBadGateway, "api_error", "could not reach the provider")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxProviderErrorBody))
		pe := providerError(resp.StatusCode, b, s.cfg.Key)
		writeError(w, pe.Status, pe.Kind, pe.Message)
		return
	}

	if req.Stream {
		s.stream(w, ctx, resp, model, names)
		return
	}
	s.complete(w, resp, model, names)
}

// complete answers a non-streaming request with one Anthropic message.
func (s *Server) complete(w http.ResponseWriter, resp *http.Response, model string, names nameMap) {
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderAnswer+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "could not read the provider's answer")
		return
	}
	if len(b) > maxProviderAnswer {
		writeError(w, http.StatusBadGateway, "api_error", "the provider's answer is too large")
		return
	}
	var parsed openAIResponse
	if err := json.Unmarshal(b, &parsed); err != nil {
		writeError(w, http.StatusBadGateway, "api_error", "the provider's answer was not JSON")
		return
	}
	out, err := json.Marshal(fromOpenAI(parsed, model, names))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_error", "could not encode the answer")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// stream answers a streaming request. Headers are written once the provider
// has answered, so a provider failure before the first byte is a normal HTTP
// error; a failure after it is one error event. A ping is sent while nothing
// else is, and the provider request is cancelled when the client goes away.
func (s *Server) stream(w http.ResponseWriter, ctx context.Context, resp *http.Response, model string, names nameMap) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "api_error", "streaming is not supported here")
		return
	}
	// Headers are written only once the first event is ready, so a provider
	// that answers a stream request with a plain body can still get one error
	// event and no message_start. A ping may be sent while waiting.
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	headerWritten := false

	events := make(chan []event)
	go s.produce(ctx, resp, model, names, events)

	timer := time.NewTimer(s.pingInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case evs, ok := <-events:
			if !ok {
				return
			}
			if len(evs) == 0 {
				continue
			}
			if !headerWritten {
				w.WriteHeader(http.StatusOK)
				headerWritten = true
			}
			for _, ev := range evs {
				writeSSE(w, ev)
			}
			flusher.Flush()
			resetTimer(timer, s.pingInterval)
		case <-timer.C:
			if !headerWritten {
				w.WriteHeader(http.StatusOK)
				headerWritten = true
			}
			writeSSE(w, event{Name: "ping", Data: pingEvent{Type: "ping"}})
			flusher.Flush()
			resetTimer(timer, s.pingInterval)
		}
	}
}

// produce reads the provider's stream and feeds it to the translator, sending
// the events it returns on out. An error in the middle becomes one error
// event; a cancellation ends quietly.
func (s *Server) produce(ctx context.Context, resp *http.Response, model string, names nameMap, out chan<- []event) {
	defer close(out)
	send := func(evs []event) bool {
		if len(evs) == 0 {
			return true
		}
		select {
		case out <- evs:
			return true
		case <-ctx.Done():
			return false
		}
	}

	tr := newStreamTranslator(model, names)
	chunks := 0
	err := readData(ctx, resp.Body, func(ch streamChunk) error {
		chunks++
		if !send(tr.Feed(ch)) {
			return ctx.Err()
		}
		if tr.overflow {
			return errStreamEventTooLarge
		}
		return nil
	}, s.cfg.Key)
	// A 200 answer that is not a stream (JSON or HTML) carried no chunk. The
	// client must see an error, not an empty successful message.
	if err == nil && chunks == 0 {
		send([]event{streamError("the provider did not answer with a stream", s.cfg.Key)})
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		message := "the provider's stream failed"
		var ue *upstreamError
		if errors.As(err, &ue) {
			message = ue.Message
		}
		send([]event{streamError(message, s.cfg.Key)})
		return
	}
	send(tr.End())
}

// handleCountTokens answers the token-counting route with the design's
// estimate.
func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	var req anthropicRequest
	if !decodeBody(w, r, &req) {
		return
	}
	body, err := json.Marshal(struct {
		InputTokens int `json:"input_tokens"`
	}{InputTokens: estimateTokens(req)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_error", "could not encode the answer")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// decodeBody reads and decodes a request body. On failure it has already
// written the answer and returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		}
		return false
	}
	if err := json.Unmarshal(b, out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return false
	}
	return true
}

// writeSSE writes one server-sent event and its JSON body.
func writeSSE(w io.Writer, ev event) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, data)
}

// writeError answers with Anthropic's error shape.
func writeError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(errorBody(kind, message))
}

// resetTimer restarts a timer for the ping interval, draining it first so a
// value that fired while other work was happening does not fire again.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
