// SPDX-License-Identifier: GPL-3.0-or-later

package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// message builds what the subscription would hand to the store.
func message(id, topic, title, body string, at int64) ntfy.Message {
	return ntfy.Message{
		ID:      id,
		Topic:   topic,
		Event:   ntfy.EventMessage,
		Time:    at,
		Title:   title,
		Message: body,
	}
}

// openStore opens a store in a fresh directory, which the test owns.
func openStore(t *testing.T, dir string, limit int) *Store {
	t.Helper()
	s, err := Open(dir, limit)
	if err != nil {
		t.Fatalf("Open(%q, %d) = error %v; want no error", dir, limit, err)
	}
	return s
}

// ids is the listing as the test reads it: the identifiers, newest first.
func ids(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

// records counts the lines of a topic file, which is what compaction is about.
func records(t *testing.T, s *Store, topic string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.dir, topic+fileSuffix))
	if err != nil {
		t.Fatalf("reading the topic file: %v", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func TestAdd(t *testing.T) {
	cases := []struct {
		name     string
		messages []ntfy.Message
		limit    int
		want     []string // newest first
	}{
		{
			name:     "one message",
			messages: []ntfy.Message{message("a", "topic", "Title", "Body", 100)},
			want:     []string{"a"},
		},
		{
			name: "order is by the time the server stamped",
			messages: []ntfy.Message{
				message("a", "topic", "", "first", 100),
				message("c", "topic", "", "third", 300),
				message("b", "topic", "", "second", 200),
			},
			want: []string{"c", "b", "a"},
		},
		{
			name: "an ID that arrives twice is kept once, with the newest copy",
			messages: []ntfy.Message{
				message("a", "topic", "old", "old body", 100),
				message("b", "topic", "", "other", 200),
				message("a", "topic", "new", "new body", 300),
			},
			want: []string{"a", "b"},
		},
		{
			name: "the limit drops the oldest",
			messages: []ntfy.Message{
				message("a", "topic", "", "", 100),
				message("b", "topic", "", "", 200),
				message("c", "topic", "", "", 300),
			},
			limit: 2,
			want:  []string{"c", "b"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openStore(t, t.TempDir(), c.limit)
			for _, m := range c.messages {
				if err := s.Add(m); err != nil {
					t.Fatalf("Add(%q) = error %v; want no error", m.ID, err)
				}
			}
			got := ids(s.List(Filter{}))
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("List() = %v; want %v", got, c.want)
			}
		})
	}
}

func TestAddKeepsTheContent(t *testing.T) {
	s := openStore(t, t.TempDir(), 0)
	m := message("abc", "work-alerts", "Backup", "line one\nline two", 1700000000)
	m.Click = "https://example.test/job/7"
	m.Priority = 4

	if err := s.Add(m); err != nil {
		t.Fatalf("Add() = error %v; want no error", err)
	}

	got := s.List(Filter{})
	if len(got) != 1 {
		t.Fatalf("List() returned %d entries; want 1", len(got))
	}
	e := got[0]
	if e.ID != m.ID || e.Topic != m.Topic || e.Title != m.Title {
		t.Errorf("entry = %+v; want id %q, topic %q, title %q", e, m.ID, m.Topic, m.Title)
	}
	if e.Body != m.Message {
		t.Errorf("entry body = %q; want %q", e.Body, m.Message)
	}
	if e.Click != m.Click || e.Priority != m.Priority {
		t.Errorf("entry click/priority = %q/%d; want %q/%d", e.Click, e.Priority, m.Click, m.Priority)
	}
	if want := time.Unix(m.Time, 0).UTC(); !e.Time.Equal(want) {
		t.Errorf("entry time = %v; want %v", e.Time, want)
	}
	if e.Read {
		t.Error("entry came back read; want unread")
	}
}

func TestAddRejectsWhatCannotBeStored(t *testing.T) {
	cases := []struct {
		name string
		msg  ntfy.Message
	}{
		{name: "no id", msg: ntfy.Message{Topic: "topic"}},
		{name: "no topic", msg: ntfy.Message{ID: "a"}},
		{name: "a topic that would be a path", msg: ntfy.Message{ID: "a", Topic: "../../etc"}},
		{name: "a topic with a slash", msg: ntfy.Message{ID: "a", Topic: "work/alerts"}},
	}

	s := openStore(t, t.TempDir(), 0)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := s.Add(c.msg); err == nil {
				t.Errorf("Add(%+v) = no error; want an error", c.msg)
			}
		})
	}
	if got := s.List(Filter{}); len(got) != 0 {
		t.Errorf("List() = %v; want nothing stored", ids(got))
	}
}

