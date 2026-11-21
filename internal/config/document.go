// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Messages of the edits. gettext msgids, like the ones in toml.go and
// config.go.
const (
	msgOnlyTopic      = "cannot remove the only topic"
	msgDuplicateTopic = "%q is already in the topic list"
	msgMissingTopic   = "%q is not in the topic list"
	msgNoValue        = "expected a quoted value on this line"
)

// Document is the text of a configuration file, kept line by line so an edit
// rewrites the line it changes and leaves every other one byte for byte,
// comments and order included.
type Document struct {
	lines []string
	// trailingNewline remembers whether the original text ended with one, so
	// Bytes gives back the same file it was built from.
	trailingNewline bool
}

// NewDocument returns the document that holds data, without parsing it.
func NewDocument(data []byte) *Document {
	text := string(data)
	trailing := strings.HasSuffix(text, "\n")
	if trailing {
		text = strings.TrimSuffix(text, "\n")
	}
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	return &Document{lines: lines, trailingNewline: trailing}
}

// Bytes returns the text of the document.
func (d *Document) Bytes() []byte {
	text := strings.Join(d.lines, "\n")
	if d.trailingNewline {
		text += "\n"
	}
	return []byte(text)
}

// Masked returns the text of the document with the value of the password key
// replaced by placeholder, so a front end can show the whole file without the
// secret. The rest of that line —the indentation and the comment behind the
// value— and every other line stay exactly as they are.
func (d *Document) Masked(placeholder string) []byte {
	index, ok := d.keyLine(keyPassword)
	if !ok {
		return d.Bytes()
	}
	raw := d.lines[index]
	equal := strings.IndexByte(raw, '=')
	if equal < 0 {
		return d.Bytes()
	}
	open := strings.IndexByte(raw[equal:], '"')
	if open < 0 {
		return d.Bytes()
	}
	open += equal
	close, ok := closingQuote(raw, open)
	if !ok {
		return d.Bytes()
	}
	masked := d.clone()
	masked.lines[index] = raw[:open+1] + placeholder + raw[close:]
	return masked.Bytes()
}

// Check parses and validates the document with the same loader that reads the
// file from disk. When the loader rejects a value, the error names the line
// the key sits on as well as the key.
func (d *Document) Check() (*Config, error) {
	cfg, err := parse(d.Bytes())
	if err != nil {
		return nil, d.blame(err)
	}
	return cfg, nil
}

// LoadDocument reads the file at path into an editable document. A file the
// loader does not accept is refused: an invalid file is never the base of an
// edit that would overwrite it.
func LoadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	doc := NewDocument(data)
	if _, err := doc.Check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// Set changes the value of key, or appends the assignment when the file does
// not have that key yet. Only the affected line changes; the comment behind
// the value stays where it was. The result is checked by Check or, at the
// latest, when it is written.
func (d *Document) Set(key, value string) error {
	if !supportedKey(key) {
		return &EditError{Key: key, Msg: msgUnsupported}
	}
	next := d.clone()
	if line, ok := next.keyLine(key); ok {
		if err := next.setValue(line, key, value); err != nil {
			return err
		}
	} else {
		next.appendLine(key + ` = "` + escapeValue(value) + `"`)
	}
	*d = *next
	return nil
}

// AddTopic appends name to the topic list, keeping the order the file already
// has. A name that is already there is an error, not a second subscription.
func (d *Document) AddTopic(name string) error {
	name = strings.TrimSpace(name)
	topics, index, err := d.topics()
	if err != nil {
		return err
	}
	for _, topic := range topics {
		if topic == name {
			return &EditError{Line: index + 1, Key: keyTopic, Msg: msgDuplicateTopic, Args: []any{name}}
		}
	}
	if !topicPattern.MatchString(name) {
		return &EditError{Line: index + 1, Key: keyTopic, Msg: msgTopicList, Args: []any{name}}
	}

	next := d.clone()
	if err := next.setValue(index, keyTopic, strings.Join(append(topics, name), ", ")); err != nil {
		return err
	}
	*d = *next
	return nil
}

