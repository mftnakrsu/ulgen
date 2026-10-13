// Package i18n selects the interface language and translates user-facing
// strings. Every string a user sees comes from a translation file in
// locales/, so the interface ships in Turkish and English from one binary.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var locales embed.FS

// Fallback is the language used when the requested one is not available.
const Fallback = "en"

var (
	supportedOnce sync.Once
	supportedList []string
)

// Supported returns the language codes that have a translation file, sorted.
func Supported() []string {
	supportedOnce.Do(func() {
		entries, err := locales.ReadDir("locales")
		if err != nil {
			return
		}
		for _, e := range entries {
			if name, ok := strings.CutSuffix(e.Name(), ".json"); ok {
				supportedList = append(supportedList, name)
			}
		}
		sort.Strings(supportedList)
	})
	return append([]string(nil), supportedList...)
}

func isSupported(lang string) bool {
	for _, s := range Supported() {
		if s == lang {
			return true
		}
	}
	return false
}

// Normalize reduces a locale tag such as "tr_TR.UTF-8", "tr-TR" or "TR" to a
// language code ("tr"). It returns "" for an empty tag and for the C and
// POSIX locales, which carry no language.
func Normalize(tag string) string {
	tag = strings.TrimSpace(tag)
	if i := strings.IndexAny(tag, ".@"); i >= 0 {
		tag = tag[:i] // encoding and modifier
	}
	if i := strings.IndexAny(tag, "_-"); i >= 0 {
		tag = tag[:i] // region
	}
	tag = strings.ToLower(tag)
	switch tag {
	case "", "c", "posix":
		return ""
	}
	return tag
}

// systemLocale reads the operating system's user-interface language. It is a
// variable so tests can replace it; otherwise environment variables faked for
// a test would be overridden by the real machine's locale on Windows.
var systemLocale = defaultSystemLocale

// Detect chooses the interface language. The order is: the override from
// the configuration, then the LC_ALL, LC_MESSAGES and LANG environment
// variables, then the operating system's user-interface language (Windows),
// then English. The first source that is set decides: a value naming a
// language yields that language (or English when it has no translation
// file), and the C or POSIX locale yields English rather than falling
// through to the next source.
func Detect(override string, getenv func(string) string) string {
	if override != "" {
		return pick(Normalize(override))
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if raw := strings.TrimSpace(getenv(name)); raw != "" {
			return pick(Normalize(raw))
		}
	}
	if raw := strings.TrimSpace(systemLocale()); raw != "" {
		return pick(Normalize(raw))
	}
	return Fallback
}

func pick(lang string) string {
	if isSupported(lang) {
		return lang
	}
	return Fallback
}

// Translator translates message keys for one language, falling back to
// English for keys the language lacks.
type Translator struct {
	lang     string
	messages map[string]string
	fallback map[string]string
}

// New returns a translator for lang. Tags such as "tr_TR.UTF-8" are accepted;
// an unknown or empty language yields English.
func New(lang string) *Translator {
	lang = pick(Normalize(lang))
	fallback := load(Fallback)
	t := &Translator{lang: lang, messages: fallback, fallback: fallback}
	if lang != Fallback {
		t.messages = load(lang)
	}
	return t
}

func load(lang string) map[string]string {
	data, err := locales.ReadFile("locales/" + lang + ".json")
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return map[string]string{}
	}
	return m
}

// Language returns the language in use, for example "tr" or "en".
func (t *Translator) Language() string { return t.lang }

// T returns the message for key, formatted with args by the rules of
// fmt.Sprintf. A key missing from every translation file is returned as is,
// so a forgotten string is visible but never a crash.
func (t *Translator) T(key string, args ...any) string {
	msg, ok := t.messages[key]
	if !ok {
		msg, ok = t.fallback[key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
