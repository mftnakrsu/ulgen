package proxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// jsonEqual reports whether two decoded JSON values are equal.
func jsonEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// decodeAny decodes a JSON object for comparison.
func decodeAny(t *testing.T, b []byte) any {
	t.Helper()
	var m any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

// assertRequest translates an Anthropic request body and compares the whole
// OpenAI JSON that would be sent, both sides decoded so key order is not
// relevant.
func assertRequest(t *testing.T, body, wantBody string) {
	t.Helper()
	var req anthropicRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode anthropic request: %v", err)
	}
	out, _, err := toOpenAI(req, "m")
	if err != nil {
		t.Fatalf("toOpenAI: %v", err)
	}
	got, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("encode openai request: %v", err)
	}
	if !jsonEqual(decodeAny(t, got), decodeAny(t, []byte(wantBody))) {
		g, _ := json.MarshalIndent(decodeAny(t, got), "", "  ")
		w, _ := json.MarshalIndent(decodeAny(t, []byte(wantBody)), "", "  ")
		t.Fatalf("request mismatch\n got: %s\nwant: %s", g, w)
	}
}

// TestToOpenAISystem covers the design's system rules and cases 1-5 of the
// rules note.
func TestToOpenAISystem(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "absent system yields no system message",
			body: `{"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "empty system yields no system message",
			body: `{"system":"","messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "string system becomes one message",
			body: `{"system":"be brief","messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"be brief"}]}`,
		},
		{
			name: "text blocks are joined with a blank line",
			body: `{"system":[{"type":"text","text":"one"},{"type":"text","text":"two"}],"messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"one\n\ntwo"}]}`,
		},
		{
			name: "cache_control is dropped",
			body: `{"system":[{"type":"text","text":"one","cache_control":{"type":"ephemeral"}}],"messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"one"}]}`,
		},
		{
			name: "a non-text system block is dropped",
			body: `{"system":[{"type":"image","source":{"type":"url","url":"http://x"}}],"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAIBillingHeader checks that billing-header lines are removed from
// the system text before it is sent.
func TestToOpenAIBillingHeader(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a billing line is removed",
			body: `{"system":"x-anthropic-billing-header: cc_version=2; cch=abc;\nkeep me","messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"keep me"}]}`,
		},
		{
			name: "a billing line after spaces is removed",
			body: `{"system":"  x-anthropic-billing-header: cch=abc;\nkeep me","messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"keep me"}]}`,
		},
		{
			name: "a system that is only a billing line is empty",
			body: `{"system":"x-anthropic-billing-header: cch=abc;","messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "the marker elsewhere in a line is kept",
			body: `{"system":"see x-anthropic-billing-header: here","messages":[]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"see x-anthropic-billing-header: here"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAIFields covers the design's list of fields passed on and dropped,
// and cases 6-10 of the rules note as the design keeps them.
func TestToOpenAIFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "max_tokens temperature and top_p are passed on",
			body: `{"max_tokens":100,"temperature":0.5,"top_p":0.9,"messages":[]}`,
			want: `{"model":"m","max_tokens":100,"temperature":0.5,"top_p":0.9,"messages":[]}`,
		},
		{
			name: "top_k is dropped",
			body: `{"top_k":40,"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "stop_sequences become stop",
			body: `{"stop_sequences":["A","B"],"messages":[]}`,
			want: `{"model":"m","stop":["A","B"],"messages":[]}`,
		},
		{
			name: "an empty stop_sequences list sets no stop",
			body: `{"stop_sequences":[],"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "stream sets stream and stream_options",
			body: `{"stream":true,"messages":[]}`,
			want: `{"model":"m","stream":true,"stream_options":{"include_usage":true},"messages":[]}`,
		},
		{
			name: "stream false sets neither",
			body: `{"stream":false,"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "metadata is dropped",
			body: `{"metadata":{"user_id":"{\"session_id\":\"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl\"}"},"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "thinking is dropped",
			body: `{"thinking":{"type":"enabled","budget_tokens":2048},"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "output_config and context_management are dropped",
			body: `{"output_config":{"effort":"high"},"context_management":{"x":1},"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "an unknown top-level field is dropped",
			body: `{"something_new":true,"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAIMessages covers the design's message and content-block rules and
// cases 11-22 of the rules note.
func TestToOpenAIMessages(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty user string yields no message",
			body: `{"messages":[{"role":"user","content":""}]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "text and image keep their order",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAA"}}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAA"}}]}]}`,
		},
		{
			name: "a URL image keeps its URL",
			body: `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"http://img/x.png"}}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://img/x.png"}}]}]}`,
		},
		{
			name: "a tool result comes before the user text",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"now"},{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"toolu_1","content":"ok"},{"role":"user","content":[{"type":"text","text":"now"}]}]}`,
		},
		{
			name: "an empty tool result content is an empty string",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[]}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"toolu_1","content":""}]}`,
		},
		{
			name: "a single text tool result is a plain string",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"done"}]}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"toolu_1","content":"done"}]}`,
		},
		{
			name: "a tool result of text and image becomes a list",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"see"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAA"}}]}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"toolu_1","content":[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAA"}}]}]}`,
		},
		{
			name: "an error tool result has Error: in front",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"boom","is_error":true}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"toolu_1","content":"Error: boom"}]}`,
		},
		{
			name: "an assistant text and tool use is one message",
			body: `{"messages":[{"role":"assistant","content":[{"type":"text","text":"let me"},{"type":"tool_use","id":"toolu_1","name":"f","input":{"a":1}}]}]}`,
			want: `{"model":"m","messages":[{"role":"assistant","content":"let me","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"f","arguments":"{\"a\":1}"}}]}]}`,
		},
		{
			name: "assistant text is concatenated into a string",
			body: `{"messages":[{"role":"assistant","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}]}`,
			want: `{"model":"m","messages":[{"role":"assistant","content":"onetwo"}]}`,
		},
		{
			name: "a tool use with no input gets empty arguments",
			body: `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"f"}]}]}`,
			want: `{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`,
		},
		{
			name: "an empty assistant message is dropped",
			body: `{"messages":[{"role":"assistant","content":[]}]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "thinking blocks of an earlier turn are dropped",
			body: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"s"},{"type":"redacted_thinking","data":"x"},{"type":"text","text":"answer"}]}]}`,
			want: `{"model":"m","messages":[{"role":"assistant","content":"answer"}]}`,
		},
		{
			name: "an assistant of only thinking is dropped",
			body: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"s"}]}]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "a document block is dropped",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AAA"}}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"see"}]}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAISystemMessages covers a message whose role is system. Claude
