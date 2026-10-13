package main

import (
	"strings"
)

// This file hides the value of everything that is named like a secret in the
// text of a diff. maskSecrets walks the text once, from left to right, and
// cuts it into tokens: white space, comments, strings, punctuation and bare
// words. Everything that is not changed is copied byte for byte, so a text
// that names no secret comes out as it went in, and the number of line breaks
// never changes: a hidden multi-line string is replaced by "[hidden]" followed
// by as many line breaks as the string held.
//
// Secret names are settings names: a name (without its quotes, in lower case)
// that ends in "key", "keys", "pat", "secret", "secrets", "password",
// "passwords", "passwd", "passphrase", "token", "authorization", "credential",
// "credentials" or "bearer". That takes a few innocent names with it
// ("hotkey", "compat"); hiding one value too many costs nothing. A name
// ending in "tokens" is usually a count of tokens, not a token: strings under
// it are hidden, bare words (numbers) are not. A name ending in "env",
// "envkey", "env_key", "keyenv" or "key_env" holds the name of a variable and
// is not a secret. A quoted name that holds white space is never a secret
// name: a pair such as "Session token": "Oturum belirteci" is interface text,
// not a setting.
//
// A value is hidden when it follows a secret name or stands inside a container
// that a secret name opened: "apiKey": {"a": [1, 2, "V"]} hides 1, 2 and "V".
// An empty string, a reference to an environment variable ({env:NAME}, $NAME,
// ${NAME}) and the bare words true, false and null stay as they are. A string
// or bare word that is copied goes through URL scrubbing (urlScrub): the
// password in "scheme://user:pw@host" and the value of a query parameter named
// like a secret (or "sig"/"signature") become "[hidden]", and whatever the key
// itself looks like (keyLike) becomes "[key]".

