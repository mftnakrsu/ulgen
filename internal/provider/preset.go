package provider

// Preset describes an API endpoint as data.
type Preset struct {
	ID      string
	Name    string
	BaseURL string // the /v1 root, without a trailing slash
	// KeyEnv is the environment variable that holds the API key.
	KeyEnv       string
	DefaultModel string
}

// EVREN is the preset for Türkiye's EVREN LLM gateway (docs/evren.md). The
// key travels as a Bearer token.
var EVREN = Preset{
	ID:           "evren",
	Name:         "EVREN",
	BaseURL:      "https://evren-llmapi.ssyz.org.tr/v1",
	KeyEnv:       "EVREN_API_KEY",
	DefaultModel: "deepseek-v4.1-flash",
}
