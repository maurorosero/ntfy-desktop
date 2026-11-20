// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/history"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

func TestEntryLine(t *testing.T) {
	at := time.Date(2026, 11, 20, 14, 3, 0, 0, time.Local)

	cases := []struct {
		name  string
		entry history.Entry
		want  string
	}{
		{
			name:  "unread with a title and a body",
			entry: history.Entry{ID: "a", Topic: "work", Time: at, Title: "Deploy", Body: "all green"},
			want:  "* 2026-11-20 14:03  work  Deploy — all green",
		},
		{
			name:  "read",
			entry: history.Entry{ID: "a", Topic: "work", Time: at, Title: "Deploy", Body: "all green", Read: true},
			want:  "  2026-11-20 14:03  work  Deploy — all green",
		},
		{
			name:  "without a title",
			entry: history.Entry{ID: "a", Topic: "work", Time: at, Body: "only the body"},
			want:  "* 2026-11-20 14:03  work  (no title) — only the body",
		},
		{
			name:  "without a body",
			entry: history.Entry{ID: "a", Topic: "work", Time: at, Title: "Only the title"},
			want:  "* 2026-11-20 14:03  work  Only the title",
		},
		{
			name:  "only the first line of the body",
			entry: history.Entry{ID: "a", Topic: "work", Time: at, Title: "T", Body: "first\nsecond\nthird"},
			want:  "* 2026-11-20 14:03  work  T — first",
		},
	}

	tr := testTranslator()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := entryLine(c.entry, tr); got != c.want {
				t.Errorf("entryLine(%+v) = %q; want %q", c.entry, got, c.want)
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	long := strings.Repeat("x", lineWidth+5)

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "", want: ""},
		{name: "one line", body: "hello", want: "hello"},
		{name: "the first of many", body: "hello\nworld", want: "hello"},
		{name: "leading blank lines", body: "\n\nhello", want: "hello"},
		{name: "spaces collapsed", body: "  hello   there  ", want: "hello there"},
		{name: "tabs and carriage returns", body: "a\tb\r\nc", want: "a b"},
		{name: "too long is cut", body: long, want: long[:lineWidth] + "…"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstLine(c.body); got != c.want {
				t.Errorf("firstLine(%q) = %q; want %q", c.body, got, c.want)
			}
		})
	}
}

func TestParseHistoryFlags(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     command
		wantUnr  bool
		wantFail bool
	}{
		{name: "the history subcommand", args: []string{"history"}, want: commandHistory},
		{name: "the history flag", args: []string{"--history"}, want: commandHistory},
		{name: "unread after the subcommand", args: []string{"history", "--unread"}, want: commandHistory, wantUnr: true},
		{name: "unread with the flag", args: []string{"--history", "--unread"}, want: commandHistory, wantUnr: true},
		{name: "unread alone lists the history", args: []string{"--unread"}, want: commandHistory, wantUnr: true},
		{name: "unread with the daemon", args: []string{"daemon", "--unread"}, wantFail: true},
		{name: "history with the daemon", args: []string{"history", "--daemon"}, wantFail: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse(c.args, testTranslator(), io.Discard)
			if c.wantFail {
				if err == nil {
					t.Fatalf("parse(%q) = %+v; want an error", c.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q) = error %v; want no error", c.args, err)
			}
			if got.command != c.want {
				t.Errorf("parse(%q).command = %q; want %q", c.args, got.command, c.want)
			}
			if got.unread != c.wantUnr {
				t.Errorf("parse(%q).unread = %v; want %v", c.args, got.unread, c.wantUnr)
			}
		})
	}
}

func TestRunHistory(t *testing.T) {
	// The command reads the history of the user cache directory, so the test
	// gives it one of its own.
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	want := historyDir(t)
	store, err := history.Open(want, history.DefaultLimit)
	if err != nil {
		t.Fatalf("Open(%q) = error %v; want no error", want, err)
	}
	seed := []ntfy.Message{
		{ID: "a", Topic: "work", Event: ntfy.EventMessage, Time: 1700000000, Title: "Older", Message: "first line\nsecond"},
		{ID: "b", Topic: "work", Event: ntfy.EventMessage, Time: 1700003600, Title: "Newer", Message: "body"},
	}
	for _, m := range seed {
		if err := store.Add(m); err != nil {
			t.Fatalf("Add(%q) = error %v; want no error", m.ID, err)
		}
	}
	if err := store.MarkRead("work", "a"); err != nil {
		t.Fatalf("MarkRead() = error %v; want no error", err)
	}

	var out, errOut bytes.Buffer
	if code := runHistory(options{}, testTranslator(), &out, &errOut); code != 0 {
		t.Fatalf("runHistory() = %d; want 0 (stderr: %s)", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("runHistory() printed %d lines; want 2:\n%s", len(lines), out.String())
	}
	// Newest first, read one without the star.
	if !strings.HasPrefix(lines[0], "* ") || !strings.Contains(lines[0], "Newer") {
		t.Errorf("first line = %q; want the unread newest message", lines[0])
	}
	if strings.HasPrefix(lines[1], "*") || !strings.Contains(lines[1], "Older — first line") {
		t.Errorf("second line = %q; want the read message with the first line of its body", lines[1])
	}
	if errOut.Len() != 0 {
		t.Errorf("runHistory() wrote to stderr: %s", errOut.String())
	}

	out.Reset()
	if code := runHistory(options{unread: true}, testTranslator(), &out, &errOut); code != 0 {
		t.Fatalf("runHistory(unread) = %d; want 0", code)
	}
	if got := strings.TrimSpace(out.String()); !strings.Contains(got, "Newer") || strings.Contains(got, "\n") {
		t.Errorf("runHistory(unread) = %q; want only the unread message", got)
	}
}

func TestRunHistoryWhenEmpty(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	cases := []struct {
		name string
		opts options
		want string
	}{
		{name: "everything", want: "the history is empty"},
		{name: "unread only", opts: options{unread: true}, want: "no unread messages"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := runHistory(c.opts, testTranslator(), &out, &errOut); code != 0 {
				t.Fatalf("runHistory() = %d; want 0", code)
			}
			if got := strings.TrimSpace(out.String()); got != c.want {
				t.Errorf("runHistory() = %q; want %q", got, c.want)
			}
		})
	}
}

func TestRunHistorySurvivesACorruptFile(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	dir := historyDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.jsonl"), []byte("not a history\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := runHistory(options{}, testTranslator(), &out, &errOut); code != 0 {
		t.Fatalf("runHistory() = %d; want 0 even with a corrupt file", code)
	}
	if !strings.Contains(errOut.String(), "a stored history was discarded") {
		t.Errorf("stderr = %q; want the warning about the discarded file", errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "the history is empty" {
		t.Errorf("stdout = %q; want the empty history message", got)
	}
}

// historyDir is where the command looks for the history under the cache
// directory the test set.
func historyDir(t *testing.T) string {
	t.Helper()
	dir, err := history.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir() = error %v; want no error", err)
	}
	return dir
}
