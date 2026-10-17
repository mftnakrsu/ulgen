package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// streamEvent is one event expected out of the translator: its name and its
// JSON body.
type streamEvent struct {
	name string
	data string
}

// runStream feeds each chunk to a fresh translator and returns the events,
// End included.
func runStream(t *testing.T, chunks []string, model string, names nameMap) []event {
	t.Helper()
	tr := newStreamTranslator(model, names)
	var out []event
	for _, raw := range chunks {
		var ch streamChunk
		if err := json.Unmarshal([]byte(raw), &ch); err != nil {
			t.Fatalf("decode chunk %s: %v", raw, err)
		}
		out = append(out, tr.Feed(ch)...)
	}
	return append(out, tr.End()...)
}

// assertStream compares the decoded JSON of each event, so key order and
// number formatting do not matter.
func assertStream(t *testing.T, got []event, want []streamEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d\ngot: %s", len(got), len(want), dumpEvents(t, got))
	}
	for i := range want {
		if got[i].Name != want[i].name {
			t.Fatalf("event %d name = %q, want %q", i, got[i].Name, want[i].name)
		}
		gb, err := json.Marshal(got[i].Data)
		if err != nil {
			t.Fatalf("encode event %d: %v", i, err)
		}
		if !jsonEqual(decodeAny(t, gb), decodeAny(t, []byte(want[i].data))) {
			t.Fatalf("event %d mismatch\n got: %s\nwant: %s", i, gb, want[i].data)
		}
	}
}

// dumpEvents renders the events for a failure message.
func dumpEvents(t *testing.T, evs []event) string {
	t.Helper()
	out, _ := json.MarshalIndent(evs, "", "  ")
	return string(out)
}

// zeroUsage is the usage a message_start carries.
const zeroUsage = `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`

// TestStream covers the design's streaming bullet and section 5 of the rules
// note: message_start first, a block opened on the first content of its kind
// and closed when the kind changes or another tool call begins, indexes from
// zero, and one message_delta with the finish reason and the usage.
func TestStream(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		names  nameMap
		chunks []string
		want   []streamEvent
	}{
		{
			name:  "text only",
			model: "evren-large",
			chunks: []string{
				`{"id":"c1","choices":[{"delta":{"role":"assistant","content":""}}]}`,
				`{"id":"c1","choices":[{"delta":{"content":"Hello"}}]}`,
				`{"id":"c1","choices":[{"delta":{"content":" world"}}]}`,
				`{"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
				`{"id":"c1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c1","type":"message","role":"assistant","model":"evren-large","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "text then a tool call",
			model: "m",
			chunks: []string{
				`{"id":"c2","choices":[{"delta":{"content":"Hello"}}]}`,
				`{"id":"c2","choices":[{"delta":{"content":" world"}}]}`,
				`{"id":"c2","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]}}]}`,
				`{"id":"c2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
				`{"id":"c2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}}]}`,
				`{"id":"c2","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"c2","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":9}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c2","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"get_weather","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":1}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":9,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "two tool calls",
			model: "m",
			chunks: []string{
				`{"id":"c3","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"f","arguments":"{\"a\":1}"}}]}}]}`,
				// An id-only fragment of the first call opens no block and its
				// arguments stay in index 0.
				`{"id":"c3","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":" "}}]}}]}`,
				`{"id":"c3","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"g","arguments":"{\"b\":2}"}}]}}]}`,
				`{"id":"c3","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"c3","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c3","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_a","name":"f","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":" "}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_b","name":"g","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"b\":2}"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":1}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "arguments in five fragments",
			model: "m",
			chunks: []string{
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"f","arguments":""}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\""}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"a"}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":"}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1"}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
				`{"id":"c4","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"c4","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c4","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"f","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"a"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\":"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"1"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"}"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "reasoning then text",
			model: "m",
			chunks: []string{
				`{"id":"c5","choices":[{"delta":{"reasoning":"think"}}]}`,
				`{"id":"c5","choices":[{"delta":{"reasoning_content":" more"}}]}`,
				`{"id":"c5","choices":[{"delta":{"content":"Answer"}}]}`,
				`{"id":"c5","choices":[{"delta":{},"finish_reason":"stop"}]}`,
				`{"id":"c5","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c5","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" more"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":1}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "usage arrives in a later choiceless chunk",
			model: "m",
			chunks: []string{
				`{"id":"c6","choices":[{"delta":{"content":"hi"}}]}`,
				`{"id":"c6","choices":[{"delta":{},"finish_reason":"length"}]}`,
				`{"id":"c6","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":60}}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c6","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"input_tokens":40,"output_tokens":7,"cache_creation_input_tokens":0,"cache_read_input_tokens":60}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "a stream that ends without a finish reason ends with end_turn",
			model: "m",
			chunks: []string{
				`{"id":"c7","choices":[{"delta":{"content":"partial"}}]}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c7","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":` + zeroUsage + `}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "a shortened tool name is restored in content_block_start",
			model: "m",
			names: nameMap{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_00000000": "the-original-long-name"},
			chunks: []string{
				`{"id":"c8","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_00000000","arguments":"{}"}}]}}]}`,
				`{"id":"c8","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"c8","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c8","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"the-original-long-name","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
		{
			name:  "text, a tool call and text again in one delta",
			model: "m",
			chunks: []string{
				`{"id":"c9","choices":[{"delta":{"content":"before"}}]}`,
				`{"id":"c9","choices":[{"delta":{"content":"after","tool_calls":[{"index":0,"id":"call_1","function":{"name":"f","arguments":"{}"}}]}}]}`,
				`{"id":"c9","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"c9","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			},
			want: []streamEvent{
				{"message_start", `{"type":"message_start","message":{"id":"c9","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
				{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"before"}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"after"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":0}`},
				{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"f","input":{}}}`},
				{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`},
				{"content_block_stop", `{"type":"content_block_stop","index":1}`},
				{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
				{"message_stop", `{"type":"message_stop"}`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runStream(t, tt.chunks, tt.model, tt.names)
			assertStream(t, got, tt.want)
		})
	}
}

