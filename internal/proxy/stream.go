package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// event is one server-sent event to send to the client: its name and the JSON
// body.
type event struct {
	Name string
	Data any
}

// streamChunk is one decoded provider chunk of a streaming chat completion.
// Usage may arrive in a chunk that has no choices, once include_usage was
// asked for.
type streamChunk struct {
	ID      string         `json:"id"`
	Choices []streamChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage"`
}

// streamChoice is one choice of a streaming chunk.
type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

// streamDelta is the delta of a streaming chunk. Reasoning text is read from
// either the reasoning or the reasoning_content field.
type streamDelta struct {
	Content          string           `json:"content"`
	Reasoning        string           `json:"reasoning"`
	ReasoningContent string           `json:"reasoning_content"`
	ToolCalls        []streamToolCall `json:"tool_calls"`
}

// reasoning returns the reasoning text of a delta, preferring the reasoning
// field and falling back to reasoning_content.
func (d streamDelta) reasoning() string {
	if d.Reasoning != "" {
		return d.Reasoning
	}
	return d.ReasoningContent
}

// streamToolCall is one tool call of a streaming chunk. The first fragments
// carry the id and the name; later fragments carry only argument text. Index
// is the provider's index of the call; when the provider does not send it, the
// call's position in the list stands in for it.
type streamToolCall struct {
	Index    *int               `json:"index"`
	ID       string             `json:"id"`
	Function openAICallFunction `json:"function"`
}

// messageStartEvent is the message_start event body.
type messageStartEvent struct {
	Type    string                 `json:"type"`
	Message anthropicStreamMessage `json:"message"`
}