// secretName reports whether a name, with its quotes removed and in lower
// case, marks its value as a secret.
func secretName(name string) bool {
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return false // a quoted name with white space is interface text
	}
	for _, suffix := range []string{"env", "envkey", "env_key", "keyenv", "key_env"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	for _, suffix := range []string{
		"key", "keys", "pat", "secret", "secrets", "password", "passwords",
		"passwd", "passphrase", "token", "tokens", "authorization",
		"credential", "credentials", "bearer",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// secretMode says what a container holds: no secret, a secret in any value,
// or a secret in strings only (inside a name ending in "tokens").
type secretMode int

const (
	modePlain secretMode = iota
	modeSecret
	modeStrings
)

// secret reports whether a value of the given kind is hidden in this mode.
func (m secretMode) secret(kind string) bool {
	return m == modeSecret || (m == modeStrings && kind == "str")
}

// modeFrom returns the mode a container opened now should take: secret when
// the name before it marked it (of the given kind, so a container under
// "tokens" hides strings only) or when the container is already inside a
// secret; otherwise plain.
func modeFrom(pending bool, ptokens bool, top secretMode) secretMode {
	if top == modeSecret {
		return modeSecret
	}
	if pending {
		if ptokens {
			return modeStrings
		}
		return modeSecret
	}
	return top
}

// maskSecrets replaces the value of everything that is named like a secret by
// "[hidden]". It is one pass over tokens; see the top of this file.
func maskSecrets(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	var (
		stack       []secretMode
		pending     bool // after a secret name and its separator
		pendTokens  bool // that name ends in "tokens": strings only
		comment     bool // inside /* ... */
		i           int
		lastLineEnd int    // where the last white-space run looked ahead to
		failEnd     [2]int // per quote kind: end of the line of an unclosed string
	)
	for i < len(text) {
		switch c := text[i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			j := i
			for j < len(text) && isSpace(text[j]) {
				j++
			}
			b.WriteString(text[i:j])
			i = j
			lastLineEnd = j
		case comment:
			// Inside /* ... */: mask the run that reaches to the closing */
			// (or to the end of the text), then write the closing */.
			j := strings.Index(text[i:], "*/")
			if j < 0 {
				b.WriteString(maskComment(text[i:]))
				i = len(text)
				continue
			}
			j += i
			b.WriteString(maskComment(text[i:j]))
			b.WriteString("*/")
			i = j + 2
			comment = false
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				b.WriteString(maskComment(text[i:]))
				i = len(text)
			} else {
				j += i
				b.WriteString(maskComment(text[i:j]))
				b.WriteByte('\n')
				i = j + 1
				lastLineEnd = i
			}
		case c == '#':
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				b.WriteString(maskComment(text[i:]))
				i = len(text)
			} else {
				j += i
				b.WriteString(maskComment(text[i:j]))
				b.WriteByte('\n')
				i = j + 1
				lastLineEnd = i
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			comment = true
			b.WriteString("/*")
			i += 2
		case c == '"' || c == '\'':
			q := 0
			if c == '\'' {
				q = 1
			}
			var end int
			ok := false
			if i >= failEnd[q] {
				if end, ok = writeString(text, i); !ok {
					// No closing quote before the end of the line: no later
					// quote of this kind on the line has one either.
					failEnd[q] = len(text)
					if nl := strings.IndexByte(text[i:], '\n'); nl >= 0 {
						failEnd[q] = i + nl
					}
				}
			}
			if !ok {
				b.WriteByte(c)
				i++
				continue
			}
			lit := text[i:end]
			if isName(text, end, &lastLineEnd) {
				b.WriteString(lit)
				pending, pendTokens = setPending(lit)
			} else {
				writeValue(&b, lit, pending, pendTokens, topMode(stack))
				pending = false
			}
			i = end
		case strings.IndexByte("[]{}(),:=", c) >= 0:
			switch c {
			case '[', '{', '(':
				stack = append(stack, modeFrom(pending, pendTokens, topMode(stack)))
				pending = false
			case ']', '}', ')':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				pending = false
			case ',':
				pending = false
			default: // ':' or '='
			}
			b.WriteByte(c)
			i++
		default:
			j := i
			for j < len(text) && isBare(text[j]) {
				j++
			}
			word := text[i:j]
			if isName(text, j, &lastLineEnd) {
				b.WriteString(word)
				pending, pendTokens = setPending(word)
			} else {
				writeValue(&b, word, pending, pendTokens, topMode(stack))
				pending = false
			}
			i = j
		}
	}
	return b.String()
}

// isSpace reports whether c is one of the four white-space bytes: space, tab,
// carriage return and line feed.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// isBare reports whether c can stand in a bare word: not white space, not a
// quote, and not one of the punctuation characters "[]{}(),:=".
func isBare(c byte) bool {
	if isSpace(c) || c == '"' || c == '\'' {
		return false
	}
	return strings.IndexByte("[]{}(),:=", c) < 0
}

// topMode returns the mode of the innermost open container, plain when none is
// open.
func topMode(stack []secretMode) secretMode {
	if len(stack) == 0 {
		return modePlain
	}
	return stack[len(stack)-1]
}

// setPending reports whether a name token (as it stands, with its quotes)
// marks its value as a secret, and whether it ends in "tokens" (strings only).
func setPending(lit string) (pending, tokensOnly bool) {
	name := strings.ToLower(strings.Trim(lit, `"'`))
	if strings.ContainsAny(name, " \t\r\n") {
		return false, false
	}
	return secretName(name), strings.HasSuffix(name, "tokens")
}

// isName reports whether the token that ends at end is a name: whether the
// next character, over any white space (line breaks included), is ":" or "=".
// The run of white space is skipped once and the place it ended is remembered
// (lastLineEnd), so a long run between a name and its separator is not walked
// again for every token. A bare word before the ":" of a TOML time such as
// 07:32:00 is then taken for a name; it is harmless, because it is not a
// secret name.
func isName(text string, end int, lastLineEnd *int) bool {
	k := *lastLineEnd
	if k < end {
		k = end
	}
	for k < len(text) && isSpace(text[k]) {
		k++
	}
	*lastLineEnd = k
	return k < len(text) && (text[k] == ':' || text[k] == '=')
}

// writeString returns the place just past the string token that starts at i,
// and whether a complete string was found. A triple-quoted string ("""..."""
// or ”') may run over several lines and to the end of the text when it is
// never closed. A double- or single-quoted string never holds a line break:
// when the closing quote does not come before the end of the line, ok is false
// and the quote is copied as ordinary punctuation by the caller, which walks
// on after it. The caller writes the token text itself.
func writeString(text string, i int) (end int, ok bool) {
	c := text[i]
	if i+3 <= len(text) && text[i:i+3] == string([]byte{c, c, c}) {
		triple := text[i : i+3]
		for j := i + 3; j < len(text); j++ {
			if c == '"' && text[j] == '\\' {
				j++ // an escaped character, a quote among them, does not end the string
				continue
			}
			if text[j] == c && strings.HasPrefix(text[j:], triple) {
				return j + 3, true
			}
		}
		return len(text), true
	}
	for j := i + 1; j < len(text); j++ {
		if text[j] == '\n' {
			break
		}
		if c == '"' && text[j] == '\\' {
			j++
			continue
		}
		if text[j] == c {
			return j + 1, true
		}
	}
	return i, false
}

// spansNewlines reports whether s holds a line break.
func spansNewlines(s string) bool {
	return strings.IndexByte(s, '\n') >= 0
}

// writeValue writes a string or bare-word token that is not a name. A secret
// value becomes "[hidden]"; a hidden value that runs over several lines keeps
// its line breaks, so the number of lines of the text never changes. A value
// that is not hidden is copied after URL scrubbing. An empty string, a
// reference to an environment variable and the bare words true, false and null
// stay as they are.
func writeValue(b *strings.Builder, tok string, pending, pendTokens bool, top secretMode) {
	kind := "str"
	inner := unquote(tok)
	if tok == "" || (tok[0] != '"' && tok[0] != '\'') {
		kind = "bare"
		inner = tok
	}
	hide := pending && (kind == "str" || !pendTokens) || top.secret(kind)
	if hide {
		if kind == "str" && inner == "" {
			b.WriteString(tok)
			return
		}
		if kind == "bare" && (inner == "true" || inner == "false" || inner == "null") {
			b.WriteString(tok)
			return
		}
		if envReference(inner) {
			b.WriteString(tok)
			return
		}
		b.WriteString("[hidden]")
		if spansNewlines(tok) {
			b.WriteString(strings.Repeat("\n", strings.Count(tok, "\n")))
		}
		return
	}
	b.WriteString(urlScrub(tok))
}

// maskComment hides the value of a secret name inside one comment. One rule
// applies on the comment's own text: after a word that is a secret name,
// optional spaces and ":" or "=", the rest of that line of the comment is
// replaced by "[hidden]" (so "# api_key = abc 123" becomes
// "# api_key = [hidden]"): a line that was commented out can hold a value with
// spaces in it. Nothing else in a comment is changed, and a comment never
// affects the line after it.
func maskComment(s string) string {
	if !strings.ContainsAny(s, ":=") {
		return urlScrub(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if isSpace(s[i]) {
			b.WriteByte(s[i])
			i++
			continue
		}
		start := i
		for i < len(s) && !isSpace(s[i]) && s[i] != ':' && s[i] != '=' {
			i++
		}
		if i == start { // a separator with no word before it
			b.WriteByte(s[i])
			i++
			continue
		}
		word := s[start:i]
		k := i
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if !secretName(strings.ToLower(strings.Trim(word, `"'`))) || k >= len(s) || (s[k] != ':' && s[k] != '=') {
			b.WriteString(urlScrub(word))
			continue
		}
		j := k + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		end := j
		for end < len(s) && s[end] != '\n' && s[end] != '\r' {
			end++
		}
		b.WriteString(urlScrub(word))
		if end == j {
			continue
		}
		b.WriteString(s[i:j])
		b.WriteString("[hidden]")
		i = end
	}
	return b.String()
}

// unquote returns the text inside a string token, without its quotes.
func unquote(lit string) string {
	if len(lit) >= 6 && (strings.HasPrefix(lit, `"""`) || strings.HasPrefix(lit, `'''`)) {
		return lit[3 : len(lit)-3]
	}
	if len(lit) >= 2 {
		return lit[1 : len(lit)-1]
	}
	return lit
}

// envReference reports whether a whole value names an environment variable
// rather than holding a secret: "{env:NAME}", "$NAME" or "${NAME}", where
// NAME is written the way variable names are (an upper-case letter or "_",
// then upper-case letters, digits or "_"). A value that only begins with "$"
// is not a reference: "$ecret1" is hidden, "$OPENAI_API_KEY" stays.
func envReference(value string) bool {
	if strings.HasPrefix(value, "{env:") && strings.HasSuffix(value, "}") {
		return isVarname(value[5 : len(value)-1])
	}
	if !strings.HasPrefix(value, "$") {
		return false
	}
	rest := value[1:]
	if strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}") {
		rest = rest[1 : len(rest)-1]
	}
	return isVarname(rest)
}