// TestStreamEndWithoutChunks checks that an empty stream still carries the
// message_start, message_delta and message_stop a client expects, with a
// generated id.
func TestStreamEndWithoutChunks(t *testing.T) {
	got := runStream(t, nil, "m", nil)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %s", len(got), dumpEvents(t, got))
	}
	start, ok := got[0].Data.(messageStartEvent)
	if !ok || got[0].Name != "message_start" {
		t.Fatalf("event 0 = %+v, want message_start", got[0])
	}
	if len(start.Message.ID) < 4 || start.Message.ID[:4] != "msg_" {
		t.Fatalf("a stream with no provider id got id %q, want a msg_ id", start.Message.ID)
	}
	if got[1].Name != "message_delta" || got[2].Name != "message_stop" {
		t.Fatalf("events 1 and 2 = %q and %q, want message_delta and message_stop", got[1].Name, got[2].Name)
	}
	md, _ := json.Marshal(got[1].Data)
	if !jsonEqual(decodeAny(t, md), decodeAny(t, []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":`+zeroUsage+`}`))) {
		t.Fatalf("message_delta = %s", md)
	}
}

// TestStreamUsageBeforeFinish covers finding 1: a usage in a chunk before any
// finish reason is only remembered, the last one winning; the message is
// finished only once a finish reason has been seen and then a usage arrives.
// All the content comes out.
func TestStreamUsageBeforeFinish(t *testing.T) {
	got := runStream(t, []string{
		`{"id":"c1","choices":[{"delta":{"content":"a"}}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`,
		`{"id":"c1","choices":[{"delta":{"content":"b"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`{"id":"c1","choices":[{"delta":{"content":"c"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`,
		`{"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}, "m", nil)
	assertStream(t, got, []streamEvent{
		{"message_start", `{"type":"message_start","message":{"id":"c1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"b"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"c"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
		{"message_stop", `{"type":"message_stop"}`},
	})
}

// TestStreamDropsContentAfterFinish covers a part of finding 1: content that
// arrives after the finish reason is dropped, before the usage that finishes
// the message.
func TestStreamDropsContentAfterFinish(t *testing.T) {
	got := runStream(t, []string{
		`{"id":"c1","choices":[{"delta":{"content":"kept"},"finish_reason":"stop"}]}`,
		`{"id":"c1","choices":[{"delta":{"content":"dropped"}}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
	}, "m", nil)
	for _, ev := range got {
		if ev.Name == "content_block_delta" {
			b, _ := json.Marshal(ev.Data)
			if strings.Contains(string(b), "dropped") {
				t.Fatalf("content after the finish reason was sent: %s", b)
			}
		}
	}
	assertStream(t, got, []streamEvent{
		{"message_start", `{"type":"message_start","message":{"id":"c1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":` + zeroUsage + `}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"kept"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`},
		{"message_stop", `{"type":"message_stop"}`},
	})
}

// toolBlocks returns, per tool_use block, its id, name and concatenated
// argument text, in the order the blocks were opened.
func toolBlocks(t *testing.T, evs []event) []struct{ id, name, args string } {
	t.Helper()
	var out []struct{ id, name, args string }
	open := -1
	for _, ev := range evs {
		b, _ := json.Marshal(ev.Data)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		switch ev.Name {
		case "content_block_start":
			cb, _ := m["content_block"].(map[string]any)
			if cb["type"] != "tool_use" {
				continue
			}
			idx := int(m["index"].(float64))
			id, _ := cb["id"].(string)
			name, _ := cb["name"].(string)
			out = append(out, struct{ id, name, args string }{id: id, name: name})
			open = idx
		case "content_block_delta":
			if open < 0 || int(m["index"].(float64)) != open {
				continue
			}
			d, _ := m["delta"].(map[string]any)
			if d["type"] != "input_json_delta" {
				continue
			}
			pj, _ := d["partial_json"].(string)
			out[len(out)-1].args += pj
		case "content_block_stop":
			open = -1
		}
	}
	return out
}

// TestStreamInterleavedCalls covers finding 2: two calls whose fragments
// interleave keep their own arguments, both valid JSON.
func TestStreamInterleavedCalls(t *testing.T) {
	got := runStream(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"g","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"y\":2}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}, "m", nil)
	blocks := toolBlocks(t, got)
	if len(blocks) != 2 {
		t.Fatalf("got %d tool blocks, want 2: %s", len(blocks), dumpEvents(t, got))
	}
	if blocks[0].id != "a" || blocks[0].name != "f" || blocks[0].args != `{"x":1}` {
		t.Errorf("block 0 = %+v", blocks[0])
	}
	if blocks[1].id != "b" || blocks[1].name != "g" || blocks[1].args != `{"y":2}` {
		t.Errorf("block 1 = %+v", blocks[1])
	}
	for i, b := range blocks {
		var v any
		if err := json.Unmarshal([]byte(b.args), &v); err != nil {
			t.Errorf("block %d arguments %q are not valid JSON: %v", i, b.args, err)
		}
	}
	// finish with tool_use
	last := got[len(got)-2]
	b, _ := json.Marshal(last.Data)
	if !jsonEqual(decodeAny(t, b), decodeAny(t, []byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":`+zeroUsage+`}`))) {
		t.Errorf("message_delta = %s", b)
	}
}

// TestStreamIdAndArgsBeforeName covers finding 2: a call whose id and first
// argument fragment come in one chunk and whose name comes in the next keeps
// the id and the whole argument text.
func TestStreamIdAndArgsBeforeName(t *testing.T) {
	got := runStream(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"arguments":"{\"p\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":"1}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}, "m", nil)
	blocks := toolBlocks(t, got)
	if len(blocks) != 1 {
		t.Fatalf("got %d tool blocks, want 1: %s", len(blocks), dumpEvents(t, got))
	}
	if blocks[0].id != "a" || blocks[0].name != "f" || blocks[0].args != `{"p":1}` {
		t.Errorf("block = %+v", blocks[0])
	}
}

// TestStreamSequentialCalls covers finding 2: two calls whose fragments do not
// interleave become two blocks with indexes without gaps.
func TestStreamSequentialCalls(t *testing.T) {
	got := runStream(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{\"a\":1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"g","arguments":"{\"b\":2}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}, "m", nil)
	blocks := toolBlocks(t, got)
	if len(blocks) != 2 {
		t.Fatalf("got %d tool blocks, want 2: %s", len(blocks), dumpEvents(t, got))
	}
	if blocks[0].name != "f" || blocks[0].args != `{"a":1}` {
		t.Errorf("block 0 = %+v", blocks[0])
	}
	if blocks[1].name != "g" || blocks[1].args != `{"b":2}` {
		t.Errorf("block 1 = %+v", blocks[1])
	}
	// indexes count up from zero without gaps
	var idxs []int
	for _, ev := range got {
		if ev.Name != "content_block_start" {
			continue
		}
		b, _ := json.Marshal(ev.Data)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		idxs = append(idxs, int(m["index"].(float64)))
	}
	for i, idx := range idxs {
		if idx != i {
			t.Fatalf("block indexes = %v, want 0..n", idxs)
		}
	}
}

// TestStreamStopWithToolCalls covers finding 12: a "stop" finish reason with a
// tool_use block sent becomes stop_reason tool_use.
func TestStreamStopWithToolCalls(t *testing.T) {
	got := runStream(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"stop"}]}`,
	}, "m", nil)
	last := got[len(got)-2]
	if last.Name != "message_delta" {
		t.Fatalf("second to last event = %q, want message_delta", last.Name)
	}
	b, _ := json.Marshal(last.Data)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	d, _ := m["delta"].(map[string]any)
	if d["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v, want tool_use", d["stop_reason"])
	}
}

// TestErrChunkIgnoresEmptyError covers finding 4: a chunk's error member is an
// error only when it is an object with a member or a non-empty string; null,
// "", {} and an absent member are not.
func TestErrChunkIgnoresEmptyError(t *testing.T) {
	notErrors := []string{
		`{"choices":[{"delta":{"content":"a"}}],"error":null}`,
		`{"choices":[{"delta":{"content":"a"}}],"error":""}`,
		`{"choices":[{"delta":{"content":"a"}}],"error":{}}`,
		`{"choices":[{"delta":{"content":"a"}}]}`,
	}
	for _, d := range notErrors {
		if err := errChunk([]byte(d), "k"); err != nil {
			t.Errorf("errChunk(%s) = %v, want nil", d, err)
		}
	}
	errCases := []string{
		`{"error":"boom"}`,
		`{"error":{"message":"boom"}}`,
		`{"error":{"code":1}}`,
	}
	for _, d := range errCases {
		if err := errChunk([]byte(d), "k"); err == nil {
			t.Errorf("errChunk(%s) = nil, want an error", d)
		}
	}
}

// TestStreamError checks the one error event sent when the provider fails
// mid-stream, and that the key is replaced.
func TestStreamError(t *testing.T) {
	ev := streamError("upstream said token evren_llm_TESTONLY0000 is bad", "evren_llm_TESTONLY0000")
	if ev.Name != "error" {
		t.Fatalf("event name = %q, want error", ev.Name)
	}
	body, err := json.Marshal(ev.Data)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got := string(body); got != `{"type":"error","error":{"type":"api_error","message":"upstream said token [key] is bad"}}` {
		t.Fatalf("error event = %s", got)
	}
}

// A provider that sends a running usage with every chunk and the final one in
// a chunk of its own after the finish reason: nothing is cut off, and the
// usage of the message is the final one.
func TestStreamWaitsForTheUsageAfterTheFinishReason(t *testing.T) {
	tr := newStreamTranslator("m", nil)
	var events []event
	feed := func(c streamChunk) { events = append(events, tr.Feed(c)...) }
	feed(streamChunk{Choices: []streamChoice{{Delta: streamDelta{Content: "Hel"}}}, Usage: &openAIUsage{PromptTokens: 10, CompletionTokens: 1}})
	feed(streamChunk{Choices: []streamChoice{{Delta: streamDelta{Content: "lo"}}}, Usage: &openAIUsage{PromptTokens: 10, CompletionTokens: 2}})
	feed(streamChunk{Choices: []streamChoice{{FinishReason: "stop"}}})
	for _, ev := range events {
		if ev.Name == "message_delta" || ev.Name == "message_stop" {
			t.Fatalf("the message ended before the final usage came: %s", ev.Name)
		}
	}
	feed(streamChunk{Usage: &openAIUsage{PromptTokens: 10, CompletionTokens: 7}})
	events = append(events, tr.End()...)
	var text string
	deltas, stops := 0, 0
	for _, ev := range events {
		switch d := ev.Data.(type) {
		case contentBlockDeltaEvent:
			if td, ok := d.Delta.(textDelta); ok {
				text += td.Text
			}
		case messageDeltaEvent:
			deltas++
			if d.Usage.OutputTokens != 7 {
				t.Errorf("output tokens = %d, want the final count 7", d.Usage.OutputTokens)
			}
		case messageStopEvent:
			stops++
		}
	}
	if text != "Hello" || deltas != 1 || stops != 1 {
		t.Errorf("text %q, %d message_delta, %d message_stop", text, deltas, stops)
	}
}

// TestStreamTranslatorBoundsHeldArguments covers a provider that sends
// arguments for a call it never names: they are kept only up to the bound,
// and passing it is flagged so the stream can be ended.
func TestStreamTranslatorBoundsHeldArguments(t *testing.T) {
	tr := newStreamTranslator("m", nameMap{})
	tr.heldLimit = 10
	fragment := func(args string) streamChunk {
		return streamChunk{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCall{
			{Function: openAICallFunction{Arguments: args}},
		}}}}}
	}
	tr.Feed(fragment("12345678"))
	if tr.overflow || tr.calls[0].args != "12345678" {
		t.Fatalf("within the bound: overflow %v, held %q", tr.overflow, tr.calls[0].args)
	}
	tr.Feed(fragment("abc"))
	if !tr.overflow || tr.calls[0].args != "12345678" {
		t.Fatalf("past the bound: overflow %v, held %q", tr.overflow, tr.calls[0].args)
	}
}
