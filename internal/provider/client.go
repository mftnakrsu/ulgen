// Package provider is the HTTP client for EVREN's API: one JSON request with
// the key in a header, bounded retries, no redirects, and errors from which
// the key has been removed.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	maxErrorBody    = 64 << 10
	maxResponseBody = 64 << 20

	// DefaultHeaderTimeout bounds the wait for the response headers.
	DefaultHeaderTimeout = 120 * time.Second
	// DefaultIdleTimeout is the time allowed for the body after the headers.
	DefaultIdleTimeout = 60 * time.Second
	// DefaultMaxRetries is how many times a request is repeated after a
	// 429, 502, 503 or 504 answer or a connection failure.
	DefaultMaxRetries = 2
	// maxRetryWait caps the pause between attempts; a longer Retry-After
	// means the quota is gone for a while and the error is returned instead.
	maxRetryWait = 30 * time.Second
)

// Client calls one provider.
type Client struct {
	Preset Preset
	// BaseURL overrides the preset's base URL when set.
	BaseURL string
	// APIKey is sent as a Bearer token. Empty means no authentication
	// header, for proxies that attach the key themselves.
	APIKey string
	// HTTP is the client used for requests. When nil, one is built on the
	// first call from the timeouts below; it never follows redirects, so a
	// key or a prompt is never resent to another host.
	HTTP          *http.Client
	UserAgent     string
	HeaderTimeout time.Duration
	IdleTimeout   time.Duration
	// MaxRetries is the number of repeats after a retryable failure; a
	// negative value disables retries.
	MaxRetries int
	// RetryBase is the first pause between attempts; later pauses grow
	// from it. Zero means one second.
	RetryBase time.Duration
}

func (c *Client) backoff(attempt int) time.Duration {
	base := c.RetryBase
	if base <= 0 {
		base = time.Second
	}
	return base * time.Duration(1+2*attempt)
}

// NewClient returns a client for the preset with the default timeouts.
func NewClient(preset Preset, apiKey string) *Client {
	return &Client{
		Preset:        preset,
		APIKey:        apiKey,
		UserAgent:     "ulgen",
		HeaderTimeout: DefaultHeaderTimeout,
		IdleTimeout:   DefaultIdleTimeout,
		MaxRetries:    DefaultMaxRetries,
	}
}

func (c *Client) baseURL() string {
	u := c.BaseURL
	if u == "" {
		u = c.Preset.BaseURL
	}
	return strings.TrimRight(u, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = c.headerTimeout()
	transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 15 * time.Second
	c.HTTP = &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return c.HTTP
}

func (c *Client) headerTimeout() time.Duration {
	if c.HeaderTimeout > 0 {
		return c.HeaderTimeout
	}
	return DefaultHeaderTimeout
}

func (c *Client) idleTimeout() time.Duration {
	if c.IdleTimeout > 0 {
		return c.IdleTimeout
	}
	return DefaultIdleTimeout
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	if c.baseURL() == "" {
		return nil, fmt.Errorf("provider %s: no base URL", c.Preset.ID)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

// do sends a request, repeating it after retryable failures. The caller
// owns the response body.
func (c *Client) do(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	attempts := 1 + c.MaxRetries
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("provider: %w", err)
			if attempt+1 < attempts && c.pause(ctx, c.backoff(attempt)) {
				continue
			}
			return nil, lastErr
		}
		if !retryable(resp.StatusCode) || attempt+1 >= attempts {
			return resp, nil
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
		apiErr := newAPIErrorKey(resp.StatusCode, b, resp.Header.Get("Retry-After"), c.APIKey)
		wait := apiErr.RetryAfter
		if wait == 0 {
			wait = c.backoff(attempt)
		}
		if wait > maxRetryWait || !c.pause(ctx, wait) {
			return nil, apiErr
		}
		lastErr = apiErr
	}
	return nil, lastErr
}

func retryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// pause waits for d unless the context ends first.
func (c *Client) pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// JSON sends one request whose body and answer are JSON. body is encoded
// when non-nil; a 2xx answer is decoded into out when out is non-nil. Any
// other status is returned as an *APIError. Retries follow do. The answer
// headers are returned even with an error, so a caller can read quota
// headers from a refusal.
func (c *Client) JSON(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("provider: encoding request: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.headerTimeout()+c.idleTimeout())
	defer cancel()
	resp, err := c.do(ctx, func() (*http.Request, error) {
		req, err := c.newRequest(ctx, method, path, payload)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return resp.Header, contextOr(ctx, fmt.Errorf("provider: reading answer: %w", err))
	}
	if resp.StatusCode/100 != 2 {
		return resp.Header, newAPIErrorKey(resp.StatusCode, b, resp.Header.Get("Retry-After"), c.APIKey)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.Header, fmt.Errorf("provider: bad answer from %s: %w", path, err)
		}
	}
	return resp.Header, nil
}

// contextOr returns the context's error when the context ended, so a
// cancellation is reported as such rather than as a broken connection.
func contextOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
