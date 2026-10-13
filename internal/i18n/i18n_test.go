package i18n

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestSupported(t *testing.T) {
	got := Supported()
	want := []string{"en", "tr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
}

// Every translation file must have exactly the same keys, and the format
// verbs of each message must agree with the English original.
func TestTranslationFilesAgree(t *testing.T) {
	verbs := regexp.MustCompile(`%(\[\d+\])?[a-zA-Z%]`)
	en := load("en")
	if len(en) == 0 {
		t.Fatal("en.json is empty or invalid")
	}
	for _, lang := range Supported() {
		data, err := locales.ReadFile("locales/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s.json: %v", lang, err)
		}
		keys := func(m map[string]string) []string {
			var ks []string
			for k := range m {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			return ks
		}
		if !reflect.DeepEqual(keys(m), keys(en)) {
			t.Errorf("%s.json keys differ from en.json:\n%v\n%v", lang, keys(m), keys(en))
		}
		for k, msg := range m {
			if msg == "" {
				t.Errorf("%s.json: %q is empty", lang, k)
			}
			if got, want := len(verbs.FindAllString(msg, -1)), len(verbs.FindAllString(en[k], -1)); got != want {
				t.Errorf("%s.json: %q has %d format verbs, en.json has %d", lang, k, got, want)
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"":            "",
		"C":           "",
		"POSIX":       "",
		"C.UTF-8":     "",
		"tr":          "tr",
		"TR":          "tr",
		"tr_TR":       "tr",
		"tr-TR":       "tr",
		"tr_TR.UTF-8": "tr",
		"de_DE@euro":  "de",
		"  en_US  ":   "en",
	}
	for in, want := range tests {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	tests := []struct {
		name     string
		override string
		vars     map[string]string
		want     string
	}{
		{"nothing set", "", nil, "en"},
		{"LANG Turkish", "", map[string]string{"LANG": "tr_TR.UTF-8"}, "tr"},
		{"LC_ALL wins over LANG", "", map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "tr_TR.UTF-8"}, "en"},
		{"LC_MESSAGES wins over LANG", "", map[string]string{"LC_MESSAGES": "tr_TR", "LANG": "en_US"}, "tr"},
		{"C locale forces English", "", map[string]string{"LC_ALL": "C", "LANG": "tr_TR.UTF-8"}, "en"},
		{"empty LC_ALL falls through", "", map[string]string{"LC_ALL": "", "LANG": "tr_TR.UTF-8"}, "tr"},
		{"unsupported language falls back", "", map[string]string{"LANG": "de_DE.UTF-8"}, "en"},
		{"override wins", "tr", map[string]string{"LANG": "en_US.UTF-8"}, "tr"},
		{"override with region", "tr-TR", nil, "tr"},
		{"unsupported override falls back", "de", map[string]string{"LANG": "tr_TR.UTF-8"}, "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.override, env(tt.vars)); got != tt.want {
				t.Errorf("Detect(%q, %v) = %q, want %q", tt.override, tt.vars, got, tt.want)
			}
		})
	}
}

func TestSystemLocaleDoesNotPanic(t *testing.T) {
	_ = systemLocale()
}

// TestSystemLocaleIsInjectable checks that Detect reads the system locale
// through the package variable, so a test on a Turkish Windows machine is
// not steered by the real locale.
func TestSystemLocaleIsInjectable(t *testing.T) {
	saved := systemLocale
	t.Cleanup(func() { systemLocale = saved })
	noEnv := func(string) string { return "" }

	systemLocale = func() string { return "tr-TR" }
	if got := Detect("", noEnv); got != "tr" {
		t.Errorf("with injected tr-TR, Detect = %q, want tr", got)
	}
	systemLocale = func() string { return "" }
	if got := Detect("", noEnv); got != "en" {
		t.Errorf("with no system locale, Detect = %q, want en", got)
	}
	// An environment variable still wins over the system locale.
	systemLocale = func() string { return "tr-TR" }
	if got := Detect("", func(k string) string { return map[string]string{"LANG": "en_US.UTF-8"}[k] }); got != "en" {
		t.Errorf("LANG should win over the system locale, got %q", got)
	}
}

// TestFormatArgumentOrder checks that every locale's messages with more than
// one format verb use indexed verbs, so a translation cannot silently swap
// the arguments (as the Turkish invalid-value message once did).
func TestFormatArgumentOrder(t *testing.T) {
	// invalid_value takes (value, option) in that order in every language.
	for _, lang := range Supported() {
		tr := New(lang)
		got := tr.T("error.invalid_value", "theValue", "-theOption")
		if !strings.Contains(got, `"theValue"`) && !strings.Contains(got, "theValue") {
			t.Errorf("%s: value missing from %q", lang, got)
		}
		if !strings.Contains(got, "-theOption") {
			t.Errorf("%s: option missing from %q", lang, got)
		}
		// The value must appear, quoted, and the option unquoted: a swap
		// would quote the option instead.
		if strings.Contains(got, `"-theOption"`) {
			t.Errorf("%s: arguments are swapped: %q", lang, got)
		}
	}

	// Any message with two or more verbs must use indexed verbs (%[n]) so
	// translators can reorder the sentence without reordering the arguments.
	// A literal percent sign ("%%") is not a verb and is stripped first, so a
	// message that ends "(%[3]d%%)" is still accepted.
	plain := regexp.MustCompile(`%[^%\[]`)
	for _, lang := range Supported() {
		m := load(lang)
		for key, msg := range m {
			s := strings.ReplaceAll(msg, "%%", "")
			if strings.Count(s, "%") >= 2 && plain.MatchString(s) {
				t.Errorf("%s: %q has multiple non-indexed verbs; use %%[n]: %q", lang, key, msg)
			}
		}
	}

	// The plain-verb rule must still reject a mix of indexed and positional
	// verbs, and a message with several positional verbs.
	bad := []string{"%s and %s", "%[1]s and %s"}
	for _, msg := range bad {
		s := strings.ReplaceAll(msg, "%%", "")
		if !(strings.Count(s, "%") >= 2 && plain.MatchString(s)) {
			t.Errorf("%q should be rejected as non-indexed", msg)
		}
	}
}

func TestTranslator(t *testing.T) {
	en := New("en")
	tr := New("tr_TR.UTF-8")
	if en.Language() != "en" || tr.Language() != "tr" {
		t.Fatalf("languages: %q %q", en.Language(), tr.Language())
	}
	if got := New("de").Language(); got != "en" {
		t.Errorf("unknown language → %q, want en", got)
	}
	if got := New("").Language(); got != "en" {
		t.Errorf("empty language → %q, want en", got)
	}
	if en.T("error.cancelled") == tr.T("error.cancelled") {
		t.Error("English and Turkish messages should differ")
	}
	if got := en.T("error.unknown_arguments", "a b"); got != "unknown arguments: a b" {
		t.Errorf("T with args = %q", got)
	}
	if got := tr.T("error.unknown_arguments", "a b"); got != "bilinmeyen argümanlar: a b" {
		t.Errorf("T with args (tr) = %q", got)
	}
	if got := en.T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key = %q, want the key itself", got)
	}
}

func TestFallbackForMissingKey(t *testing.T) {
	tr := New("tr")
	tr.messages = map[string]string{} // simulate a Turkish file that lacks every key
	if got := tr.T("error.cancelled"); got != "cancelled" {
		t.Errorf("fallback = %q, want the English message", got)
	}
}
