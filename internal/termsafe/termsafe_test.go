package termsafe

import "testing"

func TestSanitize(t *testing.T) {
	tests := map[string]string{
		"plain text":               "plain text",
		"keep\ttab and\nnewline":   "keep\ttab and\nnewline",
		"red \x1b[31malert\x1b[0m": "red alert",
		"bell\x07 and null\x00":    "bell and null",
		"title\x1b]0;evil\x07end":  "titleend",
		"crlf\r\nline":             "crlf\nline",
		"c1 \u009b31m control":     "c1 31m control",
		"Türkçe ğüşıöç İı":         "Türkçe ğüşıöç İı",
		"lone escape at end \x1b":  "lone escape at end ",
		"cursor \x1b[2J\x1b[Hgone": "cursor gone",
	}
	for in, want := range tests {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