func TestMarkRead(t *testing.T) {
	cases := []struct {
		name       string
		stored     []string
		mark       []string
		wantRead   []string // what the read listing shows
		wantUnread []string
	}{
		{
			name:       "one of them",
			stored:     []string{"a", "b"},
			mark:       []string{"b"},
			wantRead:   []string{"b"},
			wantUnread: []string{"a"},
		},
		{
			name:       "every one of them",
			stored:     []string{"a", "b"},
			mark:       []string{"a", "b"},
			wantRead:   []string{"b", "a"},
			wantUnread: nil,
		},
		{
			name:       "an id that is not stored is not an error",
			stored:     []string{"a"},
			mark:       []string{"gone"},
			wantRead:   nil,
			wantUnread: []string{"a"},
		},
		{
			name:       "marking twice changes nothing",
			stored:     []string{"a"},
			mark:       []string{"a", "a"},
			wantRead:   []string{"a"},
			wantUnread: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openStore(t, t.TempDir(), 0)
			for i, id := range c.stored {
				if err := s.Add(message(id, "topic", "", "", int64(100*(i+1)))); err != nil {
					t.Fatalf("Add(%q) = error %v; want no error", id, err)
				}
			}
			for _, id := range c.mark {
				if err := s.MarkRead("topic", id); err != nil {
					t.Fatalf("MarkRead(%q) = error %v; want no error", id, err)
				}
			}

			var read []string
			for _, e := range s.List(Filter{}) {
				if e.Read {
					read = append(read, e.ID)
				}
			}
			if fmt.Sprint(read) != fmt.Sprint(c.wantRead) {
				t.Errorf("read entries = %v; want %v", read, c.wantRead)
			}

			unread := ids(s.List(Filter{Unread: true}))
			if fmt.Sprint(unread) != fmt.Sprint(c.wantUnread) {
				t.Errorf("unread entries = %v; want %v", unread, c.wantUnread)
			}
		})
	}
}

func TestMarkReadSurvivesAnotherOpen(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir, 0)
	if err := s.Add(message("a", "topic", "t", "b", 100)); err != nil {
		t.Fatalf("Add() = error %v; want no error", err)
	}
	if err := s.MarkRead("topic", "a"); err != nil {
		t.Fatalf("MarkRead() = error %v; want no error", err)
	}

	again := openStore(t, dir, 0)
	got := again.List(Filter{})
	if len(got) != 1 || !got[0].Read {
		t.Errorf("after reopening, List() = %+v; want one read entry", got)
	}
}

func TestLimit(t *testing.T) {
	cases := []struct {
		name   string
		limit  int
		add    int
		want   int
		newest string
		oldest string
	}{
		{name: "under the limit keeps everything", limit: 5, add: 3, want: 3, newest: "m3", oldest: "m1"},
		{name: "at the limit keeps everything", limit: 5, add: 5, want: 5, newest: "m5", oldest: "m1"},
		{name: "over the limit drops the oldest", limit: 5, add: 9, want: 5, newest: "m9", oldest: "m5"},
		{name: "a limit of one", limit: 1, add: 4, want: 1, newest: "m4", oldest: "m4"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openStore(t, t.TempDir(), c.limit)
			for i := 1; i <= c.add; i++ {
				id := fmt.Sprintf("m%d", i)
				if err := s.Add(message(id, "topic", "", "", int64(i))); err != nil {
					t.Fatalf("Add(%q) = error %v; want no error", id, err)
				}
			}
			got := s.List(Filter{})
			if len(got) != c.want {
				t.Fatalf("List() returned %d entries; want %d (%v)", len(got), c.want, ids(got))
			}
			if got[0].ID != c.newest {
				t.Errorf("newest = %q; want %q", got[0].ID, c.newest)
			}
			if last := got[len(got)-1].ID; last != c.oldest {
				t.Errorf("oldest = %q; want %q", last, c.oldest)
			}
		})
	}
}

