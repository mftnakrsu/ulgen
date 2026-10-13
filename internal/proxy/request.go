package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// billingHeader is the marker Claude Code puts at the start of a system line.
// The line changes on every request, which defeats EVREN's cache of the
// prompt's beginning, so it is removed before the text is sent.
const billingHeader = "x-anthropic-billing-header:"

// toOpenAI translates an Anthropic request into an OpenAI chat-completions
// request for model. It returns the request and the map that restores tool
// names shortened to 64 characters. An error is returned for a tool_choice
// whose type is not known.
func toOpenAI(req anthropicRequest, model string) (openAIRequest, nameMap, error) {
	names := nameMap{}
	out := openAIRequest{
		Model:       model,
		Messages:    []openAIMessage{},
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}
	if len(req.StopSequences) > 0 {
		out.Stop = req.StopSequences
	}
	if req.Stream {
		out.StreamOption = &streamOptions{IncludeUsage: true}
	}

	if sys := systemText(req.System); sys != "" {
		out.Messages = append(out.Messages, openAIMessage{
			Role:    "system",
			Content: mustJSON(sys),
		})
	}

	for _, msg := range foldSystemMessages(req.Messages) {
		msgs, err := translateMessage(msg, names)
		if err != nil {
			return openAIRequest{}, nil, err
		}
		out.Messages = append(out.Messages, msgs...)
	}

	out.Tools = toOpenAITools(req.Tools, names)

	// tool_choice is only meaningful when a function tool is sent: a
	// provider rejects "required" or a function form with no tools.
	if len(out.Tools) > 0 {
		choice, err := toOpenAIToolChoice(req.ToolChoice, names)
		if err != nil {
			return openAIRequest{}, nil, err
		}
		out.ToolChoice = choice
	}

	return out, names, nil
}

