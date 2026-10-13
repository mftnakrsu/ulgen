package proxy

import "testing"

// TestPickModel covers the design's "Model" rule.
func TestPickModel(t *testing.T) {
	models := []string{"evren-large", "deepseek-v4-flash", "mid-model"}
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"offered model is used as it is", "evren-large", "evren-large"},
		{"a haiku name becomes the small model", "claude-3-5-haiku-20241022", "deepseek-v4-flash"},
		{"haiku anywhere in the name counts", "my-haiku-model", "deepseek-v4-flash"},
		{"any other name becomes the default", "claude-opus-4", "evren-large"},
		{"an empty name becomes the default", "", "evren-large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickModel(tt.requested, models, "evren-large", "deepseek-v4-flash"); got != tt.want {
				t.Fatalf("pickModel(%q) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}
