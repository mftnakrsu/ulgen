// Package proxy translates between the Anthropic Messages API, which Claude
// Code speaks, and the OpenAI chat-completions API, which EVREN speaks. It
// holds the EVREN key so that the client never sees it. This file has the two
// wire formats as far as the translator needs them; what is passed through
// untouched stays a json.RawMessage.
package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
)

// anthropicRequest is one Anthropic Messages API request. Only the fields the
// translator reads or forwards are named; every other field is dropped.
type anthropicRequest struct {
	Model         string           `json:"model"`
	System        textOrBlocks     `json:"system"`
	Messages      []anthropicMsg   `json:"messages"`
	MaxTokens     int              `json:"max_tokens"`
	Temperature   *float64         `json:"temperature"`
	TopP          *float64         `json:"top_p"`
	TopK          *float64         `json:"top_k"`
	StopSequences []string         `json:"stop_sequences"`
	Stream        bool             `json:"stream"`
	Tools         []anthropicTool  `json:"tools"`
	ToolChoice    *toolChoiceValue `json:"tool_choice"`
}

// anthropicMsg is one message of an Anthropic request. Content may be a string
// or a list of blocks, so it is decoded into textOrBlocks.
type anthropicMsg struct {
	Role    string       `json:"role"`
	Content textOrBlocks `json:"content"`
}

// anthropicTool is one entry of the request's tools list. InputSchema is kept
// as it arrived; a tool without one is not a function tool.
type anthropicTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// toolChoiceValue is the tool_choice field, which is either a bare string such
// as "auto" or an object such as {"type":"tool","name":"x"}.
type toolChoiceValue struct {
	kind string
	name string
}

// textOrBlocks decodes a field that may be a string or a list of blocks.
type textOrBlocks struct {
	blocks  []contentBlock
	isList  bool
	str     string
	present bool
}

// contentBlock is one block of a message or of a system field. The fields of
// every block type share this struct; a field that does not belong to the
// block's type stays zero.
type contentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text"`

	// image and document
	Source *blockSource `json:"source"`

	// tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`

	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`

	// thinking and redacted_thinking share text/data
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
	Data      string `json:"data"`
}

// blockSource is the source of an image or document block.
type blockSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url"`
}

// openAIRequest is one OpenAI chat-completions request. Fields the design
// forwards are pointers so that an absent field stays absent.
type openAIRequest struct {
	Model        string          `json:"model"`
	Messages     []openAIMessage `json:"messages"`
	MaxTokens    int             `json:"max_tokens,omitempty"`
	Temperature  *float64        `json:"temperature,omitempty"`
	TopP         *float64        `json:"top_p,omitempty"`
	Stop         []string        `json:"stop,omitempty"`
	Tools        []openAITool    `json:"tools,omitempty"`
	ToolChoice   *toolChoiceOut  `json:"tool_choice,omitempty"`
	Stream       bool            `json:"stream,omitempty"`
	StreamOption *streamOptions  `json:"stream_options,omitempty"`
}

// streamOptions asks the provider to send a final chunk that carries usage.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// toolChoiceOut is an OpenAI tool_choice value: a bare string or a function
// reference.
type toolChoiceOut struct {
	str    string
	isFunc bool
	name   string
}

// openAIMessage is one message of an OpenAI request. Content is either a
// string or a list of parts, so it stays a json.RawMessage to keep the shape
// the translator chose.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// openAITool is one function tool of an OpenAI request.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

// openAIFunction is the function body of an OpenAI tool.
type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// openAIToolCall is one tool call of an OpenAI assistant message.
type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAICallFunction `json:"function"`
}

// openAICallFunction is the function body of a tool call: a name and its
// arguments as a JSON string.
type openAICallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// contentPart is one part of an OpenAI content list: a text part or an image
// URL part. A text part always carries its text member (Text has no
// omitempty), because a provider rejects a text part without one; an image
// part carries only its URL. MarshalJSON keeps the two shapes apart.
type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

// partText is the wire form of a text part: it always has a text member.
type partText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// partImage is the wire form of an image part: it carries only the URL.
type partImage struct {
	Type     string        `json:"type"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

// MarshalJSON writes a text part with its text and an image part with its URL.
func (p contentPart) MarshalJSON() ([]byte, error) {
	if p.Type == "text" {
		return json.Marshal(partText{Type: "text", Text: p.Text})
	}
	return json.Marshal(partImage{Type: p.Type, ImageURL: p.ImageURL})
}

// imageURLPart is the image_url body of a content part.
type imageURLPart struct {
	URL string `json:"url"`
}

// nameMap maps a shortened tool name back to the original. A name that was not
// shortened is not in the map.
type nameMap map[string]string

// openAIResponse is one OpenAI chat-completions response.
type openAIResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage"`
}

// openAIChoice is one choice of an OpenAI response.
type openAIChoice struct {
	Message      openAIResponseMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

// openAIResponseMessage is the assistant message of an OpenAI response.
// Content may be a string, null or a list of parts, so it is decoded into
// messageContent.
type openAIResponseMessage struct {
	Content          messageContent       `json:"content"`
	Reasoning        string               `json:"reasoning"`
	ReasoningContent string               `json:"reasoning_content"`
	ToolCalls        []openAIResponseCall `json:"tool_calls"`
}

// messageContent decodes an assistant message's content: a string, null, or a
// list of parts. From a list the text of every part of type text is taken,
// joined in order; an image part of an answer carries no text.
type messageContent struct {
	value string
}

// text returns the message text.
func (c messageContent) text() string { return c.value }

// UnmarshalJSON decodes a string, null or list of parts.
func (c *messageContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &c.value)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return err
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	c.value = b.String()
	return nil
}

// openAIResponseCall is one tool call of an OpenAI response.
type openAIResponseCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAICallFunction `json:"function"`
}

// openAIUsage is the usage block of an OpenAI response.
type openAIUsage struct {
	PromptTokens        int            `json:"prompt_tokens"`
	CompletionTokens    int            `json:"completion_tokens"`
	PromptTokensDetails *promptDetails `json:"prompt_tokens_details"`
}

// promptDetails is the prompt_tokens_details block of an OpenAI usage.
type promptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// anthropicResponse is one Anthropic Messages API response.
type anthropicResponse struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Role         string              `json:"role"`
	Model        string              `json:"model"`
	Content      []anthropicOutBlock `json:"content"`
	StopReason   string              `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        anthropicOutUsage   `json:"usage"`
}

// anthropicOutBlock is one block of an Anthropic response. Signature is a
// pointer so that a thinking block carries an empty signature while other
// blocks leave the field out.
type anthropicOutBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// anthropicOutUsage is the usage block of an Anthropic response.
type anthropicOutUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// UnmarshalJSON decodes a field that may be a string or a list of blocks.
func (t *textOrBlocks) UnmarshalJSON(data []byte) error {
	t.present = true
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		t.isList = false
		return json.Unmarshal(trimmed, &t.str)
	}
	t.isList = true
	return json.Unmarshal(trimmed, &t.blocks)
}

// UnmarshalJSON decodes a tool_choice that may be a string or an object.
func (c *toolChoiceValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &c.kind)
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	c.kind = obj.Type
	c.name = obj.Name
	return nil
}

// MarshalJSON encodes a tool_choice as a bare string or a function object.
func (c toolChoiceOut) MarshalJSON() ([]byte, error) {
	if c.isFunc {
		return json.Marshal(struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}{Type: "function", Function: struct {
			Name string `json:"name"`
		}{Name: c.name}})
	}
	return json.Marshal(c.str)
}