// systemText joins the request's system text blocks with a blank line and
// removes the billing-header lines. A string system is used as it is. A part
// that is empty after the billing-header lines are removed is left out, and
// the joined text has no leading or trailing blank lines.
func systemText(sb textOrBlocks) string {
	if !sb.isList {
		return strings.Trim(stripBillingHeader(sb.str), "\n")
	}
	var parts []string
	for _, b := range sb.blocks {
		if b.Type != "text" {
			continue
		}
		if s := strings.Trim(stripBillingHeader(b.Text), "\n"); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// stripBillingHeader removes every line that begins with the billing-header
// marker, after leading spaces or tabs, and joins the rest with a newline.
// Line ends may be \n or \r\n.
func stripBillingHeader(s string) string {
	if !strings.Contains(s, billingHeader) {
		return s
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), billingHeader) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// translateMessage turns one Anthropic message into one or more OpenAI
// messages. A user turn carrying tool results yields the tool messages first,
// then the user's text and images. An assistant turn with text and tool uses
// becomes one message. A message that keeps nothing produces nothing. A
// system message never arrives here: foldSystemMessages has moved its text
// into a user turn.
func translateMessage(msg anthropicMsg, names nameMap) ([]openAIMessage, error) {
	switch msg.Role {
	case "assistant":
		m, ok := translateAssistant(msg.Content, names)
		if !ok {
			return nil, nil
		}
		return []openAIMessage{m}, nil
	case "user":
		return translateUser(msg.Content), nil
	default:
		return nil, nil
	}
}

// The tags around the text of a system message that travels in a user turn.
// They are the ones Claude Code itself puts around a note of the system
// inside a user turn, and its prompt tells the model what they mean.
const (
	systemNoteOpen  = "<system-reminder>\n"
	systemNoteClose = "\n</system-reminder>"
)

// foldSystemMessages returns msgs with every system message's text moved into
// a user turn, as a text block inside the tags above. Claude Code sends system
// messages between the turns to a model it does not know: the first one,
// after the first user turn, names the working directory, the platform and
// the date, and without it the model has to guess where it is. EVREN's models
// do not all take a system message in that place (one refuses the request,
// one ignores the message), but every one of them reads the text in a user
// turn. The text joins the user turn before it; with none before it, the one
// after it; with neither, it becomes a user turn of its own. Joining a turn,
// not standing beside it, keeps the tool messages of that turn right after
// the tool calls they answer. An empty system message leaves nothing. msgs
// and the content it holds are not changed.
func foldSystemMessages(msgs []anthropicMsg) []anthropicMsg {
	found := false
	for _, msg := range msgs {
		if msg.Role == "system" {
			found = true
			break
		}
	}
	if !found {
		return msgs
	}
	out := make([]anthropicMsg, 0, len(msgs))
	var waiting []contentBlock // notes with no user turn before them
	for _, msg := range msgs {
		if msg.Role == "system" {
			text := systemText(msg.Content)
			if text == "" {
				continue
			}
			note := contentBlock{Type: "text", Text: systemNoteOpen + text + systemNoteClose}
			if n := len(out); n > 0 && out[n-1].Role == "user" {
				out[n-1].Content = blockContent(contentBlocks(out[n-1].Content), []contentBlock{note})
			} else {
				waiting = append(waiting, note)
			}
			continue
		}
		if len(waiting) > 0 {
			if msg.Role == "user" {
				msg.Content = blockContent(waiting, contentBlocks(msg.Content))
			} else {
				out = append(out, anthropicMsg{Role: "user", Content: blockContent(waiting, nil)})
			}
			waiting = nil
		}
		out = append(out, msg)
	}
	if len(waiting) > 0 {
		out = append(out, anthropicMsg{Role: "user", Content: blockContent(waiting, nil)})
	}
	return out
}

// contentBlocks returns a message's content as blocks: a string becomes one
// text block, an empty string none.
func contentBlocks(content textOrBlocks) []contentBlock {
	if content.isList {
		return content.blocks
	}
	if content.str == "" {
		return nil
	}
	return []contentBlock{{Type: "text", Text: content.str}}
}

// blockContent returns list content that holds first and then second, in a
// slice of its own.
func blockContent(first, second []contentBlock) textOrBlocks {
	blocks := make([]contentBlock, 0, len(first)+len(second))
	blocks = append(blocks, first...)
	blocks = append(blocks, second...)
	return textOrBlocks{blocks: blocks, isList: true, present: true}
}

// translateUser translates a user turn. Tool results come first, then one user
// message with the collected text and image parts.
func translateUser(content textOrBlocks) []openAIMessage {
	var out []openAIMessage
	if !content.isList {
		if content.str != "" {
			out = append(out, openAIMessage{Role: "user", Content: mustJSON(content.str)})
		}
		return out
	}

	var parts []contentPart
	for _, b := range content.blocks {
		switch b.Type {
		case "tool_result":
			out = append(out, openAIMessage{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    toolResultContent(b.Content, b.IsError),
			})
		case "text":
			parts = appendTextPart(parts, b.Text)
		case "image":
			if url, ok := imageURL(b.Source); ok {
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURLPart{URL: url}})
			}
		}
	}
	if len(parts) > 0 {
		out = append(out, openAIMessage{Role: "user", Content: mustJSON(parts)})
	}
	return out
}

// toolResultContent builds the content of a tool message from a tool_result
// block. A string stays a string; a list becomes a list of text and image
// parts, or collapses to a single string when it holds one text part. An
// is_error result has "Error: " put in front of its content.
func toolResultContent(raw json.RawMessage, isError bool) json.RawMessage {
	prefix := ""
	if isError {
		prefix = "Error: "
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return mustJSON(prefix)
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return mustJSON(prefix)
		}
		return mustJSON(prefix + s)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return mustJSON(prefix)
	}
	var parts []contentPart
	for _, it := range items {
		var s string
		if err := json.Unmarshal(it, &s); err == nil {
			parts = appendTextPart(parts, s)
			continue
		}
		var obj struct {
			Type   string       `json:"type"`
			Text   string       `json:"text"`
			Source *blockSource `json:"source"`
		}
		if err := json.Unmarshal(it, &obj); err != nil {
			continue
		}
		switch obj.Type {
		case "text":
			parts = appendTextPart(parts, obj.Text)
		case "image":
			if url, ok := imageURL(obj.Source); ok {
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURLPart{URL: url}})
			}
		}
	}
	if len(parts) == 0 {
		return mustJSON(prefix)
	}
	if len(parts) == 1 && parts[0].Type == "text" {
		return mustJSON(prefix + parts[0].Text)
	}
	if prefix != "" {
		parts = append([]contentPart{{Type: "text", Text: prefix}}, parts...)
	}
	return mustJSON(parts)
}

