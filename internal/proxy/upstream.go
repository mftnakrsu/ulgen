package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// upstreamClient sends chat-completions requests to EVREN. It carries the key
// and puts it only on requests to the configured base URL.
type upstreamClient struct {
	baseURL string
	key     string
	http    *http.Client
}

// newUpstream returns a client for the EVREN API root baseURL with key. A nil
// http client becomes one without an overall timeout, as the design asks: a
// stream may run for as long as the model needs.
func newUpstream(baseURL, key string, hc *http.Client) *upstreamClient {
	if hc == nil {
		hc = &http.Client{}
	}
	return &upstreamClient{baseURL: strings.TrimRight(baseURL, "/"), key: key, http: hc}
}

// chat sends a chat-completions request. The body is already encoded; only the
// Authorization and Content-Type headers are set, and no header of the client
// is passed on. The caller owns the response.
func (c *upstreamClient) chat(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

// upstreamError is a failed answer from EVREN, before any byte was sent to the
// client. Status is the provider's status; Kind is the Anthropic error type
// the design maps it to, and Message is EVREN's own message with the key
// replaced.
type upstreamError struct {
	Status  int
	Kind    string
	Message string
}

// Error reports the status and the message; the key is already replaced.
func (e *upstreamError) Error() string {
	return fmt.Sprintf("provider: HTTP %d: %s", e.Status, e.Message)
}

// fixedAuthMessages are the texts Claude Code shows for a refused key. They
// are fixed English strings inside the protocol answer, not user-interface
// messages of Ülgen, so they are not translated. Claude Code answers a 401
// with advice to sign in to Anthropic, which is wrong here.
var fixedAuthMessages = map[int]string{
	http.StatusUnauthorized: "EVREN did not accept the key; leave Claude Code and run: ulgen login",
	http.StatusForbidden:    "EVREN refused the request (terms not accepted or access removed); leave Claude Code and run: ulgen login",
}

// providerError builds an upstreamError from a non-2xx answer. A refused key
// (401) or a refused request (403) becomes 400 invalid_request_error with a
// fixed message, so the client does not tell the user to sign in to Anthropic.
// Otherwise the key is replaced in EVREN's own message should the provider
// have echoed it; nothing else of the body is kept.
func providerError(status int, body []byte, key string) *upstreamError {
	if msg, ok := fixedAuthMessages[status]; ok {
		return &upstreamError{Status: http.StatusBadRequest, Kind: "invalid_request_error", Message: msg}
	}
	msg := providerMessage(body)
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &upstreamError{Status: status, Kind: errorKind(status), Message: redactText(msg, key)}
}

// providerMessage reads the message out of an EVREN error body. Known shapes
// are OpenAI's {"error":{"message":...}} and EVREN's flat {"message":...};
// anything else contributes nothing.
func providerMessage(body []byte) string {
	var top map[string]any
	if json.Unmarshal(body, &top) != nil || top == nil {
		return ""
	}
	if inner, ok := top["error"].(map[string]any); ok {
		if s := stringField(inner, "message", "detail", "msg"); s != "" {
			return s
		}
	}
	return stringField(top, "message", "detail", "msg")
}

// stringField returns the first of the named fields that is a string or a
// number, formatted as text.
func stringField(m map[string]any, names ...string) string {
	for _, n := range names {
		switch v := m[n].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			if enc.Encode(v) == nil {
				return strings.TrimSpace(buf.String())
			}
		}
	}
	return ""
}

// readData reads a streaming answer: server-sent events whose data payload is
// one OpenAI chunk in JSON. Each decoded chunk is passed to emit. A payload
// that is EVREN's error shape is returned as an *upstreamError, as is an
// unreadable chunk. The stream ends at [DONE] or at the end of the body.
func readData(ctx context.Context, r io.Reader, emit func(streamChunk) error, key string) error {
	s := newSSEReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		payload, err := s.next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		data := strings.TrimSpace(payload)
		if data == "" {
			continue
		}
		// A provider may report an error inside a 200 stream as a chunk.
		if err := errChunk([]byte(data), key); err != nil {
			return err
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return errors.New("proxy: bad chunk from provider")
		}
		if err := emit(ch); err != nil {
			return err
		}
	}
}

// errChunk reports an upstreamError when a chunk is EVREN's error shape:
// {"error":{...}} or {"error":"..."} instead of a completion. An error member
// that is null, an empty string or an empty object is not an error.
func errChunk(data []byte, key string) error {
	var probe struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(data, &probe) != nil || !realErrorValue(probe.Error) {
		return nil
	}
	msg := providerMessage(data)
	if msg == "" {
		msg = "provider error"
	}
	return &upstreamError{Status: http.StatusBadGateway, Kind: "api_error", Message: redactText(msg, key)}
}

// realErrorValue reports whether a raw JSON error member carries an error: an
// object with at least one member or a non-empty string. null, "", {} and an
// absent member (a nil raw message) do not.
func realErrorValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '"':
		var s string
		if json.Unmarshal(trimmed, &s) != nil {
			return false
		}
		return s != ""
	case '{':
		var obj map[string]json.RawMessage
		if json.Unmarshal(trimmed, &obj) != nil {
			return false
		}
		return len(obj) > 0
	default:
		return false
	}
}

// sseReader reads server-sent events from a stream. It yields the data payload
// of each event, joining several data lines with newlines. Line endings may be
// \n or \r\n, lines may be any length, and comment lines are ignored.
type sseReader struct {
	r    *bufio.Reader
	data []string
	have bool
}

// newSSEReader returns a reader of server-sent events over r.
func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReader(r)}
}

// next returns the payload of the next event. A blank line dispatches the
// event; a payload of [DONE] and the end of the stream both return io.EOF.
func (s *sseReader) next() (string, error) {
	for {
		raw, err := s.r.ReadString('\n')
		line := strings.TrimRight(raw, "\r\n")
		if line != "" {
			s.consume(line)
		} else if s.have {
			return s.dispatch()
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if s.have {
				if payload, derr := s.dispatch(); derr == nil {
					return payload, nil
				}
			}
			return "", io.EOF
		}
		return "", err
	}
}

// dispatch returns the collected data of one event and clears it. It returns
// io.EOF for the [DONE] sentinel.
func (s *sseReader) dispatch() (string, error) {
	payload := strings.Join(s.data, "\n")
	s.data = s.data[:0]
	s.have = false
	if strings.TrimSpace(payload) == "[DONE]" {
		return "", io.EOF
	}
	return payload, nil
}

// consume takes one data line into the current event and ignores every other
// line.
func (s *sseReader) consume(line string) {
	if strings.HasPrefix(line, ":") {
		return
	}
	field, value, found := strings.Cut(line, ":")
	if field != "data" {
		return
	}
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	s.data = append(s.data, value)
	s.have = true
}
