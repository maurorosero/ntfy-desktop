// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
)

const sample = `# my configuration
server    = "https://ntfy.example.org"   # the server, with a comment
topic     = "casa, pruebas"             # one stream, two topics

# credentials, when the topic is protected
user     = "andrea"
password = "s3cr3t"
`

func TestSetRewritesOneLine(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		key   string
		value string
		want  string
	}{
		{
			name:  "trailing comment of the edited line stays",
			in:    "# ntfy-desktop\nserver = \"https://old.example.org\"  # my server\ntopic = \"alerts\"\n",
			key:   keyServer,
			value: "https://new.example.org",
			want:  "# ntfy-desktop\nserver = \"https://new.example.org\"  # my server\ntopic = \"alerts\"\n",
		},
		{
			name:  "alignment in front of the value stays",
			in:    "server    = \"https://ntfy.sh\"\ntopic     = \"casa\"\n",
			key:   keyTopic,
			value: "casa, avisos",
			want:  "server    = \"https://ntfy.sh\"\ntopic     = \"casa, avisos\"\n",
		},
		{
			name:  "every other line is identical",
			in:    sample,
			key:   keyPassword,
			value: "other",
			want:  strings.Replace(sample, `password = "s3cr3t"`, `password = "other"`, 1),
		},
		{
			name:  "a comment that mentions the key is not the key",
			in:    "# server = \"https://commented.example.org\"\nserver = \"https://ntfy.sh\"\ntopic = \"alerts\"\n",
			key:   keyServer,
			value: "https://real.example.org",
			want:  "# server = \"https://commented.example.org\"\nserver = \"https://real.example.org\"\ntopic = \"alerts\"\n",
		},
		{
			name:  "a key the file does not have is appended",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n",
			key:   keyLogLevel,
			value: "debug",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nlog-level = \"debug\"\n",
		},
		{
			name:  "crlf line endings stay",
			in:    "server = \"https://ntfy.sh\"\r\ntopic = \"casa\"\r\n",
			key:   keyTopic,
			value: "avisos",
			want:  "server = \"https://ntfy.sh\"\r\ntopic = \"avisos\"\r\n",
		},
		{
			name:  "a file without the final newline keeps it that way",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"casa\"",
			key:   keyTopic,
			value: "avisos",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"avisos\"",
		},
		{
			name:  "the first key of an empty document",
			in:    "",
			key:   keyServer,
			value: "https://ntfy.sh",
			want:  "server = \"https://ntfy.sh\"\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := NewDocument([]byte(c.in))
			if err := doc.Set(c.key, c.value); err != nil {
				t.Fatalf("Set(%q, %q) = %v; want no error", c.key, c.value, err)
			}
			if got := string(doc.Bytes()); got != c.want {
				t.Errorf("Set(%q, %q) = %q; want %q", c.key, c.value, got, c.want)
			}
		})
	}
}

func TestSetKeepsTheOtherLinesIntact(t *testing.T) {
	doc := NewDocument([]byte(sample))
	if err := doc.Set(keyServer, "https://ntfy.sh"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyServer, err)
	}

	before := strings.Split(sample, "\n")
	after := strings.Split(string(doc.Bytes()), "\n")
	if len(before) != len(after) {
		t.Fatalf("the edit changed the line count: %d before, %d after", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
			if i != 1 {
				t.Errorf("line %d changed to %q; want only line 2", i+1, after[i])
			}
		}
	}
	if changed != 1 {
		t.Errorf("the edit changed %d lines; want 1", changed)
	}
}

func TestSetEscapesTheValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "quotes and backslashes", value: `a"b\c`, want: `a\"b\\c`},
		{name: "newline and tab", value: "a\nb\tc", want: `a\nb\tc`},
		{name: "plain", value: "casa", want: "casa"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := NewDocument([]byte("server = \"https://ntfy.sh\"\nuser = \"andrea\"\npassword = \"x\"\n"))
			if err := doc.Set(keyPassword, c.value); err != nil {
				t.Fatalf("Set(%q, %q) = %v; want no error", keyPassword, c.value, err)
			}
			want := `password = "` + c.want + `"`
			if !strings.Contains(string(doc.Bytes()), want) {
				t.Errorf("Set(%q, %q) wrote %q; want the line %q", keyPassword, c.value, doc.Bytes(), want)
			}
		})
	}
}