// isVarname reports whether name is written the way an environment variable
// name is: an upper-case letter or "_", then upper-case letters, digits or
// "_".
func isVarname(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z'):
			continue
		case i > 0 && r >= '0' && r <= '9':
			continue
		}
		return false
	}
	return true
}

// urlScrub hides the secrets of a URL token that is copied: the password in
// "scheme://user:password@host" and the value of a query parameter named like
// a secret (or "sig"/"signature"). Then whatever the key itself looks like is
// replaced by "[key]", here and in every other token that is copied.
func urlScrub(tok string) string {
	tok = urlScrubAuth(tok)
	tok = urlScrubQuery(tok)
	return keyLike.ReplaceAllString(tok, "[key]")
}

// urlScrubAuth replaces the "user:password" before the "@" of every URL in a
// token by "[hidden]", so "https://u:pw@host" becomes "https://[hidden]@host".
// A bare user name stays, and so does a host with a port whose path happens
// to hold an "@" ("https://host:443/users/@me").
func urlScrubAuth(tok string) string {
	if !strings.Contains(tok, "://") {
		return tok
	}
	var b strings.Builder
	b.Grow(len(tok))
	for {
		scheme := strings.Index(tok, "://")
		if scheme < 0 {
			break
		}
		b.WriteString(tok[:scheme+3])
		tok = tok[scheme+3:]
		// The user and password stand before the first "@" that comes
		// before any "/", "?", "#", quote or white space.
		end := 0
		for end < len(tok) && tok[end] != '@' && tok[end] != '/' && tok[end] != '?' && !isQueryEnd(tok[end]) {
			end++
		}
		if end < len(tok) && tok[end] == '@' && strings.Contains(tok[:end], ":") {
			b.WriteString("[hidden]")
			tok = tok[end:]
		}
	}
	b.WriteString(tok)
	return b.String()
}

