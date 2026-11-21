// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTextPrompterLine(t *testing.T) {
	cases := []struct {
		name    string
		answer  string
		current string
		want    string
	}{
		{name: "an answer", answer: "https://ntfy.sh\n", want: "https://ntfy.sh"},
		{name: "an answer with a windows newline", answer: "casa\r\n", want: "casa"},
		{name: "enter keeps the current value", answer: "\n", current: "https://ntfy.sh", want: "https://ntfy.sh"},
		{name: "an answer wins over the current value", answer: "avisos\n", current: "casa", want: "avisos"},
		{name: "a last line without its newline", answer: "casa", want: "casa"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			p := newTextPrompter(testTranslator(), strings.NewReader(c.answer), &out, &out)
			got, err := p.Line(context.Background(), "question:", c.current)
			if err != nil {
				t.Fatalf("Line(%q) = error %v; want no error", c.answer, err)
			}
			if got != c.want {
				t.Errorf("Line(%q) with current %q = %q; want %q", c.answer, c.current, got, c.want)
			}
		})
	}
}

func TestTextPrompterYesNo(t *testing.T) {
	cases := []struct {
		name  string
		input string
		def   bool
		want  bool
	}{
		{name: "y", input: "y\n", want: true},
		{name: "yes", input: "yes\n", want: true},
		{name: "s", input: "s\n", want: true},
		{name: "sí", input: "sí\n", want: true},
		{name: "n", input: "n\n", want: false},
		{name: "no", input: "NO\n", want: false},
		{name: "enter keeps the default no", input: "\n", def: false, want: false},
		{name: "enter keeps the default yes", input: "\n", def: true, want: true},
		{name: "an answer asks again", input: "quizá\nn\n", def: true, want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			p := newTextPrompter(testTranslator(), strings.NewReader(c.input), &out, &out)
			got, err := p.YesNo(context.Background(), "write?", c.def)
			if err != nil {
				t.Fatalf("YesNo(%q) = error %v; want no error", c.input, err)
			}
			if got != c.want {
				t.Errorf("YesNo(%q) with default %v = %v; want %v", c.input, c.def, got, c.want)
			}
		})
	}
}

func TestTextPrompterSecretWithoutATerminal(t *testing.T) {
	var out bytes.Buffer
	p := newTextPrompter(testTranslator(), strings.NewReader("s3cr3t\n"), &out, &out)
	got, err := p.Secret(context.Background(), "password:")
	if err != nil {
		t.Fatalf("Secret() = error %v; want no error", err)
	}
	if got != "s3cr3t" {
		t.Errorf("Secret() = %q; want %q", got, "s3cr3t")
	}
	if !strings.Contains(out.String(), "password:") {
		t.Errorf("output = %q; want the question printed", out.String())
	}
}

func TestTextPrompterCancelsAtTheEndOfTheInput(t *testing.T) {
	var out bytes.Buffer
	p := newTextPrompter(testTranslator(), strings.NewReader(""), &out, &out)
	if _, err := p.Line(context.Background(), "question:", ""); !errors.Is(err, ErrCancelled) {
		t.Errorf("Line() at the end of the input = %v; want ErrCancelled", err)
	}
}

func TestTextPrompterNote(t *testing.T) {
	var out bytes.Buffer
	p := newTextPrompter(testTranslator(), strings.NewReader(""), &out, &out)
	p.Note("wrote the file")
	if out.String() != "wrote the file\n" {
		t.Errorf("output = %q; want the note and its newline", out.String())
	}
}
