package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// APIError is an answer from the provider that is not a success.
type APIError struct {
	Status int
	// Code is the error code from the body when there is one, for example
	// "terms_not_accepted" or "invalid_api_key".
	Code    string
	Message string
	// TermsVersion is the terms version the provider wants accepted, when
	// Code is "terms_not_accepted".
	TermsVersion string
	// RetryAfter is the server's Retry-After hint, when given.
	RetryAfter time.Duration
	// Body is the start of the raw body, for diagnostics.
	Body string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// IsUnauthorized reports a rejected or missing API key.
func (e *APIError) IsUnauthorized() bool { return e.Status == 401 }

// IsTermsNotAccepted reports EVREN's terms gate.
func (e *APIError) IsTermsNotAccepted() bool { return e.Code == "terms_not_accepted" }

// IsQuotaExceeded reports a rate or quota limit.
func (e *APIError) IsQuotaExceeded() bool { return e.Status == 429 }

// IsBusy reports an overloaded server.
func (e *APIError) IsBusy() bool { return e.Status == 503 }

// newAPIErrorKey builds an APIError from a body that came from the server.
// The key is replaced in the raw body first, so a server or proxy that repeats
// the Authorization header cannot print it: redaction happens before anything
// is cut or parsed, so a key that lies across the cut is still removed. The
// parsed fields are then cleaned once more: a body can carry the key in an
// escaped form (\u0065..., \/) that the raw text does not match and the JSON
// decoder puts back together. An empty or short key leaves the error as is.
func newAPIErrorKey(status int, body []byte, retryAfter, key string) *APIError {
	e := buildAPIError(status, []byte(redact(string(body), key)), retryAfter)
	e.Message = redact(e.Message, key)
	e.Code = redact(e.Code, key)
	e.TermsVersion = redact(e.TermsVersion, key)
	return e
}

// minRedactLen is the shortest string redact will treat as a key. Shorter
// values are placeholders (a test key, a one-letter value), and replacing a
// one-letter key would damage ordinary words.
const minRedactLen = 8

// redact removes the client's key from text that came from the server, so a
// server or proxy that repeats the Authorization header cannot put the key
// on the screen. Keys shorter than minRedactLen are left alone: they are
// placeholders, and replacing a one-letter key would damage ordinary words.
func redact(text, key string) string {
	if len(key) < minRedactLen || !strings.Contains(text, key) {
		return text
	}
	return strings.ReplaceAll(text, key, "[key]")
}

// buildAPIError interprets an error body. Known shapes:
//
//	{"error": {"message": "...", "type": "...", "code": "..."}}             OpenAI
//	{"code": "terms_not_accepted", "message": "...", "required_version": 3}  EVREN
//	{"detail": "..."}                                                        FastAPI servers
//
// Anything else becomes the message as is.
func buildAPIError(status int, body []byte, retryAfter string) *APIError {
	e := &APIError{Status: status, Body: truncate(string(body), 2000)}
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
		e.RetryAfter = time.Duration(secs) * time.Second
	}
	var top map[string]any
	if json.Unmarshal(body, &top) != nil || top == nil {
		e.Message = truncate(strings.TrimSpace(string(body)), 500)
		return e
	}
	fields := top
	switch inner := top["error"].(type) {
	case map[string]any:
		fields = inner
	case string:
		e.Message = inner
	}
	e.Code = stringField(fields, "code")
	if e.Code == "" {
		e.Code = stringField(top, "code")
	}
	if e.Message == "" {
		e.Message = stringField(fields, "message", "detail", "msg")
	}
	if e.Message == "" {
		e.Message = stringField(top, "message", "detail", "msg")
	}
	if e.IsTermsNotAccepted() {
		e.TermsVersion = stringField(fields, "required_version", "terms_version", "version")
		if e.TermsVersion == "" {
			e.TermsVersion = stringField(top, "required_version", "terms_version", "version")
		}
	}
	if e.Message == "" {
		// A shape not known here, such as a "detail" array: show the body.
		e.Message = truncate(strings.TrimSpace(string(body)), 500)
	}
	return e
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
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