// Code sends one after the first user turn to a model it does not know, and it
// carries the working directory, the platform and the date; a translator that
// leaves it out leaves the model to guess where it is. EVREN's models do not
// all take a system message in that place, so its text travels in the user
// turn next to it.
func TestToOpenAISystemMessages(t *testing.T) {
	const toolTurn = `{"role":"user","content":"time?"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"clock","input":{}}]}`
	const toolTurnOut = `{"role":"user","content":"time?"},{"role":"assistant","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"clock","arguments":"{}"}}]}`
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "after the first user turn it joins that turn",
			body: `{"system":"be brief","messages":[{"role":"user","content":"hi"},{"role":"system","content":"# Environment\n - Primary working directory: /work"}]}`,
			want: `{"model":"m","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"<system-reminder>\n# Environment\n - Primary working directory: /work\n</system-reminder>"}]}]}`,
		},
		{
			name: "after a turn of tool results it follows the tool messages",
			body: `{"messages":[` + toolTurn + `,{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"12:00"}]},{"role":"system","content":"note"}]}`,
			want: `{"model":"m","messages":[` + toolTurnOut + `,{"role":"tool","tool_call_id":"toolu_1","content":"12:00"},{"role":"user","content":[{"type":"text","text":"<system-reminder>\nnote\n</system-reminder>"}]}]}`,
		},
		{
			name: "before a turn of tool results it still follows the tool messages",
			body: `{"messages":[` + toolTurn + `,{"role":"system","content":"note"},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"12:00"}]}]}`,
			want: `{"model":"m","messages":[` + toolTurnOut + `,{"role":"tool","tool_call_id":"toolu_1","content":"12:00"},{"role":"user","content":[{"type":"text","text":"<system-reminder>\nnote\n</system-reminder>"}]}]}`,
		},
		{
			name: "with no user turn before it, it joins the one after it",
			body: `{"messages":[{"role":"system","content":"note"},{"role":"user","content":"hi"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nnote\n</system-reminder>"},{"type":"text","text":"hi"}]}]}`,
		},
		{
			name: "with no user turn next to it, it is a user message of its own",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"one"},{"role":"system","content":"note"},{"role":"assistant","content":"two"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"one"},{"role":"user","content":[{"type":"text","text":"<system-reminder>\nnote\n</system-reminder>"}]},{"role":"assistant","content":"two"}]}`,
		},
		{
			name: "at the end, after an assistant turn, it is a user message of its own",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"one"},{"role":"system","content":"note"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"one"},{"role":"user","content":[{"type":"text","text":"<system-reminder>\nnote\n</system-reminder>"}]}]}`,
		},
		{
			name: "several in a row join the same turn in their order",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"system","content":"a"},{"role":"system","content":"b"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"<system-reminder>\na\n</system-reminder>"},{"type":"text","text":"<system-reminder>\nb\n</system-reminder>"}]}]}`,
		},
		{
			name: "its text blocks are joined like the system field's",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"system","content":[{"type":"text","text":"one","cache_control":{"type":"ephemeral"}},{"type":"text","text":"two"}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"<system-reminder>\none\n\ntwo\n</system-reminder>"}]}]}`,
		},
		{
			name: "an empty one changes nothing",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"system","content":""},{"role":"system","content":[]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "a message of an unknown role is still left out",
			body: `{"messages":[{"role":"user","content":"hi"},{"role":"developer","content":"x"}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestFoldSystemMessagesLeavesTheRequestAlone checks that joining a system
// message to a user turn builds new content: the request that was decoded is
// read again for the token estimate and must not have grown.
func TestFoldSystemMessagesLeavesTheRequestAlone(t *testing.T) {
	var req anthropicRequest
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"system","content":"note"}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	// Room to grow in place, which is where an append would write.
	blocks := make([]contentBlock, 1, 4)
	copy(blocks, req.Messages[0].Content.blocks)
	req.Messages[0].Content.blocks = blocks
	folded := foldSystemMessages(req.Messages)
	if len(folded) != 1 || len(folded[0].Content.blocks) != 2 {
		t.Fatalf("folded = %+v, want one user turn of two blocks", folded)
	}
	if len(req.Messages) != 2 || len(req.Messages[0].Content.blocks) != 1 {
		t.Fatalf("the request was changed: %+v", req.Messages)
	}
	if got := blocks[:2][1]; got.Text != "" {
		t.Fatalf("the request's own blocks were written to: %+v", got)
	}
}

// TestToOpenAIEmptyTextBlocks covers finding 5: an empty text block is dropped
// wherever content parts are built, and a text part always carries its text
// member.
func TestToOpenAIEmptyTextBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "an empty user text block is dropped",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":""}]}]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "an empty text block next to a real one is dropped",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":""},{"type":"text","text":"hi"}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`,
		},
		{
			name: "an empty text in a tool result list is dropped",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"text","text":""},{"type":"text","text":"ok"}]}]}]}`,
			want: `{"model":"m","messages":[{"role":"tool","tool_call_id":"t","content":"ok"}]}`,
		},
		{
			name: "an empty assistant text block leaves the tool call only",
			body: `{"messages":[{"role":"assistant","content":[{"type":"text","text":""},{"type":"tool_use","id":"t","name":"f","input":{}}]}]}`,
			want: `{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"t","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`,
		},
		{
			name: "a text part always has its text member",
			body: `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"http://x"}},{"type":"text","text":"look"}]}]}`,
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://x"}},{"type":"text","text":"look"}]}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestShortenToolNameUnicodeBoundary covers finding 9: a name of multi-byte
// characters is shortened at a rune boundary, and the answer restores it.
func TestShortenToolNameUnicodeBoundary(t *testing.T) {
	long := strings.Repeat("ş", 70) // 70 two-byte characters, 140 bytes
	names := nameMap{}
	got := shortenToolName(long, names)
	if !utf8.ValidString(got) {
		t.Fatalf("shortened name is not valid UTF-8: %q", got)
	}
	if len(got) > 64 {
		t.Fatalf("shortened name is %d bytes, want at most 64: %q", len(got), got)
	}
	if names[got] != long {
		t.Fatalf("the map does not restore %q", got)
	}
	if restoreToolName(got, names) != long {
		t.Fatalf("restoreToolName(%q) did not give the original back", got)
	}
}

// TestSystemBillingOnlyBlockIsSkipped covers finding 11: a system part that is
// empty after the billing-header lines are removed is skipped, the joined text
// has no leading or trailing blank lines, and \r\n is handled like \n.
func TestSystemBillingOnlyBlockIsSkipped(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a billing-only block leaves no blank line",
			body: "{\"system\":[{\"type\":\"text\",\"text\":\"a\\r\\nx-anthropic-billing-header: cc=1\\r\\nb\"},{\"type\":\"text\",\"text\":\"x-anthropic-billing-header: z\"}],\"messages\":[]}",
			want: `{"model":"m","messages":[{"role":"system","content":"a\nb"}]}`,
		},
		{
			name: "a string system with a trailing billing line has no trailing blank",
			body: "{\"system\":\"keep\\nx-anthropic-billing-header: cc=1\",\"messages\":[]}",
			want: `{"model":"m","messages":[{"role":"system","content":"keep"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestShortenToolName checks that a name of 65 or more characters is shortened
// to 64 and that the map restores it.
func TestShortenToolName(t *testing.T) {
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 69
	names := nameMap{}
	got := shortenToolName(long, names)
	if len(got) != 64 {
		t.Fatalf("shortened name has %d characters, want 64: %q", len(got), got)
	}
	if got[55] != '_' {
		t.Fatalf("shortened name %q does not put an underscore at 55", got)
	}
	if names[got] != long {
		t.Fatalf("map[%q] = %q, want %q", got, names[got], long)
	}
	short := "short"
	if shortenToolName(short, names) != short {
		t.Fatalf("a short name must be unchanged")
	}
	if _, ok := names[short]; ok {
		t.Fatalf("a short name must not enter the map")
	}
}

// TestToOpenAITools covers the design's tool rules and cases 20, 23-26 of the
// rules note as the design keeps them.
func TestToOpenAITools(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a function tool keeps the input schema",
			body: `{"tools":[{"name":"f","description":"does f","input_schema":{"type":"object","properties":{"a":{"type":"string"}}}}],"messages":[]}`,
			want: `{"model":"m","tools":[{"type":"function","function":{"name":"f","description":"does f","parameters":{"type":"object","properties":{"a":{"type":"string"}}}}}],"messages":[]}`,
		},
		{
			name: "a tool without an input schema is left out",
			body: `{"tools":[{"name":"web_search"}],"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "a web search tool is left out",
			body: `{"tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "a tool with a blank name is left out",
			body: `{"tools":[{"name":"","input_schema":{"type":"object"}}],"messages":[]}`,
			want: `{"model":"m","messages":[]}`,
		},
		{
			name: "extra tool keys are not merged into parameters",
			body: `{"tools":[{"name":"f","input_schema":{"type":"object"},"foo":"bar","type":"custom"}],"messages":[]}`,
			want: `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"messages":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAItoolChoiceShortensName checks that a tool_choice naming a long
// tool uses the shortened name.
func TestToOpenAItoolChoiceShortensName(t *testing.T) {
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 69
	body := `{"tools":[{"name":"f","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"` + long + `"},"messages":[]}`
	var req anthropicRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, names, err := toOpenAI(req, "m")
	if err != nil {
		t.Fatalf("toOpenAI: %v", err)
	}
	if out.ToolChoice == nil || !out.ToolChoice.isFunc {
		t.Fatalf("tool_choice = %+v, want a function form", out.ToolChoice)
	}
	if len(out.ToolChoice.name) != 64 {
		t.Fatalf("tool_choice name %q is not 64 characters", out.ToolChoice.name)
	}
	if names[out.ToolChoice.name] != long {
		t.Fatalf("the shortened name is not in the map")
	}
	b, _ := json.Marshal(out.ToolChoice)
	want := `{"type":"function","function":{"name":"` + out.ToolChoice.name + `"}}`
	if !jsonEqual(decodeAny(t, b), decodeAny(t, []byte(want))) {
		t.Fatalf("tool_choice JSON = %s, want %s", b, want)
	}
}

// TestToOpenAIToolChoice covers the design's tool_choice mapping and case 27
// of the rules note. A function tool is sent along, because tool_choice is
// only meaningful with one.
func TestToOpenAIToolChoice(t *testing.T) {
	const tool = `"tools":[{"name":"f","input_schema":{"type":"object"}}],`
	tests := []struct {
		name string
		body string
		want string
	}{
		{"auto stays auto", `{` + tool + `"tool_choice":{"type":"auto"},"messages":[]}`, `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"tool_choice":"auto","messages":[]}`},
		{"any becomes required", `{` + tool + `"tool_choice":{"type":"any"},"messages":[]}`, `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"tool_choice":"required","messages":[]}`},
		{"none stays none", `{` + tool + `"tool_choice":{"type":"none"},"messages":[]}`, `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"tool_choice":"none","messages":[]}`},
		{"tool becomes a function form", `{` + tool + `"tool_choice":{"type":"tool","name":"f"},"messages":[]}`, `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"f"}},"messages":[]}`},
		{"a bare string is read", `{` + tool + `"tool_choice":"auto","messages":[]}`, `{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"tool_choice":"auto","messages":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertRequest(t, tt.body, tt.want) })
	}
}

// TestToOpenAIToolChoiceLeftOutWithoutTools covers that tool_choice is dropped
// when no function tool is sent: a provider rejects it.
func TestToOpenAIToolChoiceLeftOutWithoutTools(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no tools", `{"tool_choice":{"type":"any"},"messages":[]}`},
		{"only a tool without a schema", `{"tools":[{"type":"web_search","name":"w"}],"tool_choice":{"type":"any"},"messages":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRequest(t, tt.body, `{"model":"m","messages":[]}`)
		})
	}
}

// TestToOpenAIToolChoiceUnknown checks that an unknown tool_choice type is an
// error.
func TestToOpenAIToolChoiceUnknown(t *testing.T) {
	var req anthropicRequest
	if err := json.Unmarshal([]byte(`{"tools":[{"name":"f","input_schema":{"type":"object"}}],"tool_choice":{"type":"weird"},"messages":[]}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, err := toOpenAI(req, "m"); err == nil {
		t.Fatal("an unknown tool_choice type must be an error")
	}
}

// TestToOpenAIModel checks that the model that was chosen is the one sent.
func TestToOpenAIModel(t *testing.T) {
	var req anthropicRequest
	if err := json.Unmarshal([]byte(`{"model":"claude-opus-4","messages":[]}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, _, err := toOpenAI(req, "evren-large")
	if err != nil {
		t.Fatalf("toOpenAI: %v", err)
	}
	if out.Model != "evren-large" {
		t.Fatalf("model = %q, want evren-large", out.Model)
	}
}
