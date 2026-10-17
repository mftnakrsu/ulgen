// Package evren talks to the parts of EVREN's API that are not part of the
// OpenAI-compatible surface: terms of use, quota and model details. The
// shapes here were recorded from the live API on 2026-10-11 (docs/evren.md).
package evren

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/mftnakrsu/ulgen/internal/provider"
)

// KeysURL is the page where a signed-in user creates API keys.
const KeysURL = "https://evren.ssyz.org.tr/api-keys"

// KeyPrefix starts every EVREN API key.
const KeyPrefix = "evren_llm_"

// Client calls EVREN through the provider client.
type Client struct {
	*provider.Client
}

// New returns a client for EVREN with the given key. An empty baseURL means
// the preset's; an empty userAgent keeps the provider default.
func New(apiKey, baseURL, userAgent string) *Client {
	c := provider.NewClient(provider.EVREN, apiKey)
	c.BaseURL = baseURL
	if userAgent != "" {
		c.UserAgent = userAgent
	}
	return &Client{c}
}

// TermsStatus says which terms version is current and whether the account
// accepted it.
type TermsStatus struct {
	CurrentVersion int  `json:"current_version"`
	IsMaterial     bool `json:"is_material"`
	Accepted       bool `json:"accepted"`
}

// Terms is the text of the current terms, as Markdown.
type Terms struct {
	Version int    `json:"version"`
	Content string `json:"content"`
}

// Acceptance is the server's record of an accepted version.
type Acceptance struct {
	Version    int       `json:"accepted_version"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// Text is a JSON string or number kept as text, because EVREN sends credit
// amounts both ways.
type Text string

// UnmarshalJSON accepts a string, a number or null.
func (t *Text) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*t = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Text(s)
		return nil
	}
	*t = Text(b)
	return nil
}

// Quota is the account's daily token budget and the short rate window.
type Quota struct {
	DailyLimit       int64     `json:"daily_limit_tokens"`
	DailyRemaining   int64     `json:"daily_remaining_tokens"`
	DailyUsed        int64     `json:"daily_used_tokens"`
	DailyReset       time.Time `json:"daily_reset_at"`
	WindowUsed       int64     `json:"used_tokens"`
	WindowCap        int64     `json:"cap"`
	WindowMinutes    int       `json:"window_minutes"`
	WindowReset      time.Time `json:"reset_at"`
	Level            string    `json:"level"`
	CreditsRemaining Text      `json:"remaining_cr"`
	CreditsHeld      Text      `json:"held_cr"`
}

// Model describes one served model. MaxOutput is 0 when EVREN gives none.
// Effort says that the model takes a reasoning_effort; EffortValues are the
// values EVREN lists for it, nil when it lists none.
type Model struct {
	ID            string
	Task          string
	ContextLength int
	MaxOutput     int
	Tools         bool
	Thinking      bool
	Vision        bool
	Effort        bool
	EffortValues  []string
	FreeUntil     string
}

// IsChat reports a model that answers chat completions. The API writes the
// vision task as "vision_chat". The form with a space, which this project's
// first notes used, is accepted too.
func IsChat(m Model) bool {
	return m.Task == "chat" || m.Task == "vision_chat" || m.Task == "vision chat"
}

type modelJSON struct {
	ID            string `json:"id"`
	Task          string `json:"task"`
	ContextLength int    `json:"context_length"`
	MaxOutput     *int   `json:"default_max_output_tokens"`
	Capabilities  struct {
		Tools        bool     `json:"tools"`
		Thinking     bool     `json:"thinking"`
		Vision       bool     `json:"vision"`
		Effort       bool     `json:"reasoning_effort"`
		EffortValues []string `json:"reasoning_effort_values"`
	} `json:"capabilities"`
	Pricing struct {
		FreeUntil string `json:"free_until"`
	} `json:"pricing"`
}

// TermsStatus reads GET /terms/status.
func (c *Client) TermsStatus(ctx context.Context) (TermsStatus, error) {
	var out TermsStatus
	_, err := c.JSON(ctx, http.MethodGet, "/terms/status", nil, &out)
	return out, err
}

// TermsText reads GET /terms/text.
func (c *Client) TermsText(ctx context.Context) (Terms, error) {
	var out Terms
	_, err := c.JSON(ctx, http.MethodGet, "/terms/text", nil, &out)
	return out, err
}

// AcceptTerms records acceptance of version through POST /terms/accept.
func (c *Client) AcceptTerms(ctx context.Context, version int) (Acceptance, error) {
	var out Acceptance
	_, err := c.JSON(ctx, http.MethodPost, "/terms/accept", map[string]int{"version": version}, &out)
	return out, err
}

// Quota reads GET /quota. It costs no tokens.
func (c *Client) Quota(ctx context.Context) (Quota, error) {
	var out Quota
	_, err := c.JSON(ctx, http.MethodGet, "/quota", nil, &out)
	return out, err
}

// Models reads GET /models with each model's details, in the API's order.
// It hides the embedded client's id-only Models.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var list struct {
		Data []modelJSON `json:"data"`
	}
	if _, err := c.JSON(ctx, http.MethodGet, "/models", nil, &list); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		out := Model{
			ID:            m.ID,
			Task:          m.Task,
			ContextLength: m.ContextLength,
			Tools:         m.Capabilities.Tools,
			Thinking:      m.Capabilities.Thinking,
			Vision:        m.Capabilities.Vision,
			Effort:        m.Capabilities.Effort,
			EffortValues:  m.Capabilities.EffortValues,
			FreeUntil:     m.Pricing.FreeUntil,
		}
		if m.MaxOutput != nil {
			out.MaxOutput = *m.MaxOutput
		}
		models = append(models, out)
	}
	return models, nil
}
