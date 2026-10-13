package proxy

import (
	"encoding/json"
	"testing"
)

// decodeResp decodes an Anthropic response body for comparison.
func decodeResp(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode response %s: %v", b, err)
	}
	return m
}

// responseJSON encodes an anthropicResponse so the test compares the whole
// Anthropic JSON.
func responseJSON(t *testing.T, r anthropicResponse) map[string]any {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return decodeResp(t, b)
}

// TestFromOpenAI covers the design's "Response" section, without streaming.
func TestFromOpenAI(t *testing.T) {
	tests := []struct {
		name string
		body string
		// names restores shortened tool names.
		names nameMap
		// model is the model named in the answer.
		model string
		want  string
	}{
		{
			name:  "text only",
			body:  `{"id":"chatcmpl-1","choices":[{"message":{"content":"Hello"},"finish_reason":"stop"}]}`,
			model: "evren-large",
			want: `{
				"id":"chatcmpl-1","type":"message","role":"assistant","model":"evren-large",
				"content":[{"type":"text","text":"Hello"}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "thinking in front of the text",
			body:  `{"id":"c2","choices":[{"message":{"reasoning":"step one","content":"Answer"},"finish_reason":"stop"}]}`,
			model: "m",
			want: `{
				"id":"c2","type":"message","role":"assistant","model":"m",
				"content":[{"type":"thinking","thinking":"step one","signature":""},{"type":"text","text":"Answer"}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "reasoning_content is read too",
			body:  `{"id":"c3","choices":[{"message":{"reasoning_content":"why","content":""},"finish_reason":"stop"}]}`,
			model: "m",
			want: `{
				"id":"c3","type":"message","role":"assistant","model":"m",
				"content":[{"type":"thinking","thinking":"why","signature":""}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "text and a tool call with parsed arguments",
			body:  `{"id":"c4","choices":[{"message":{"content":"let me look","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}`,
			model: "m",
			want: `{
				"id":"c4","type":"message","role":"assistant","model":"m",
				"content":[{"type":"text","text":"let me look"},{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "invalid tool arguments become an empty object",
			body:  `{"id":"c5","choices":[{"message":{"tool_calls":[{"id":"toolu_2","function":{"name":"f","arguments":"{not json"}}]},"finish_reason":"tool_calls"}]}`,
			model: "m",
			want: `{
				"id":"c5","type":"message","role":"assistant","model":"m",
				"content":[{"type":"tool_use","id":"toolu_2","name":"f","input":{}}],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "empty tool arguments become an empty object",
			body:  `{"id":"c6","choices":[{"message":{"tool_calls":[{"id":"toolu_3","function":{"name":"f","arguments":""}}]},"finish_reason":"tool_calls"}]}`,
			model: "m",
			want: `{
				"id":"c6","type":"message","role":"assistant","model":"m",
				"content":[{"type":"tool_use","id":"toolu_3","name":"f","input":{}}],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "a shortened tool name is restored",
			body:  `{"id":"c7","choices":[{"message":{"tool_calls":[{"id":"toolu_4","function":{"name":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_00000000","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			names: nameMap{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_00000000": "the-original-long-name"},
			model: "m",
			want: `{
				"id":"c7","type":"message","role":"assistant","model":"m",
				"content":[{"type":"tool_use","id":"toolu_4","name":"the-original-long-name","input":{}}],
				"stop_reason":"tool_use","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
		{
			name:  "no choices ends the turn",
			body:  `{"id":"c8","choices":[]}`,
			model: "m",
			want: `{
				"id":"c8","type":"message","role":"assistant","model":"m","content":[],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp openAIResponse
			if err := json.Unmarshal([]byte(tt.body), &resp); err != nil {
				t.Fatalf("decode openai response: %v", err)
			}
			got := responseJSON(t, fromOpenAI(resp, tt.model, tt.names))
			want := decodeResp(t, []byte(tt.want))
			if !jsonEqual(got, want) {
				g, _ := json.MarshalIndent(got, "", "  ")
				w, _ := json.MarshalIndent(want, "", "  ")
				t.Fatalf("response mismatch\n got: %s\nwant: %s", g, w)
			}
		})
	}
}

// TestFromOpenAIContentShapes covers finding 13: message.content may be a
// string, null, or a list of parts; from a list the text of every text part is
// taken, joined in order.
func TestFromOpenAIContentShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a string",
			body: `{"id":"c","choices":[{"message":{"content":"hello"}}]}`,
			want: "hello",
		},
		{
			name: "null",
			body: `{"id":"c","choices":[{"message":{"content":null}}]}`,
			want: "",
		},
		{
			name: "a list of parts",
			body: `{"id":"c","choices":[{"message":{"content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"b"}]}}]}`,
			want: "ab",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp openAIResponse
			if err := json.Unmarshal([]byte(tt.body), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := fromOpenAI(resp, "m", nil)
			var text string
			for _, b := range got.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			if text != tt.want {
				t.Fatalf("text = %q, want %q", text, tt.want)
			}
		})
	}
}

// TestFromOpenAIStopWithToolCalls covers finding 12 for a non-stream answer: a
// "stop" finish reason with tool calls gives stop_reason tool_use.
func TestFromOpenAIStopWithToolCalls(t *testing.T) {
	body := `{"id":"c","choices":[{"message":{"tool_calls":[{"id":"t","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"stop"}]}`
	var resp openAIResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := fromOpenAI(resp, "m", nil).StopReason; got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
}

// TestStopReason covers the design's stop_reason mapping directly.
func TestStopReason(t *testing.T) {
	tests := []struct {
		finish string
		want   string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"function_call", "tool_use"},
		{"content_filter", "refusal"},
		{"something else", "end_turn"},
		{"", "end_turn"},
	}
	for _, tt := range tests {
		if got := stopReason(tt.finish); got != tt.want {
			t.Errorf("stopReason(%q) = %q, want %q", tt.finish, got, tt.want)
		}
	}
}

// TestResponseUsage covers the design's usage mapping.
func TestResponseUsage(t *testing.T) {
	body := `{"id":"u","choices":[{"message":{"content":"x"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":60}}}`
	var resp openAIResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := fromOpenAI(resp, "m", nil).Usage
	want := anthropicOutUsage{InputTokens: 40, OutputTokens: 7, CacheReadInputTokens: 60}
	if got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

// TestErrorKind covers the design's "Errors from EVREN" status-to-kind table.
func TestErrorKind(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{401, "authentication_error"},
		{403, "permission_error"},
		{404, "not_found_error"},
		{413, "request_too_large"},
		{429, "rate_limit_error"},
		{400, "invalid_request_error"},
		{422, "invalid_request_error"},
		{503, "overloaded_error"},
		{529, "overloaded_error"},
		{500, "api_error"},
		{502, "api_error"},
	}
	for _, tt := range tests {
		if got := errorKind(tt.status); got != tt.want {
			t.Errorf("errorKind(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestErrorBody checks the Anthropic error shape.
func TestErrorBody(t *testing.T) {
	got := decodeResp(t, errorBody("rate_limit_error", "slow down"))
	want := decodeResp(t, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	if !jsonEqual(got, want) {
		t.Fatalf("errorBody mismatch: got %v want %v", got, want)
	}
}
