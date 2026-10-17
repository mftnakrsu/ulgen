package main

import (
	"fmt"
	"strings"
)

// The environment variables Claude Code documents for pointing it at another
// Anthropic-shaped API. Only variables that Claude Code documents are used.
const (
	claudeEnvBaseURL      = "ANTHROPIC_BASE_URL"
	claudeEnvToken        = "ANTHROPIC_AUTH_TOKEN"
	claudeEnvAPIKey       = "ANTHROPIC_API_KEY"
	claudeEnvModel        = "ANTHROPIC_MODEL"
	claudeEnvOpus         = "ANTHROPIC_DEFAULT_OPUS_MODEL"
	claudeEnvSonnet       = "ANTHROPIC_DEFAULT_SONNET_MODEL"
	claudeEnvSubagent     = "CLAUDE_CODE_SUBAGENT_MODEL"
	claudeEnvHaiku        = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	claudeEnvDisableBetas = "CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS"
	claudeEnvUnneeded     = "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"
	claudeEnvMaxOutput    = "CLAUDE_CODE_MAX_OUTPUT_TOKENS"
	// claudeEnvMaxContext tells Claude Code the context window of a model it
	// does not know. Without it Claude Code assumes 200k tokens, compacts the
	// conversation at that size and says so at every start.
	claudeEnvMaxContext = "CLAUDE_CODE_MAX_CONTEXT_TOKENS"
	// claudeEnvAttribution removes the attribution block from Claude Code's
	// system prompt (design, "ulgen run claude: one addition"). It is set for
	// the child of both translated commands.
	claudeEnvAttribution = "CLAUDE_CODE_ATTRIBUTION_HEADER"
)

// defaultMaxOutput is the output limit given to Claude Code when the chosen
// model's own limit is higher or unknown.
const defaultMaxOutput = 16000

// claudeRemoveVars are the variables taken out of the child's environment:
// the EVREN key (Claude Code never gets it), the caller's own Anthropic
// token and key (the worker would otherwise carry a real Anthropic token into
// an unattended, model-driven process; "ulgen run claude" writes its own back
// in claudeVars), and the variables that would send Claude Code to a real
// Anthropic service or a cloud provider. On Windows the names are compared
// without case.
var claudeRemoveVars = []string{
	"EVREN_API_KEY",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_MANTLE",
	"CLAUDE_CODE_USE_ANTHROPIC_AWS",
}

// maxOutput is the value of CLAUDE_CODE_MAX_OUTPUT_TOKENS: the chosen model's
// own output limit when that is lower than defaultMaxOutput, else
// defaultMaxOutput.
func maxOutput(s translatorSetup) int {
	if s.maxOutputs > 0 && s.maxOutputs < defaultMaxOutput {
		return s.maxOutputs
	}
	return defaultMaxOutput
}

// claudeVars returns the variables to set for a translator on baseURL with
// the given token, in the order they are written. tokenVar is the variable
// the token is written to; the two translated commands differ here (design,
// step 4 of "ulgen worker"): "ulgen run claude" uses ANTHROPIC_AUTH_TOKEN,
// the worker's bare mode reads ANTHROPIC_API_KEY.
func claudeVars(baseURL, token, tokenVar string, s translatorSetup) []string {
	return []string{
		claudeEnvBaseURL + "=" + baseURL,
		tokenVar + "=" + token,
		claudeEnvModel + "=" + s.def,
		claudeEnvOpus + "=" + s.def,
		claudeEnvSonnet + "=" + s.def,
		claudeEnvSubagent + "=" + s.def,
		claudeEnvHaiku + "=" + s.small,
		claudeEnvDisableBetas + "=1",
		claudeEnvUnneeded + "=1",
		claudeEnvAttribution + "=0",
	}
}

// claudeLimits returns the limits Claude Code is told, in the order they are
// written: CLAUDE_CODE_MAX_OUTPUT_TOKENS, and CLAUDE_CODE_MAX_CONTEXT_TOKENS
// (the chosen model's context window) when EVREN gives one. "ulgen run
// claude", the worker and "ulgen proxy --print-env" all take them from here,
// so the three cannot tell Claude Code different things.
func claudeLimits(s translatorSetup) []string {
	limits := []string{fmt.Sprintf("%s=%d", claudeEnvMaxOutput, maxOutput(s))}
	if s.context > 0 {
		limits = append(limits, fmt.Sprintf("%s=%d", claudeEnvMaxContext, s.context))
	}
	return limits
}

// claudeChildEnv returns parent with the variables of step 4 of the design
// set or removed: the local address and the token, the model variables, the
// two switches, the attribution switch, and the limits of claudeLimits
// unless the caller already set them. tokenVar names where the token is written
// (the worker's one difference from "ulgen run claude" here). The EVREN key
// and the variables that name another provider are removed, exactly once
// each; foldCase is for Windows, where the names of variables differ in case
// only on paper. Every other entry of the caller's environment stays.
func claudeChildEnv(parent []string, baseURL, token, tokenVar string, s translatorSetup, foldCase bool) []string {
	set := claudeVars(baseURL, token, tokenVar, s)
	for _, kv := range claudeLimits(s) {
		name, _, _ := strings.Cut(kv, "=")
		if !hasVar(parent, name, foldCase) {
			set = append(set, kv)
		}
	}
	out := make([]string, 0, len(parent)+len(set))
	for _, kv := range parent {
		if replaces(kv, set, foldCase) || removes(kv, foldCase) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, set...)
}

// removes reports whether an environment entry names a variable the child
// must not have.
func removes(kv string, foldCase bool) bool {
	name, _, _ := strings.Cut(kv, "=")
	return namesAny(name, claudeRemoveVars, foldCase)
}

// replaces reports whether an environment entry names a variable that set
// writes again, so the entry is dropped and the value of set used instead.
func replaces(kv string, set []string, foldCase bool) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, kv := range set {
		want, _, _ := strings.Cut(kv, "=")
		if name == want || foldCase && strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}

// hasVar reports whether env names the variable name, directly or, with
// foldCase, in any case.
func hasVar(env []string, name string, foldCase bool) bool {
	for _, kv := range env {
		got, _, _ := strings.Cut(kv, "=")
		if got == name || foldCase && strings.EqualFold(got, name) {
			return true
		}
	}
	return false
}

// namesAny reports whether name is one of names, directly or, with foldCase,
// in any case.
func namesAny(name string, names []string, foldCase bool) bool {
	for _, want := range names {
		if name == want || foldCase && strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}
