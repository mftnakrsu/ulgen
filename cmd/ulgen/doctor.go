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

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/evren"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// The three states of a check: fine, a problem that keeps Ülgen from doing
// its work, and something worth knowing that is not a problem.
const (
	checkOK      = "ok"
	checkProblem = "problem"
	checkNote    = "note"
)

// check is one line of "ulgen doctor".
type check struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message"`
	Remedy  string `json:"remedy,omitempty"` // what to run or do, when there is something to do
}

// runDoctor answers "why does it not work": "ulgen doctor [options]". It
// looks at the key, at EVREN and at every tool, says one line about each and
// what to do about the ones that are wrong. It changes nothing and never
// prints the key. The exit code is 1 when a check found a problem.
func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	fs := flag.NewFlagSet("ulgen doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, tr.T("flag.status_json"))
	baseURL := fs.String("base-url", "", tr.T("flag.evren_base_url"))
	if code, done := parseSub(fs, "doctor", args, stdout, stderr, tr); done {
		return code
	}
	if err := checkBaseURL(*baseURL); err != nil {
		return usageError(fs, "doctor", tr.T("error.base_url_unsafe"), stderr, tr)
	}

	checks := doctorEvren(ctx, *baseURL, getenv, tr)
	if ctx.Err() == nil {
		checks = append(checks, doctorTools(ctx, getenv, tr)...)
	}
	if ctx.Err() != nil {
		return cancelled(tr).report(stderr, "")
	}

	healthy := true
	for _, c := range checks {
		if c.State == checkProblem {
			healthy = false
		}
	}
	code := exitOK
	if !healthy {
		code = exitError
	}
	if *asJSON {
		b, err := json.MarshalIndent(struct {
			OK     bool    `json:"ok"`
			Checks []check `json:"checks"`
		}{healthy, checks}, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "ulgen: "+err.Error())
			return exitError
		}
		fmt.Fprintln(stdout, string(b))
		return code
	}
	marks := map[string]string{checkOK: "ok", checkProblem: "!!", checkNote: "--"}
	for _, c := range checks {
		fmt.Fprintf(stdout, "%s  %s\n", marks[c.State], c.Message)
		if c.Remedy != "" {
			fmt.Fprintf(stdout, "    %s\n", c.Remedy)
		}
	}
	fmt.Fprintln(stdout)
	if healthy {
		fmt.Fprintln(stdout, tr.T("doctor.healthy"))
	} else {
		fmt.Fprintln(stdout, tr.T("doctor.unhealthy"))
	}
	return code
}

// doctorEvren checks the key and, with a key, what EVREN says about it: one
// request for the quota, which costs no tokens.
func doctorEvren(ctx context.Context, baseURL string, getenv func(string) string, tr *i18n.Translator) []check {
	store, err := openKeyStore(getenv)
	if err != nil {
		return []check{{ID: "key", State: checkProblem, Message: describeStoreError(err, tr)}}
	}
	cred, err := credential.Resolve(store, evrenAccount, provider.EVREN.KeyEnv, getenv)
	if errors.Is(err, credential.ErrNotFound) {
		return []check{{ID: "key", State: checkProblem, Message: tr.T("doctor.not_signed_in"), Remedy: tr.T("doctor.run", "ulgen login")}}
	}
	if err != nil {
		return []check{{ID: "key", State: checkProblem, Message: describeStoreError(err, tr)}}
	}
	checks := []check{{ID: "key", State: checkOK, Message: tr.T("doctor.signed_in", sourcePhrase(tr, cred.Source))}}

	q, err := evren.New(cred.Secret, baseURL, userAgent()).Quota(ctx)
	if err != nil {
		c := check{ID: "evren", State: checkProblem, Message: describeEvrenError(err, tr, tr.T("status.key_invalid"), tr.T("status.terms_missing"))}
		var apiErr *provider.APIError
		switch {
		case errors.As(err, &apiErr) && (apiErr.IsUnauthorized() || apiErr.IsTermsNotAccepted()):
			c.Remedy = tr.T("doctor.run", "ulgen login")
		case !errors.As(err, &apiErr):
			c.Remedy = tr.T("doctor.check_network", evrenHost(baseURL))
		}
		return append(checks, c)
	}
	checks = append(checks, check{ID: "evren", State: checkOK, Message: tr.T("doctor.evren_ok")})

	sep := tr.T("format.thousands")
	reset := q.DailyReset.In(localZone())
	if q.DailyRemaining <= 0 {
		return append(checks, check{ID: "quota", State: checkProblem,
			Message: tr.T("doctor.quota_empty", reset.Format("15:04"), formatDuration(tr, reset.Sub(now())))})
	}
	return append(checks, check{ID: "quota", State: checkOK,
		Message: tr.T("doctor.quota", groupDigits(q.DailyRemaining, sep), groupDigits(q.DailyLimit, sep), reset.Format("15:04"))})
}

