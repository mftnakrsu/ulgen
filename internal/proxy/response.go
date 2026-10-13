package proxy

import (
	"bytes"
	"encoding/json"
)

// fromOpenAI translates an OpenAI chat-completions response into an Anthropic
// Messages response. Content blocks come in the order thinking, text, tool
// uses; tool names shortened on the way out are restored, and the response
// names the model that was used.
func fromOpenAI(resp openAIResponse, model string, names nameMap) anthropicResponse {
	out := anthropicResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    []anthropicOutBlock{},
		StopReason: "end_turn",
	}

	var choice openAIChoice
	if len(resp.Choices) > 0 {
		choice = resp.Choices[0]
	}

	if text := reasoningText(choice.Message); text != "" {
		empty := ""
		out.Content = append(out.Content, anthropicOutBlock{Type: "thinking", Thinking: text, Signature: &empty})
	}
	if text := choice.Message.Content.text(); text != "" {
		out.Content = append(out.Content, anthropicOutBlock{Type: "text", Text: text})
	}
	for _, call := range choice.Message.ToolCalls {
		out.Content = append(out.Content, anthropicOutBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  restoreToolName(call.Function.Name, names),
			Input: parseArguments(call.Function.Arguments),
		})
	}

	out.StopReason = stopReason(choice.FinishReason)
	// A "stop" (or unknown) finish reason with tool calls is really a tool
	// use: the model stopped to call a tool.
	if out.StopReason == "end_turn" && len(choice.Message.ToolCalls) > 0 {
		out.StopReason = "tool_use"
	}
	out.Usage = toAnthropicUsage(resp.Usage)
	return out
}

// reasoningText returns the reasoning the provider sent, reading the
// reasoning_content field as well as reasoning.
func reasoningText(m openAIResponseMessage) string {
	if m.Reasoning != "" {
		return m.Reasoning
	}
	return m.ReasoningContent
}

// restoreToolName maps a shortened tool name back to the original; a name that
// was not shortened comes back as it is.
func restoreToolName(name string, names nameMap) string {
	if orig, ok := names[name]; ok {
		return orig
	}
	return name
}

// parseArguments turns a tool call's arguments into a JSON object for the
// input field. Arguments that are not a valid JSON object become {}.
func parseArguments(args string) json.RawMessage {
	trimmed := bytes.TrimSpace([]byte(args))
	if len(trimmed) == 0 {
		return json.RawMessage("{}")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(trimmed)
}

// stopReason maps an OpenAI finish reason to an Anthropic stop reason.
func stopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

// toAnthropicUsage maps an OpenAI usage block to an Anthropic one. The cached
// part of the prompt is read out of input_tokens; there is no cache creation.
func toAnthropicUsage(u *openAIUsage) anthropicOutUsage {
	if u == nil {
		return anthropicOutUsage{}
	}
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	input := u.PromptTokens - cached
	if input < 0 {
		input = 0
	}
	return anthropicOutUsage{
		InputTokens:          input,
		OutputTokens:         u.CompletionTokens,
		CacheReadInputTokens: cached,
	}
}

// errorKind maps an HTTP status from EVREN to the Anthropic error type that
// describes it.
func errorKind(status int) string {
	switch status {
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 404:
		return "not_found_error"
	case 413:
		return "request_too_large"
	case 429:
		return "rate_limit_error"
	case 503, 529:
		return "overloaded_error"
	}
	if status >= 400 && status < 500 {
		return "invalid_request_error"
	}
	return "api_error"
}

// errorBody builds an Anthropic error body of the given kind and message.
func errorBody(kind, message string) []byte {
	body, err := json.Marshal(struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		Type: "error",
		Error: struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}{Type: kind, Message: message},
	})
	if err != nil {
		return []byte(`{"type":"error","error":{"type":"api_error","message":""}}`)
	}
	return body
}
