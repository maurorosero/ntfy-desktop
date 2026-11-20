// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"regexp"
	"strings"
)

// Messages of the parser. gettext msgids, like the ones in config.go.
const (
	msgAssignment   = "expected key = value"
	msgKey          = "invalid key"
	msgQuoted       = "value must be a quoted string"
	msgTrailing     = "unexpected text after the closing quote"
	msgEscape       = "invalid escape sequence"
	msgUnterminated = "unterminated quoted value"
	msgDuplicate    = "duplicate key"
	msgUnsupported  = "unsupported key"
)

// Keys are TOML bare keys, which is all this file uses.
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parse turns the file content into a validated Config. It reads only the
// supported subset: comments, blank lines and one `key = "value"` per line.
func parse(data []byte) (*Config, error) {
	values := make(map[string]string, 4)
	for i, raw := range strings.Split(string(data), "\n") {
		line := i + 1
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		key, value, err := assignment(text, line)
		if err != nil {
			return nil, err
		}
		switch key {
		case keyServer, keyTopic, keyUser, keyPassword:
		default:
			return nil, &SyntaxError{Line: line, Key: key, Msg: msgUnsupported}
		}
		if _, dup := values[key]; dup {
			return nil, &SyntaxError{Line: line, Key: key, Msg: msgDuplicate}
		}
		values[key] = value
	}

	cfg := &Config{
		ServerURL: values[keyServer],
		Topic:     values[keyTopic],
		User:      values[keyUser],
		Password:  values[keyPassword],
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// assignment splits `key = "value"` into its two halves.
func assignment(text string, line int) (key, value string, err error) {
	i := strings.IndexByte(text, '=')
	if i <= 0 {
		return "", "", &SyntaxError{Line: line, Msg: msgAssignment}
	}
	key = strings.TrimSpace(text[:i])
	if !keyPattern.MatchString(key) {
		return "", "", &SyntaxError{Line: line, Msg: msgKey}
	}
	value, err = quoted(strings.TrimSpace(text[i+1:]), line)
	if err != nil {
		return "", "", err
	}
	return key, value, nil
}

// quoted reads a double-quoted string with the escapes TOML defines for the
// characters a URL, a topic or a password can need.
func quoted(text string, line int) (string, error) {
	if text == "" || text[0] != '"' {
		return "", &SyntaxError{Line: line, Msg: msgQuoted}
	}

	var b strings.Builder
	for i := 1; i < len(text); i++ {
		switch c := text[i]; c {
		case '"':
			// Only a comment may follow the closing quote.
			rest := strings.TrimSpace(text[i+1:])
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return "", &SyntaxError{Line: line, Msg: msgTrailing}
			}
			return b.String(), nil
		case '\\':
			i++
			if i >= len(text) {
				return "", &SyntaxError{Line: line, Msg: msgEscape}
			}
			switch text[i] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				return "", &SyntaxError{Line: line, Msg: msgEscape}
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", &SyntaxError{Line: line, Msg: msgUnterminated}
}