// TestSetRoundTripsAValue closes the loop through the loader: the escape Set
// writes is what the loader reads back as the same value.
func TestSetRoundTripsAValue(t *testing.T) {
	const value = `a"b\c`

	doc := NewDocument([]byte("server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nuser = \"andrea\"\npassword = \"x\"\n"))
	if err := doc.Set(keyPassword, value); err != nil {
		t.Fatalf("Set(%q, %q) = %v; want no error", keyPassword, value, err)
	}
	cfg, err := doc.Check()
	if err != nil {
		t.Fatalf("Check() = %v; want the document to load", err)
	}
	if cfg.Password != value {
		t.Errorf("Check().Password = %q; want %q", cfg.Password, value)
	}
}

func TestSetRefusesAnUnknownKey(t *testing.T) {
	doc := NewDocument([]byte(sample))
	err := doc.Set("secret", "x")
	if err == nil {
		t.Fatalf("Set(%q) = nil; want an error", "secret")
	}
	if got := string(doc.Bytes()); got != sample {
		t.Errorf("Set(%q) changed the document to %q; want it untouched", "secret", got)
	}
	var eErr *EditError
	if !errors.As(err, &eErr) {
		t.Fatalf("Set(%q) = %T; want *EditError", "secret", err)
	}
	if eErr.Key != "secret" {
		t.Errorf("EditError.Key = %q; want %q", eErr.Key, "secret")
	}
}

func TestAddTopic(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		topic   string
		want    string
		wantErr string
	}{
		{
			name:  "appends after the topics the file has",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"casa, pruebas\"   # topics\n",
			topic: "avisos",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"casa, pruebas, avisos\"   # topics\n",
		},
		{
			name:  "the spaces around the name are typing, not part of it",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
			topic: "  avisos  ",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\n",
		},
		{
			name:  "the comment in front of the key stays",
			in:    "# the topic to follow\nserver = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
			topic: "avisos",
			want:  "# the topic to follow\nserver = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\n",
		},
		{
			name:    "a name that is already there is refused",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\n",
			topic:   "avisos",
			wantErr: `line 2: topic: "avisos" is already in the topic list`,
		},
		{
			name:    "an invalid name is refused before it is written",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
			topic:   "a/b",
			wantErr: `line 2: topic: "a/b" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "an empty name is refused",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
			topic:   "  ",
			wantErr: `line 2: topic: "" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "an unusable file is not edited",
			in:      "topic = \"casa\"\n",
			topic:   "avisos",
			wantErr: "server: must not be empty",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := NewDocument([]byte(c.in))
			err := doc.AddTopic(c.topic)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("AddTopic(%q) = nil; want the error %q", c.topic, c.wantErr)
				}
				if got := err.Error(); got != c.wantErr {
					t.Errorf("AddTopic(%q) = %q; want %q", c.topic, got, c.wantErr)
				}
				if got := string(doc.Bytes()); got != c.in {
					t.Errorf("AddTopic(%q) left %q; want the original %q", c.topic, got, c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddTopic(%q) = %v; want no error", c.topic, err)
			}
			if got := string(doc.Bytes()); got != c.want {
				t.Errorf("AddTopic(%q) = %q; want %q", c.topic, got, c.want)
			}
			if _, err := doc.Check(); err != nil {
				t.Errorf("AddTopic(%q) left a document that does not load: %v", c.topic, err)
			}
		})
	}
}

func TestRemoveTopic(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		topic   string
		want    string
		wantErr string
	}{
		{
			name:  "the first of three",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"casa, pruebas, avisos\"   # topics\n",
			topic: "casa",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"pruebas, avisos\"   # topics\n",
		},
		{
			name:  "the middle keeps the order of the rest",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"zeta, alfa, medio\"\n",
			topic: "alfa",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"zeta, medio\"\n",
		},
		{
			name:  "the spaces are not part of the name",
			in:    "server = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\n",
			topic: " avisos ",
			want:  "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
		},
		{
			name:    "the only topic cannot go",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n",
			topic:   "casa",
			wantErr: "line 2: topic: cannot remove the only topic",
		},
		{
			name:    "a name that is not there is refused",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\n",
			topic:   "otro",
			wantErr: `line 2: topic: "otro" is not in the topic list`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := NewDocument([]byte(c.in))
			err := doc.RemoveTopic(c.topic)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("RemoveTopic(%q) = nil; want the error %q", c.topic, c.wantErr)
				}
				if got := err.Error(); got != c.wantErr {
					t.Errorf("RemoveTopic(%q) = %q; want %q", c.topic, got, c.wantErr)
				}
				if got := string(doc.Bytes()); got != c.in {
					t.Errorf("RemoveTopic(%q) left %q; want the original %q", c.topic, got, c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("RemoveTopic(%q) = %v; want no error", c.topic, err)
			}
			if got := string(doc.Bytes()); got != c.want {
				t.Errorf("RemoveTopic(%q) = %q; want %q", c.topic, got, c.want)
			}
		})
	}
}

