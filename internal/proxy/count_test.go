package proxy

import (
	"encoding/json"
	"testing"
)

// TestEstimateTokens covers the design's "Token counting" rule: the UTF-8
// bytes of the system text, the message texts, the tool definitions and the
// tool results, divided by 3 and rounded up.
func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty request", `{"messages":[]}`, 0},
		{"three bytes round up to one", `{"system":"abc"}`, 1},
		{"a byte over a multiple of three rounds up", `{"system":"abcd"}`, 2},
		{"message text counts", `{"messages":[{"role":"user","content":"abcdef"}]}`, 2},
		{"system and message add up", `{"system":"abc","messages":[{"role":"user","content":"abc"}]}`, 2},
		{"tool definition counts", `{"tools":[{"name":"get_weather","description":"Look it up","input_schema":{"type":"object"}}]}`, 13},
		{"tool result counts its text", `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"abcdef"}]}]}`, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req anthropicRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if got := estimateTokens(req); got != tt.want {
				t.Fatalf("estimateTokens(%s) = %d, want %d", tt.body, got, tt.want)
			}
		})
	}
}