// evrenHost is the host a request goes to, for the message about the network.
func evrenHost(baseURL string) string {
	if host := baseURLNotice(baseURL); host != "" {
		return host
	}
	host := strings.TrimPrefix(provider.EVREN.BaseURL, "https://")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return host
}

// doctorTools checks every supported tool: installed or not, and whether
// EVREN is set up in it. A tool that is not installed, or installed and not
// set up, is a note; a tool whose file setup would refuse is a problem, and
// so is a machine with no tool at all.
func doctorTools(ctx context.Context, getenv func(string) string, tr *i18n.Translator) []check {
	env, err := checkedEnv(getenv)
	if err != nil {
		return []check{{ID: "tools", State: checkProblem, Message: describeSetupError(err, env.Home, tr)}}
	}
	var checks []check
	installed := 0
	for _, tool := range setup.Tools() {
		if ctx.Err() != nil {
			return checks
		}
		// The version probe runs the tool; a tool that hangs must not hang the doctor.
		probe, stop := context.WithTimeout(ctx, 10*time.Second)
		found := tool.Detect(probe, env)
		stop()
		c := check{ID: tool.ID()}
		if !found.Installed {
			c.State, c.Message = checkNote, tr.T("doctor.tool", tool.Name(), tr.T("doctor.not_installed"))
			checks = append(checks, c)
			continue
		}
		installed++
		name := tool.Name()
		if v := oneLine(found.Version); v != "" {
			name += " " + v
		}
		proxied := false
		if p, ok := tool.(setup.Proxied); ok {
			proxied = p.Proxied()
		}
		configured, err := tool.Configured(env)
		switch {
		case proxied:
			c.State, c.Message = checkOK, tr.T("doctor.tool", name, tr.T("doctor.ready", "ulgen "+tool.ID()))
		case err != nil:
			c.State, c.Message = checkProblem, tr.T("doctor.tool", name, tr.T(listState(configured, err)))
			c.Remedy = tr.T("doctor.run_for_details", "ulgen setup "+tool.ID())
		case !configured:
			c.State, c.Message = checkNote, tr.T("doctor.tool", name, tr.T("setup.list.not_set_up"))
			c.Remedy = tr.T("doctor.run", "ulgen setup "+tool.ID())
		default:
			c.State, c.Message = checkOK, tr.T("doctor.tool", name, tr.T("setup.list.set_up"))
		}
		checks = append(checks, c)
	}
	if installed == 0 {
		var hints []string
		for _, tool := range setup.Tools() {
			hints = append(hints, fmt.Sprintf("%-12s %s", tool.Name(), strings.TrimSpace(renderNote(tr, env, tool.InstallHint(env)))))
		}
		checks = append(checks, check{ID: "tools", State: checkProblem, Message: tr.T("doctor.none_installed"), Remedy: strings.Join(hints, "\n    ")})
	}
	return checks
}
