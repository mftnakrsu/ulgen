package proxy

import "bytes"

// estimateTokens estimates the input tokens of a request: the UTF-8 bytes of
// the system text, the message texts, the tool definitions and the tool
// results, divided by 3 and rounded up. EVREN has no counting endpoint, and
// the estimate errs on the high side so that a client compacts early rather
// than late.
func estimateTokens(req anthropicRequest) int {
	n := 0
	n += len(systemText(req.System))
	for _, msg := range req.Messages {
		n += messageBytes(msg.Content)
	}
	for _, t := range req.Tools {
		n += len(t.Name) + len(t.Description)
		n += len(bytes.TrimSpace(t.InputSchema))
	}
	if n == 0 {
		return 0
	}
	return (n + 2) / 3
}

// messageBytes counts the text bytes of one message, including the text of its
// tool results.
func messageBytes(content textOrBlocks) int {
	if !content.isList {
		return len(content.str)
	}
	n := 0
	for _, b := range content.blocks {
		switch b.Type {
		case "text":
			n += len(b.Text)
		case "thinking":
			n += len(b.Thinking)
		case "tool_result":
			n += len(toolResultContent(b.Content, b.IsError))
		}
	}
	return n
}