// anthropicStreamMessage is the message carried by a message_start event. The
// stop reason and the stop sequence are null while the message runs.
type anthropicStreamMessage struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Role         string              `json:"role"`
	Model        string              `json:"model"`
	Content      []anthropicOutBlock `json:"content"`
	StopReason   *string             `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        anthropicOutUsage   `json:"usage"`
}

// contentBlockStartEvent opens a content block.
type contentBlockStartEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock any    `json:"content_block"`
}

// textBlock is the content_block body of a text block.
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// thinkingBlock is the content_block body of a thinking block. EVREN sends no
// signature, so the signature is empty.
type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

// toolUseBlock is the content_block body of a tool use block.
type toolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// contentBlockDeltaEvent carries one delta of the open content block.
type contentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

// textDelta is a piece of assistant text.
type textDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// thinkingDelta is a piece of reasoning text.
type thinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

// inputJSONDelta is a fragment of a tool call's arguments.
type inputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

// contentBlockStopEvent closes a content block.
type contentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// messageDeltaEvent ends the message with its stop reason and usage.
type messageDeltaEvent struct {
	Type  string            `json:"type"`
	Delta messageDeltaBody  `json:"delta"`
	Usage anthropicOutUsage `json:"usage"`
}

// messageDeltaBody is the delta of a message_delta event.
type messageDeltaBody struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// messageStopEvent is the last event of a message.
type messageStopEvent struct {
	Type string `json:"type"`
}

// pingEvent keeps a silent stream alive.
type pingEvent struct {
	Type string `json:"type"`
}

// errorEvent is the single event sent when the provider fails in the middle of
// a stream.
type errorEvent struct {
	Type  string      `json:"type"`
	Error errorDetail `json:"error"`
}

// errorDetail is the error body of an error event.
type errorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// toolCall is one tool call the provider is streaming. Its id, name and
// argument text may arrive in any order and in separate chunks; whether its
// block was opened is remembered so a closed call emits no more deltas.
type toolCall struct {
	id     string
	name   string
	args   string
	opened bool
	sent   bool // its block was opened and then closed
}

// streamTranslator turns decoded provider chunks into Anthropic streaming
// events. It is a pure state machine: Feed is given one chunk and returns the
// events to send, End closes whatever is open and finishes the message. A text
// or thinking block is opened on the first content of its kind and closed when
// the kind changes. Tool calls are tracked by the provider's index; only one
// tool-use block is open at a time, and the others are emitted in index order
// at the end. Indexes count up from zero.
type streamTranslator struct {
	model string
	names nameMap

	started    bool
	id         string
	blockOpen  bool
	blockKind  string // "text", "thinking" or "tool_use"
	blockIndex int
	nextIndex  int

	calls    map[int]*toolCall
	order    []int // the tool call indexes in the order they were first seen
	current  int   // index of the open or last tool call, -1 when none
	toolSent bool  // at least one tool-use block was opened

	// held counts the argument text kept back for calls that could not be
	// sent yet; past heldLimit nothing more is kept and overflow is set, which
	// ends the stream. A provider could otherwise fill the memory with
	// fragments of a call that never gets a name.
	held      int
	heldLimit int
	overflow  bool

	stopReason string
	haveFinish bool
	usage      *openAIUsage
	haveUsage  bool
	deltaSent  bool
	done       bool
}

// newStreamTranslator returns a translator for a message that names model and
// restores tool names through names.
func newStreamTranslator(model string, names nameMap) *streamTranslator {
	return &streamTranslator{model: model, names: names, calls: map[int]*toolCall{}, current: -1, heldLimit: maxStreamEvent}
}

// Feed takes one decoded provider chunk and returns the events to send. Once
// the message_delta has been sent, or End was called, nothing more comes out.
func (t *streamTranslator) Feed(c streamChunk) []event {
	if t.done || t.deltaSent {
		return nil
	}
	if t.id == "" && c.ID != "" {
		t.id = c.ID
	}
	if c.Usage != nil {
		t.usage, t.haveUsage = c.Usage, true
	}

	var out []event
	out = append(out, t.start()...)

	if len(c.Choices) > 0 {
		ch := c.Choices[0]
		// A finish reason may arrive in the same chunk as content, so the
		// content is handled first; after that, content is dropped.
		out = append(out, t.content(ch.Delta)...)
		if !t.haveFinish && ch.FinishReason != "" {
			t.stopReason = stopReason(ch.FinishReason)
			t.haveFinish = true
		}
	}

	// The message is finished only once a finish reason has been seen and
	// then a usage arrives: a usage before any finish reason is only
	// remembered, the last one winning. A provider that never sends usage is
	// finished by End at the end of the stream.
	// The message ends when the finish reason is known and a usage arrives
	// with it or after it. A usage that came before the finish reason is a
	// running count (some providers send one with every chunk): it is kept,
	// and the final one, which may still follow in a chunk of its own, is
	// waited for until the stream ends.
	if t.haveFinish && c.Usage != nil {
		out = append(out, t.finish()...)
	}
	return out
}

// End is called when the provider's stream ends. It closes any open block and
// sends the message_delta and message_stop that were still owed. Usage and the
// finish reason may have arrived in a last chunk; a stream that never named a
// finish reason ends with end_turn.
func (t *streamTranslator) End() []event {
	if t.done {
		return nil
	}
	out := t.start()
	out = append(out, t.finish()...)
	return out
}

// start returns the message_start event once, on the first content or at the
// end when the provider sent nothing at all.
func (t *streamTranslator) start() []event {
	if t.started {
		return nil
	}
	t.started = true
	msg := anthropicStreamMessage{
		ID:      t.messageID(),
		Type:    "message",
		Role:    "assistant",
		Model:   t.model,
		Content: []anthropicOutBlock{},
	}
	return []event{{Name: "message_start", Data: messageStartEvent{Type: "message_start", Message: msg}}}
}

// messageID returns the message id: the provider's id when it sent one, else a
// fresh msg_<hex> so the client always has an id.
func (t *streamTranslator) messageID() string {
	if t.id != "" {
		return t.id
	}
	return "msg_" + randomHex(16)
}

// content translates one delta into the events it produces. Text and
// reasoning close an open tool-use block when their kind differs, and a new
// block is opened for them; a tool call opens its own block once its name is
// known.
func (t *streamTranslator) content(d streamDelta) []event {
	if t.haveFinish {
		return nil
	}
	var out []event
	if r := d.reasoning(); r != "" {
		out = append(out, t.open("thinking")...)
		out = append(out, t.delta(thinkingDelta{Type: "thinking_delta", Thinking: r})...)
	}
	if d.Content != "" {
		out = append(out, t.open("text")...)
		out = append(out, t.delta(textDelta{Type: "text_delta", Text: d.Content})...)
	}
	for i, call := range d.ToolCalls {
		idx := i
		if call.Index != nil {
			idx = *call.Index
		}
		out = append(out, t.toolCall(idx, call)...)
	}
	return out
}

// toolCall records one fragment of a tool call and returns the events it
// produces. Its id, name and arguments may arrive in any order. It becomes the
// current call when no tool-use block is open and its name is known, which
// opens its block; fragments of any other call are kept, not sent, while a
// call is current.
func (t *streamTranslator) toolCall(idx int, call streamToolCall) []event {
	c := t.calls[idx]
	if c == nil {
		c = &toolCall{}
		t.calls[idx] = c
		t.order = append(t.order, idx)
	}
	if call.ID != "" {
		c.id = call.ID
	}
	if call.Function.Name != "" {
		c.name = call.Function.Name
	}
	args := call.Function.Arguments

	var out []event
	// A call whose block was closed when text came between is no more; a
	// later fragment of it is dropped, which keeps the output well-formed.
	if c.sent {
		return nil
	}
	if c.opened {
		// The current call: later fragments go out as they arrive.
		if args != "" {
			out = append(out, t.delta(inputJSONDelta{Type: "input_json_delta", PartialJSON: args})...)
		}
		return out
	}
	if t.blockOpen && t.blockKind == "tool_use" {
		// Another call is current: keep this fragment for the end.
		t.hold(c, args)
		return nil
	}
	if c.name == "" {
		// The name is not known yet: keep everything until it arrives.
		t.hold(c, args)
		return nil
	}
	// The call becomes current: open its block, then send the arguments that
	// arrived before it as one fragment.
	id := c.id
	if id == "" {
		id = "toolu_" + randomHex(12)
	}
	c.id = id
	c.opened = true
	t.toolSent = true
	out = append(out, t.close()...)
	t.blockOpen = true
	t.blockKind = "tool_use"
	t.current = idx
	t.blockIndex = t.nextIndex
	t.nextIndex++
	out = append(out, event{Name: "content_block_start", Data: contentBlockStartEvent{
		Type: "content_block_start", Index: t.blockIndex,
		ContentBlock: toolUseBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  restoreToolName(c.name, t.names),
			Input: json.RawMessage("{}"),
		},
	}})
	if c.args != "" {
		out = append(out, t.delta(inputJSONDelta{Type: "input_json_delta", PartialJSON: c.args})...)
	}
	c.args = ""
	if args != "" {
		out = append(out, t.delta(inputJSONDelta{Type: "input_json_delta", PartialJSON: args})...)
	}
	return out
}

// hold keeps a fragment of a call's arguments until the call can be sent. All
// that is held, of every call together, is bounded by heldLimit.
func (t *streamTranslator) hold(c *toolCall, args string) {
	if t.held+len(args) > t.heldLimit {
		t.overflow = true
		return
	}
	t.held += len(args)
	c.args += args
}

// open closes the open block when the kind differs and opens a block of kind
// "text" or "thinking" when none of that kind is already open. A tool-use
// block that was open is closed and marked spent, so no later fragment of it
// is sent. Opening a tool use is left to toolCall.
func (t *streamTranslator) open(kind string) []event {
	if t.blockOpen && t.blockKind == kind {
		return nil
	}
	out := t.close()
	t.blockOpen = true
	t.blockKind = kind
	t.blockIndex = t.nextIndex
	t.nextIndex++
	body := any(textBlock{Type: "text"})
	if kind == "thinking" {
		body = thinkingBlock{Type: "thinking"}
	}
	return append(out, event{Name: "content_block_start", Data: contentBlockStartEvent{
		Type: "content_block_start", Index: t.blockIndex, ContentBlock: body,
	}})
}

// close closes the open block, if there is one. A tool-use block that is
// closed this way is spent: its call gets no more deltas, and any later
// fragment of it is dropped.
func (t *streamTranslator) close() []event {
	if !t.blockOpen {
		return nil
	}
	if t.blockKind == "tool_use" && t.current >= 0 {
		if c := t.calls[t.current]; c != nil {
			c.sent = true
		}
	}
	t.blockOpen = false
	t.blockKind = ""
	return []event{{Name: "content_block_stop", Data: contentBlockStopEvent{
		Type: "content_block_stop", Index: t.blockIndex,
	}}}
}

// delta wraps a block delta in its event, for the open block.
func (t *streamTranslator) delta(d any) []event {
	if !t.blockOpen {
		return nil
	}
	return []event{{Name: "content_block_delta", Data: contentBlockDeltaEvent{
		Type: "content_block_delta", Index: t.blockIndex, Delta: d,
	}}}
}

// finish closes the open block, emits the tool calls that were held back, and
// sends the message_delta and message_stop. The stop reason is end_turn when
// the provider never named one, but tool_use whenever a tool-use block was
// sent.
func (t *streamTranslator) finish() []event {
	if t.deltaSent {
		return nil
	}
	t.deltaSent = true
	t.done = true
	out := t.close()
	out = append(out, t.flushCalls()...)
	reason := t.stopReason
	if reason == "" {
		reason = "end_turn"
	}
	if t.toolSent && reason == "end_turn" {
		reason = "tool_use"
	}
	out = append(out, event{Name: "message_delta", Data: messageDeltaEvent{
		Type:  "message_delta",
		Delta: messageDeltaBody{StopReason: reason},
		Usage: toAnthropicUsage(t.usage),
	}})
	out = append(out, event{Name: "message_stop", Data: messageStopEvent{Type: "message_stop"}})
	return out
}

// flushCalls emits every tool call that is not the current one and has a name,
// in the order of its index: a content_block_start, one input_json_delta with
// all of its argument text (omitted when there is none), and a
// content_block_stop. A call that never got a name, and the call whose block
// was opened (and then closed) already, are left out.
func (t *streamTranslator) flushCalls() []event {
	idxs := append([]int(nil), t.order...)
	sort.Ints(idxs)
	var out []event
	for _, idx := range idxs {
		c := t.calls[idx]
		if c == nil || c.name == "" || c.opened || c.sent {
			continue
		}
		id := c.id
		if id == "" {
			id = "toolu_" + randomHex(12)
		}
		t.blockIndex = t.nextIndex
		t.nextIndex++
		t.toolSent = true
		out = append(out, event{Name: "content_block_start", Data: contentBlockStartEvent{
			Type: "content_block_start", Index: t.blockIndex,
			ContentBlock: toolUseBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  restoreToolName(c.name, t.names),
				Input: json.RawMessage("{}"),
			},
		}})
		if c.args != "" {
			out = append(out, event{Name: "content_block_delta", Data: contentBlockDeltaEvent{
				Type: "content_block_delta", Index: t.blockIndex,
				Delta: inputJSONDelta{Type: "input_json_delta", PartialJSON: c.args},
			}})
		}
		out = append(out, event{Name: "content_block_stop", Data: contentBlockStopEvent{
			Type: "content_block_stop", Index: t.blockIndex,
		}})
	}
	return out
}

// randomHex returns n random bytes as hex text, or an empty string if the
// system's random source fails, which for an id is not worth failing over.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// streamError builds the one error event sent when the provider fails in the
// middle of a stream. The key, should the provider have echoed it, is replaced
// with [key].
func streamError(message, key string) event {
	return event{Name: "error", Data: errorEvent{
		Type:  "error",
		Error: errorDetail{Type: "api_error", Message: redactText(message, key)},
	}}
}

// redactText removes the key from text that came from the provider.
func redactText(text, key string) string {
	if len(key) < 8 || !strings.Contains(text, key) {
		return text
	}
	return strings.ReplaceAll(text, key, "[key]")
}