// appendTextPart adds a text part to parts unless it is exactly empty: an
// empty text block carries nothing and a provider rejects a text part without
// text.
func appendTextPart(parts []contentPart, text string) []contentPart {
	if text == "" {
		return parts
	}
	return append(parts, contentPart{Type: "text", Text: text})
}

// imageURL turns an image or document source into the URL an OpenAI image part
// needs: a data: URL for base64, the URL itself for a URL source.
func imageURL(src *blockSource) (string, bool) {
	if src == nil {
		return "", false
	}
	switch src.Type {
	case "url":
		if src.URL == "" {
			return "", false
		}
		return src.URL, true
	case "base64":
		return "data:" + src.MediaType + ";base64," + src.Data, true
	default:
		return "", false
	}
}

// translateAssistant translates an assistant turn. Text blocks are joined into
// one string; tool_use blocks become tool calls. Thinking blocks of an earlier
// turn are dropped. The message is kept only if it has text or tool calls.
func translateAssistant(content textOrBlocks, names nameMap) (openAIMessage, bool) {
	m := openAIMessage{Role: "assistant"}
	if !content.isList {
		if content.str == "" {
			return m, false
		}
		m.Content = mustJSON(content.str)
		return m, true
	}

	var text strings.Builder
	for _, b := range content.blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			args := b.Input
			if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
				args = json.RawMessage("{}")
			}
			m.ToolCalls = append(m.ToolCalls, openAIToolCall{
				ID:   b.ID,
				Type: "function",
				Function: openAICallFunction{
					Name:      shortenToolName(b.Name, names),
					Arguments: string(args),
				},
			})
		}
	}
	if text.Len() > 0 {
		m.Content = mustJSON(text.String())
	}
	if len(m.Content) == 0 && len(m.ToolCalls) == 0 {
		return m, false
	}
	return m, true
}

// toOpenAITools keeps the tools that carry an input_schema and turns them into
// function tools. Any other tool, and a tool with no name, is left out.
func toOpenAITools(tools []anthropicTool, names nameMap) []openAITool {
	var out []openAITool
	for _, t := range tools {
		trimmed := bytes.TrimSpace(t.InputSchema)
		if len(trimmed) == 0 || string(trimmed) == "null" {
			continue
		}
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        shortenToolName(t.Name, names),
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// shortenToolName shortens a name longer than 64 characters to at most 55
// bytes that end on a complete character, an underscore and the first 8 hex
// digits of its SHA-256, and records the original so the answer can restore
// it. The cut is at a rune boundary so a multi-byte name never ends in half a
// character.
func shortenToolName(name string, names nameMap) string {
	if len(name) <= 64 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	short := name[:runeBoundary(name, 55)] + "_" + hex.EncodeToString(sum[:4])
	names[short] = name
	return short
}

// runeBoundary returns the largest i at most max that starts a character in
// s: the cut never splits a multi-byte character.
func runeBoundary(s string, max int) int {
	if max >= len(s) {
		return len(s)
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// toOpenAIToolChoice translates the request's tool_choice. A missing one stays
// missing.
func toOpenAIToolChoice(v *toolChoiceValue, names nameMap) (*toolChoiceOut, error) {
	if v == nil {
		return nil, nil
	}
	switch v.kind {
	case "auto":
		return &toolChoiceOut{str: "auto"}, nil
	case "any":
		return &toolChoiceOut{str: "required"}, nil
	case "none":
		return &toolChoiceOut{str: "none"}, nil
	case "tool":
		return &toolChoiceOut{isFunc: true, name: shortenToolName(v.name, names)}, nil
	default:
		return nil, fmt.Errorf("proxy: unknown tool_choice type %q", v.kind)
	}
}

// mustJSON encodes a value that is known to encode.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