func TestCheckNamesTheLineOfTheValue(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantMsg string
	}{
		{
			name:    "a value that does not validate",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nlog-level = \"trace\"\n",
			wantMsg: `line 3: log-level: must be debug, info, warn or error`,
		},
		{
			name:    "a syntax error keeps its own line",
			in:      "server = \"https://ntfy.sh\"\ntopic = alerts\n",
			wantMsg: "line 2: value must be a quoted string",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewDocument([]byte(c.in)).Check()
			if err == nil {
				t.Fatalf("Check(%q) = nil; want the error %q", c.in, c.wantMsg)
			}
			if got := err.Error(); got != c.wantMsg {
				t.Errorf("Check(%q) = %q; want %q", c.in, got, c.wantMsg)
			}
		})
	}
}

func TestLineErrorKeepsTheLoaderError(t *testing.T) {
	_, err := NewDocument([]byte("server = \"https://ntfy.sh\"\ntopic = \"\"\n")).Check()
	if err == nil {
		t.Fatal("Check() = nil; want an error")
	}
	var lineErr *LineError
	if !errors.As(err, &lineErr) {
		t.Fatalf("Check() = %T; want *LineError", err)
	}
	if lineErr.Line != 2 {
		t.Errorf("LineError.Line = %d; want 2", lineErr.Line)
	}
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Errorf("Check() = %v; want a *ValidationError reachable with errors.As", err)
	}
}

func TestNewDocumentRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"\n",
		"server = \"https://ntfy.sh\"\n",
		"server = \"https://ntfy.sh\"",
		"# only a comment\n\n",
		"a\r\nb\r\n",
	}

	for _, in := range cases {
		if got := string(NewDocument([]byte(in)).Bytes()); got != in {
			t.Errorf("NewDocument(%q).Bytes() = %q; want the same text", in, got)
		}
	}
}

func FuzzDocument(f *testing.F) {
	seeds := []string{
		"",
		"server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n",
		"# comment\nserver = \"https://ntfy.sh\"  # trailing\ntopic = \"casa, pruebas\"\n",
		"server = \"https://ntfy.sh\"\ntopic = \"a\\\"b\"\n",
		"topic = \"casa\"\n",
	}
	for _, s := range seeds {
		f.Add(s, "topic", "avisos")
	}

	f.Fuzz(func(t *testing.T, in, key, value string) {
		doc := NewDocument([]byte(in))
		original := string(doc.Bytes())

		if err := doc.Set(key, value); err != nil && string(doc.Bytes()) != original {
			t.Fatalf("Set(%q, %q) failed and changed the document", key, value)
		}

		edited := NewDocument([]byte(original))
		if err := edited.AddTopic(value); err != nil && string(edited.Bytes()) != original {
			t.Fatalf("AddTopic(%q) failed and changed the document", value)
		}

		removed := NewDocument([]byte(original))
		if err := removed.RemoveTopic(value); err != nil && string(removed.Bytes()) != original {
			t.Fatalf("RemoveTopic(%q) failed and changed the document", value)
		}
	})
}

// TestCatalogCoversTheEdits keeps the new msgids in the catalogues: an error
// whose msgid is missing reaches the user in English.
func TestCatalogCoversTheEdits(t *testing.T) {
	cases := []struct {
		locale string
		want   string
	}{
		{locale: "en", want: "cannot remove the only topic"},
		{locale: "es", want: "no se puede quitar el único topic"},
	}

	for _, c := range cases {
		t.Run(c.locale, func(t *testing.T) {
			tr := i18n.New(c.locale)
			if got := tr.T(msgOnlyTopic); got != c.want {
				t.Errorf("catalog %s = %q; want %q", c.locale, got, c.want)
			}
		})
	}
}