func TestCompact(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		add   int
		want  int // entries kept
	}{
		{name: "nothing to compact", limit: 10, add: 3, want: 3},
		{name: "compaction keeps the limit", limit: 4, add: 12, want: 4},
		{name: "compaction with a limit of one", limit: 1, add: 9, want: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openStore(t, t.TempDir(), c.limit)
			for i := 1; i <= c.add; i++ {
				id := fmt.Sprintf("m%d", i)
				if err := s.Add(message(id, "topic", "", "", int64(i))); err != nil {
					t.Fatalf("Add(%q) = error %v; want no error", id, err)
				}
				// The file is rewritten before it can grow past the limit plus
				// a quarter, whatever the number of messages.
				if got := records(t, s, "topic"); got > s.compactAt() {
					t.Fatalf("after %d messages the file holds %d records; want at most %d",
						i, got, s.compactAt())
				}
			}
			if got := s.List(Filter{}); len(got) != c.want {
				t.Errorf("List() returned %d entries; want %d", len(got), c.want)
			}

			// An explicit compaction leaves exactly what the store keeps.
			if err := s.Compact("topic"); err != nil {
				t.Fatalf("Compact() = error %v; want no error", err)
			}
			if got := records(t, s, "topic"); got != c.want {
				t.Errorf("after Compact the file holds %d records; want %d", got, c.want)
			}
			if got := again(t, s, "topic"); got != c.want {
				t.Errorf("after Compact another open reads %d entries; want %d", got, c.want)
			}
		})
	}
}

// again reopens the store of s and counts what the file holds now.
func again(t *testing.T, s *Store, topic string) int {
	t.Helper()
	reopened := openStore(t, s.dir, s.limit)
	if warns := reopened.Warnings(); len(warns) != 0 {
		t.Fatalf("reopening reported %v; want no warnings", warns)
	}
	return len(reopened.List(Filter{Topic: topic}))
}

func TestCorruptFile(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantWarns int
		wantKept  int
	}{
		{name: "not json at all", content: "this is not a history\n", wantWarns: 1},
		{name: "json that is not an entry", content: "{\"foo\": 1}\n", wantWarns: 1},
		{name: "a good line and a broken one", content: "{\"id\":\"a\",\"topic\":\"topic\"}\n{bad\n", wantWarns: 1},
		{name: "a write that was cut short", content: "{\"id\":\"a\",\"topic\":\"topic\"}\n{\"id\":\"b\"", wantKept: 1},
		{name: "an empty file", content: ""},
		{name: "blank lines", content: "\n\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "topic"+fileSuffix), []byte(c.content), fileMode); err != nil {
				t.Fatalf("writing the fixture: %v", err)
			}
			s := openStore(t, dir, 0)

			warns := s.Warnings()
			if len(warns) != c.wantWarns {
				t.Errorf("Warnings() = %v; want %d warning(s)", warns, c.wantWarns)
			}
			got := s.List(Filter{})
			if len(got) != c.wantKept {
				t.Errorf("List() returned %v; want %d entries", ids(got), c.wantKept)
			}
			if len(warns) > 0 {
				var corrupt *CorruptionError
				if !errors.As(warns[0], &corrupt) {
					t.Fatalf("warning = %v; want a *CorruptionError", warns[0])
				}
				if corrupt.Path == "" || corrupt.Line < 1 {
					t.Errorf("CorruptionError = %+v; want a path and a line", corrupt)
				}
			}
		})
	}
}

