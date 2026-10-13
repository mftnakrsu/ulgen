package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
)

// statusReport is what status fetches: the quota and the chat models.
type statusReport struct {
	Quota  evren.Quota
	Models []evren.Model
}

// runStatus shows the remaining quota and the models: "ulgen status [options]".
func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, tr.T("flag.status_json"))
	baseURL := fs.String("base-url", "", tr.T("flag.evren_base_url"))
	if code, done := parseSub(fs, "status", args, stdout, stderr, tr); done {
		return code
	}
	if err := checkBaseURL(*baseURL); err != nil {
		return usageError(fs, "status", tr.T("error.base_url_unsafe"), stderr, tr)
	}
	if notice := baseURLNotice(*baseURL); notice != "" {
		fmt.Fprintln(stderr, tr.T("notice.base_url", notice))
	}

	cred, code := resolveKey(stderr, getenv, tr)
	if code != exitOK {
		return code
	}
	client := evren.New(cred.Secret, *baseURL, userAgent())
	rep, err := fetchReport(ctx, client)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeEvrenError(err, tr, tr.T("status.key_invalid"), tr.T("status.terms_missing")))
		if errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		return exitError
	}
	if *asJSON {
		if err := writeStatusJSON(stdout, rep, cred.Source); err != nil {
			fmt.Fprintln(stderr, "ulgen: "+err.Error())
			return exitError
		}
		return exitOK
	}
	writeStatusText(stdout, rep, cred.Source, tr)
	return exitOK
}

// fetchReport reads the quota and the chat models.
func fetchReport(ctx context.Context, c *evren.Client) (statusReport, error) {
	var rep statusReport
	q, err := c.Quota(ctx)
	if err != nil {
		return rep, err
	}
	rep.Quota = q
	models, err := c.Models(ctx)
	if err != nil {
		return rep, err
	}
	for _, m := range models {
		if evren.IsChat(m) {
			rep.Models = append(rep.Models, m)
		}
	}
	return rep, nil
}

// writeStatusText prints the report for people.
func writeStatusText(w io.Writer, rep statusReport, source string, tr *i18n.Translator) {
	sep := tr.T("format.thousands")
	q := rep.Quota
	fmt.Fprintln(w, tr.T("status.signed_in", sourcePhrase(tr, source)))
	percent := 0
	if q.DailyLimit > 0 {
		percent = int(q.DailyRemaining * 100 / q.DailyLimit)
	}
	reset := q.DailyReset.In(localZone())
	current := now().In(localZone())
	resetText := reset.Format("15:04")
	if reset.Sub(current) >= 24*time.Hour {
		resetText = reset.Format("2006-01-02 15:04")
	}
	fmt.Fprintln(w, tr.T("status.daily", groupDigits(q.DailyRemaining, sep), groupDigits(q.DailyLimit, sep), percent, resetText, formatDuration(tr, reset.Sub(current))))
	fmt.Fprintln(w, tr.T("status.credits", oneLine(string(q.CreditsRemaining)), oneLine(string(q.CreditsHeld))))
	fmt.Fprintln(w, tr.T("status.models"))
	ids := make([]string, len(rep.Models))
	width := 0
	for i, m := range rep.Models {
		ids[i] = oneLine(m.ID)
		if n := utf8.RuneCountInString(ids[i]); n > width {
			width = n
		}
	}
	for i, m := range rep.Models {
		out := "-"
		if m.MaxOutput > 0 {
			out = groupDigits(int64(m.MaxOutput), sep)
		}
		var caps []string
		if m.Tools {
			caps = append(caps, tr.T("status.cap.tools"))
		}
		if m.Thinking {
			caps = append(caps, tr.T("status.cap.thinking"))
		}
		if m.Vision {
			caps = append(caps, tr.T("status.cap.vision"))
		}
		fmt.Fprintln(w, tr.T("status.model_line", fmt.Sprintf("%-*s", width, ids[i]), fmt.Sprintf("%9s", groupDigits(int64(m.ContextLength), sep)), fmt.Sprintf("%6s", out), strings.Join(caps, " ")))
	}
}

// statusJSON is the top-level shape of "ulgen status --json".
type statusJSON struct {
	KeySource string          `json:"key_source"`
	Quota     quotaJSON       `json:"quota"`
	Models    []modelJSONLine `json:"models"`
}

// quotaJSON is the quota part of the JSON report.
type quotaJSON struct {
	DailyLimitTokens     int64  `json:"daily_limit_tokens"`
	DailyRemainingTokens int64  `json:"daily_remaining_tokens"`
	DailyUsedTokens      int64  `json:"daily_used_tokens"`
	DailyResetAt         string `json:"daily_reset_at"`
	WindowUsedTokens     int64  `json:"window_used_tokens"`
	WindowCap            int64  `json:"window_cap"`
	WindowMinutes        int    `json:"window_minutes"`
	WindowResetAt        string `json:"window_reset_at"`
	Level                string `json:"level"`
	CreditsRemaining     string `json:"credits_remaining"`
	CreditsHeld          string `json:"credits_held"`
}

// modelJSONLine is one chat model in the JSON report. MaxOutputTokens is a
// pointer so a model with no stated output limit is null, not zero.
type modelJSONLine struct {
	ID              string `json:"id"`
	Task            string `json:"task"`
	ContextLength   int    `json:"context_length"`
	MaxOutputTokens *int   `json:"max_output_tokens"`
	Tools           bool   `json:"tools"`
	Thinking        bool   `json:"thinking"`
	Vision          bool   `json:"vision"`
	FreeUntil       string `json:"free_until"`
}

// writeStatusJSON prints the report as one JSON object with stable field names.
func writeStatusJSON(w io.Writer, rep statusReport, source string) error {
	q := rep.Quota
	out := statusJSON{
		KeySource: source,
		Quota: quotaJSON{
			DailyLimitTokens:     q.DailyLimit,
			DailyRemainingTokens: q.DailyRemaining,
			DailyUsedTokens:      q.DailyUsed,
			DailyResetAt:         q.DailyReset.UTC().Format(time.RFC3339),
			WindowUsedTokens:     q.WindowUsed,
			WindowCap:            q.WindowCap,
			WindowMinutes:        q.WindowMinutes,
			WindowResetAt:        q.WindowReset.UTC().Format(time.RFC3339),
			Level:                q.Level,
			CreditsRemaining:     string(q.CreditsRemaining),
			CreditsHeld:          string(q.CreditsHeld),
		},
		Models: []modelJSONLine{},
	}
	for _, m := range rep.Models {
		line := modelJSONLine{ID: m.ID, Task: m.Task, ContextLength: m.ContextLength, Tools: m.Tools, Thinking: m.Thinking, Vision: m.Vision, FreeUntil: m.FreeUntil}
		if m.MaxOutput > 0 {
			v := m.MaxOutput
			line.MaxOutputTokens = &v
		}
		out.Models = append(out.Models, line)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
