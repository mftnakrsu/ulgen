package proxy

import "slices"

// thinkingConfig is the request's thinking field. Claude Code sends
// {"type":"adaptive"} while thinking is on and leaves the field out when it
// is off; "enabled" with a budget and "disabled" are the API's older forms.
type thinkingConfig struct {
	Type string `json:"type"`
}

// outputConfig is the request's output_config field, of which only the
// effort is read: low, medium, high, xhigh or max.
type outputConfig struct {
	Effort string `json:"effort"`
}

// effortLevels maps Claude Code's effort to EVREN's reasoning_effort. "max"
// becomes "xhigh": some EVREN models refuse "max", and all that take an
// effort take "xhigh".
var effortLevels = map[string]string{
	"low":    "low",
	"medium": "medium",
	"high":   "high",
	"xhigh":  "xhigh",
	"max":    "xhigh",
}

// reasoningEffort returns the reasoning_effort for a request to a model that
// takes one, or "" for none. With thinking off (the field left out, or
// "disabled") the model is asked not to reason; with thinking on, the
// request's effort is passed on, and without one the model keeps its own
// default. values are the efforts EVREN lists for the model, nil when it
// lists none; a value outside the list is not sent, since EVREN refuses it.
func reasoningEffort(req anthropicRequest, values []string) string {
	var want string
	switch {
	case req.Thinking == nil || req.Thinking.Type == "disabled":
		want = "none"
	case req.OutputConfig != nil:
		want = effortLevels[req.OutputConfig.Effort]
	}
	if want == "" || values != nil && !slices.Contains(values, want) {
		return ""
	}
	return want
}