func TestCorruptFileIsReplacedOnTheNextWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topic"+fileSuffix)
	if err := os.WriteFile(path, []byte("this is not a history\n"), fileMode); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	s := openStore(t, dir, 0)
	if err := s.Add(message("a", "topic", "", "after the damage", 100)); err != nil {
		t.Fatalf("Add() = error %v; want no error", err)
	}

	again := openStore(t, dir, 0)
	if warns := again.Warnings(); len(warns) != 0 {
		t.Errorf("reopening reported %v; want the file to be readable again", warns)
	}
	if got := again.List(Filter{}); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("after reopening, List() = %v; want just %q", ids(got), "a")
	}
}

func TestLatest(t *testing.T) {
	cases := []struct {
		name  string
		add   []ntfy.Message
		topic string
		want  string
		found bool
	}{
		{
			name:  "the newest of the topic",
			add:   []ntfy.Message{message("a", "one", "", "", 100), message("b", "two", "", "", 200)},
			topic: "one",
			want:  "a",
			found: true,
		},
		{
			name:  "a topic that is not stored",
			add:   []ntfy.Message{message("a", "one", "", "", 100)},
			topic: "two",
		},
		{
			name:  "nothing stored",
			topic: "one",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openStore(t, t.TempDir(), 0)
			for _, m := range c.add {
				if err := s.Add(m); err != nil {
					t.Fatalf("Add(%q) = error %v; want no error", m.ID, err)
				}
			}
			got, ok := s.Latest(c.topic)
			if ok != c.found {
				t.Fatalf("Latest(%q) found = %v; want %v", c.topic, ok, c.found)
			}
			if ok && got.ID != c.want {
				t.Errorf("Latest(%q) = %q; want %q", c.topic, got.ID, c.want)
			}
		})
	}
}

func TestListFilterByTopic(t *testing.T) {
	s := openStore(t, t.TempDir(), 0)
	for _, m := range []ntfy.Message{
		message("a", "one", "", "", 100),
		message("b", "two", "", "", 200),
		message("c", "one", "", "", 300),
	} {
		if err := s.Add(m); err != nil {
			t.Fatalf("Add(%q) = error %v; want no error", m.ID, err)
		}
	}

	if got := ids(s.List(Filter{Topic: "one"})); fmt.Sprint(got) != fmt.Sprint([]string{"c", "a"}) {
		t.Errorf("List(topic one) = %v; want [c a]", got)
	}
	if got := ids(s.List(Filter{Limit: 2})); fmt.Sprint(got) != fmt.Sprint([]string{"c", "b"}) {
		t.Errorf("List(limit 2) = %v; want [c b]", got)
	}
}

func FuzzParseLog(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("{\"id\":\"a\",\"topic\":\"t\",\"time\":\"2026-01-01T00:00:00Z\"}\n"))
	f.Add([]byte("{\"id\":\"a\"}\n{\"id\":\"b\"}\n"))
	f.Add([]byte("{\"id\":\"a\"}\n{bad"))
	f.Add([]byte("null\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		entries, records, err := parseLog(data)
		if err != nil {
			if entries != nil || records != 0 {
				t.Fatalf("parseLog(%q) returned %d entries with an error; want neither", data, len(entries))
			}
			return
		}
		if records < len(entries) {
			t.Fatalf("parseLog(%q) counted %d records for %d entries", data, records, len(entries))
		}
		seen := make(map[string]bool, len(entries))
		for _, e := range entries {
			if e.ID == "" {
				t.Fatalf("parseLog(%q) returned an entry without an id", data)
			}
			if seen[e.ID] {
				t.Fatalf("parseLog(%q) returned %q twice", data, e.ID)
			}
			seen[e.ID] = true
		}
	})
}

func TestDefaultDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir() = error %v; want no error", err)
	}
	want := filepath.Join(base, dirName, subdir)
	if got != want {
		t.Errorf("DefaultDir() = %q; want %q", got, want)
	}
}

func TestEntryJSONShape(t *testing.T) {
	// The file is the contract between this version and the next one: an
	// optional field that is not set must not appear.
	e := Entry{ID: "a", Topic: "t", Time: time.Unix(100, 0).UTC(), Title: "x", Body: "y"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() = error %v; want no error", err)
	}
	for _, field := range []string{"click", "priority", "read"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Errorf("Marshal() = %s; want no %q for an empty value", data, field)
		}
	}
}