// urlScrubQuery replaces the value of every query parameter of a copied token
// whose name is a secret name, or "sig" or "signature":
// "?token=abc&a=1" becomes "?token=[hidden]&a=1". The value ends at "&", "#",
// a quote, white space or the end of the token.
func urlScrubQuery(tok string) string {
	q := strings.IndexByte(tok, '?')
	if q < 0 {
		return tok
	}
	var b strings.Builder
	b.Grow(len(tok))
	b.WriteString(tok[:q+1])
	for i := q + 1; i < len(tok); {
		// A parameter starts here: its name runs to "=" or to the end.
		j := i
		for j < len(tok) && tok[j] != '=' && tok[j] != '?' && !isQueryEnd(tok[j]) {
			j++
		}
		name := strings.ToLower(tok[i:j])
		b.WriteString(tok[i:j])
		if j < len(tok) && tok[j] == '=' {
			b.WriteByte('=')
			j++
			k := j
			for k < len(tok) && tok[k] != '?' && !isQueryEnd(tok[k]) {
				k++
			}
			if name == "sig" || name == "signature" || secretName(name) {
				b.WriteString("[hidden]")
			} else {
				b.WriteString(tok[j:k])
			}
			j = k
		}
		if j < len(tok) {
			b.WriteByte(tok[j]) // "&", "#", "?", a quote or white space
			j++
			if tok[j-1] == '#' {
				b.WriteString(tok[j:])
				break
			}
		}
		i = j
	}
	return b.String()
}

// isQueryEnd reports whether c ends a query parameter's name or value.
func isQueryEnd(c byte) bool {
	return c == '&' || c == '#' || c == '"' || c == '\'' || isSpace(c)
}