// RemoveTopic drops name from the topic list and leaves the rest in the same
// order. Removing the only topic is an error: the program would have nothing
// to subscribe to.
func (d *Document) RemoveTopic(name string) error {
	name = strings.TrimSpace(name)
	topics, index, err := d.topics()
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(topics))
	found := false
	for _, topic := range topics {
		if topic == name {
			found = true
			continue
		}
		kept = append(kept, topic)
	}
	switch {
	case !found:
		return &EditError{Line: index + 1, Key: keyTopic, Msg: msgMissingTopic, Args: []any{name}}
	case len(kept) == 0:
		return &EditError{Line: index + 1, Key: keyTopic, Msg: msgOnlyTopic}
	}

	next := d.clone()
	if err := next.setValue(index, keyTopic, strings.Join(kept, ", ")); err != nil {
		return err
	}
	*d = *next
	return nil
}

// topics returns the topics the document names and the index of the topic key.
// Both operations that change the list need the validated current one, so an
// unusable document stops here instead of being edited.
func (d *Document) topics() ([]string, int, error) {
	cfg, err := d.Check()
	if err != nil {
		return nil, 0, err
	}
	index, ok := d.keyLine(keyTopic)
	if !ok {
		return nil, 0, &EditError{Key: keyTopic, Msg: msgRequired}
	}
	return cfg.Topics, index, nil
}

// setValue replaces the quoted value of the line, keeping everything before
// the opening quote and everything from the closing quote on, which includes
// the comment behind the value.
func (d *Document) setValue(index int, key, value string) error {
	raw := d.lines[index]
	line := index + 1

	equal := strings.IndexByte(raw, '=')
	if equal < 0 {
		return &EditError{Line: line, Key: key, Msg: msgAssignment}
	}
	open := strings.IndexByte(raw[equal:], '"')
	if open < 0 {
		return &EditError{Line: line, Key: key, Msg: msgNoValue}
	}
	open += equal
	close, ok := closingQuote(raw, open)
	if !ok {
		return &EditError{Line: line, Key: key, Msg: msgUnterminated}
	}

	d.lines[index] = raw[:open+1] + escapeValue(value) + raw[close:]
	return nil
}

// keyLine returns the index of the line that assigns key. It reads the line
// the way the loader does: the first `=` splits the key from the value and the
// key is a bare key, so a comment that mentions `key = x` is not a match.
func (d *Document) keyLine(key string) (int, bool) {
	for i, raw := range d.lines {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		equal := strings.IndexByte(text, '=')
		if equal <= 0 {
			continue
		}
		if strings.TrimSpace(text[:equal]) == key {
			return i, true
		}
	}
	return 0, false
}

// appendLine adds a line at the end of the document.
func (d *Document) appendLine(text string) {
	d.lines = append(d.lines, text)
	d.trailingNewline = true
}

// clone copies the document so a failed edit can be dropped whole.
func (d *Document) clone() *Document {
	lines := make([]string, len(d.lines))
	copy(lines, d.lines)
	return &Document{lines: lines, trailingNewline: d.trailingNewline}
}

// blame attaches the line of the key the loader rejected. The loader knows the
// key; only the document knows where it is written.
func (d *Document) blame(err error) error {
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		return err
	}
	if index, ok := d.keyLine(vErr.Field); ok {
		return &LineError{Line: index + 1, Err: err}
	}
	return err
}

// supportedKey reports whether key is one the configuration has.
func supportedKey(key string) bool {
	switch key {
	case keyServer, keyTopic, keyUser, keyPassword, keyLogLevel:
		return true
	}
	return false
}

// escapeValue renders value as the body of a TOML basic string, with the same
// escapes the loader reads.
func escapeValue(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// closingQuote returns the index of the quote that closes the string opened at
// open, skipping the escaped quotes.
func closingQuote(text string, open int) (int, bool) {
	for i := open + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i, true
		}
	}
	return 0, false
}

// EditError reports an edit the document refuses to make. Msg is a gettext
// msgid, Key is the key it is about and Line is the line of the file, or 0
// when the problem is not about one line.
type EditError struct {
	Line int
	Key  string
	Msg  string
	Args []any
}

func (e *EditError) Error() string {
	msg := fmt.Sprintf(e.Msg, e.Args...)
	if e.Line == 0 {
		return fmt.Sprintf("%s: %s", e.Key, msg)
	}
	return fmt.Sprintf("line %d: %s: %s", e.Line, e.Key, msg)
}

// LineError is the loader error of a value, with the line the key sits on. The
// original error stays reachable with errors.As, so a caller that knows
// ValidationError or SyntaxError still gets it.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e *LineError) Unwrap() error { return e.Err }
