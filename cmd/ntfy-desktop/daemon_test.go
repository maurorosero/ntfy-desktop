// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/history"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/queue"
)

// fakeHistory is the store the daemon writes to: it records the calls instead of
// touching a file, and can be told to fail.
type fakeHistory struct {
	mu      sync.Mutex
	added   []ntfy.Message
	read    []string // "topic/id", in order
	addErr  error
	readErr error
}

func (f *fakeHistory) Add(m ntfy.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, m)
	return nil
}

func (f *fakeHistory) MarkRead(topic, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.read = append(f.read, topic+"/"+id)
	return nil
}

func (f *fakeHistory) storedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, len(f.added))
	for i, m := range f.added {
		ids[i] = m.ID
	}
	return ids
}

func (f *fakeHistory) readIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.read...)
}

// fakeLatest is the history as a subscription sees it: the last entry of every
// topic.
type fakeLatest struct{ entries map[string]history.Entry }

func (f fakeLatest) Latest(topic string) (history.Entry, bool) {
	e, ok := f.entries[topic]
	return e, ok
}

// capture keeps the records of a logger, so a test can assert that a failure was
// reported instead of swallowed.
type capture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func (c *capture) warned() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Level >= slog.LevelWarn {
			return true
		}
	}
	return false
}

// newTestLogger returns a logger that keeps what it is told and discards the
// rest, so a test run stays quiet.
func newTestLogger() (*slog.Logger, *capture) {
	logs := &capture{}
	return slog.New(logs), logs
}

// TestDeliverStoresOnlyMessages is the acceptance criterion: a keepalive, an
// open event or an event without a type never reaches the history.
func TestDeliverStoresOnlyMessages(t *testing.T) {
	cases := []struct {
		name    string
		event   string
		wantAdd bool
	}{
		{name: "a message", event: ntfy.EventMessage, wantAdd: true},
		{name: "a keepalive", event: ntfy.EventKeepalive, wantAdd: false},
		{name: "an open event", event: ntfy.EventOpen, wantAdd: false},
		{name: "an event without a type", event: "", wantAdd: false},
		{name: "a poll request", event: ntfy.EventPollRequest, wantAdd: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, _ := newTestLogger()
			h := &fakeHistory{}
			m := ntfy.Message{ID: "m1", Event: c.event, Topic: "ops", Message: "hola"}

			deliver(context.Background(), log, m, ports{hist: h})

			got := h.storedIDs()
			if c.wantAdd && len(got) != 1 {
				t.Errorf("stored %v; want the message", got)
			}
			if !c.wantAdd && len(got) != 0 {
				t.Errorf("stored %v for a %q event; want nothing", got, c.event)
			}
		})
	}
}

// TestDeliverShowsAndQueues checks that the history does not replace the rest of
// the delivery: the notification is still shown and a decision is still queued.
func TestDeliverShowsAndQueues(t *testing.T) {
	cases := []struct {
		name      string
		actions   []ntfy.Action
		wantShown int
		wantQueue int
	}{
		{name: "plain message", actions: nil, wantShown: 1, wantQueue: 0},
		{
			name:      "message with an action",
			actions:   []ntfy.Action{{Action: "view", Label: "Open", URL: "https://example.com"}},
			wantShown: 1,
			wantQueue: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, _ := newTestLogger()
			h := &fakeHistory{}
			shown, queued := 0, 0
			p := ports{
				hist: h,
				show: func(context.Context, ntfy.Message) error { shown++; return nil },
				submit: func(ntfy.Message) error {
					queued++
					return nil
				},
			}

			deliver(context.Background(), log, ntfy.Message{
				ID: "m1", Event: ntfy.EventMessage, Topic: "ops", Actions: c.actions,
			}, p)

			if shown != c.wantShown {
				t.Errorf("shown %d times; want %d", shown, c.wantShown)
			}
			if queued != c.wantQueue {
				t.Errorf("queued %d times; want %d", queued, c.wantQueue)
			}
			if got := len(h.storedIDs()); got != 1 {
				t.Errorf("stored %d messages; want 1", got)
			}
		})
	}
}

// TestDeliverWithoutHistory is the degraded session: no store, and the message is
// still shown.
func TestDeliverWithoutHistory(t *testing.T) {
	log, _ := newTestLogger()
	shown := 0
	p := ports{show: func(context.Context, ntfy.Message) error { shown++; return nil }}

	deliver(context.Background(), log, ntfy.Message{ID: "m1", Event: ntfy.EventMessage, Topic: "ops"}, p)

	if shown != 1 {
		t.Errorf("shown %d times; want 1", shown)
	}
}

// TestDeliverReportsAStoringFailure: the history is a cache, so failing to write
// it costs a warning and not the notification.
func TestDeliverReportsAStoringFailure(t *testing.T) {
	log, logs := newTestLogger()
	h := &fakeHistory{addErr: errors.New("the cache directory is read-only")}
	shown := 0
	p := ports{
		hist: h,
		show: func(context.Context, ntfy.Message) error { shown++; return nil },
	}

	deliver(context.Background(), log, ntfy.Message{ID: "m1", Event: ntfy.EventMessage, Topic: "ops"}, p)

	if shown != 1 {
		t.Errorf("shown %d times; want 1 even when the history failed", shown)
	}
	if !logs.warned() {
		t.Error("the storing failure was not logged at warning level")
	}
}

// TestAnsweredMarksTheMessageAsRead is the acceptance criterion: only an answer
// marks the message, and it does so whether the action then worked or not.
func TestAnsweredMarksTheMessageAsRead(t *testing.T) {
	cases := []struct {
		name     string
		result   queue.Result
		wantRead []string
	}{
		{
			name: "answered",
			result: queue.Result{
				Message: ntfy.Message{ID: "m1", Topic: "ops"},
				Action:  ntfy.Action{Action: "view", Label: "Open"},
				Status:  queue.Answered,
			},
			wantRead: []string{"ops/m1"},
		},
		{
			name: "answered and the action failed",
			result: queue.Result{
				Message: ntfy.Message{ID: "m2", Topic: "ops"},
				Action:  ntfy.Action{Action: "http", Label: "Approve"},
				Status:  queue.Answered,
				Err:     errors.New("the server refused the request"),
			},
			wantRead: []string{"ops/m2"},
		},
		{
			name:     "expired without an answer",
			result:   queue.Result{Message: ntfy.Message{ID: "m3", Topic: "ops"}, Status: queue.Expired},
			wantRead: nil,
		},
		{
			name: "the dialog could not be asked",
			result: queue.Result{
				Message: ntfy.Message{ID: "m4", Topic: "ops"},
				Status:  queue.Unavailable,
				Err:     errors.New("no zenity"),
			},
			wantRead: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, _ := newTestLogger()
			h := &fakeHistory{}
			results := make(chan queue.Result, 1)
			results <- c.result
			close(results)

			logResults(log, results, h)

			got := h.readIDs()
			if len(got) != len(c.wantRead) {
				t.Fatalf("marked read %v; want %v", got, c.wantRead)
			}
			for i := range got {
				if got[i] != c.wantRead[i] {
					t.Errorf("marked read %v; want %v", got, c.wantRead)
				}
			}
		})
	}
}

// TestLogResultsWithoutHistory is the degraded session: results are still
// reported and nothing is marked.
func TestLogResultsWithoutHistory(t *testing.T) {
	log, _ := newTestLogger()
	results := make(chan queue.Result, 1)
	results <- queue.Result{
		Message: ntfy.Message{ID: "m1", Topic: "ops"},
		Status:  queue.Answered,
	}
	close(results)

	logResults(log, results, nil) // must not panic
}

// TestLogResultsReportsAMarkFailure: the person answered, so the failure of the
// history is a line and not a reason to change the outcome.
func TestLogResultsReportsAMarkFailure(t *testing.T) {
	log, logs := newTestLogger()
	h := &fakeHistory{readErr: errors.New("the cache directory is read-only")}
	results := make(chan queue.Result, 1)
	results <- queue.Result{Message: ntfy.Message{ID: "m1", Topic: "ops"}, Status: queue.Answered}
	close(results)

	logResults(log, results, h)

	if !logs.warned() {
		t.Error("the failure to mark the message was not logged at warning level")
	}
}

func TestStartID(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 11, 20, hour, 0, 0, 0, time.UTC) }

	cases := []struct {
		name    string
		store   resumable
		topics  []string
		wantID  string
		comment string
	}{
		{
			name:   "no history at all",
			store:  nil,
			topics: []string{"ops"},
			wantID: "",
		},
		{
			name:   "an empty history",
			store:  fakeLatest{},
			topics: []string{"ops"},
			wantID: "",
		},
		{
			name:   "one topic resumes after its newest message",
			store:  fakeLatest{entries: map[string]history.Entry{"ops": {ID: "m3", Time: at(9)}}},
			topics: []string{"ops"},
			wantID: "m3",
		},
		{
			name: "a topic the history never saw contributes nothing",
			store: fakeLatest{entries: map[string]history.Entry{
				"ops": {ID: "m3", Time: at(9)},
			}},
			topics: []string{"ops", "other"},
			wantID: "m3",
		},
		{
			name: "two topics share the oldest of their newest ids",
			store: fakeLatest{entries: map[string]history.Entry{
				"ops":    {ID: "m3", Time: at(9)},
				"builds": {ID: "b1", Time: at(7)},
			}},
			topics:  []string{"ops", "builds"},
			wantID:  "b1",
			comment: "the older id cannot skip either topic",
		},
		{
			name: "the configured order does not change the answer",
			store: fakeLatest{entries: map[string]history.Entry{
				"ops":    {ID: "m3", Time: at(9)},
				"builds": {ID: "b1", Time: at(7)},
			}},
			topics: []string{"builds", "ops"},
			wantID: "b1",
		},
		{
			name: "a configured topic with nothing stored is ignored",
			store: fakeLatest{entries: map[string]history.Entry{
				"other": {ID: "x1", Time: at(1)},
			}},
			topics: []string{"ops"},
			wantID: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := startID(c.store, c.topics); got != c.wantID {
				t.Errorf("startID(%v) = %q; want %q (%s)", c.topics, got, c.wantID, c.comment)
			}
		})
	}
}

// TestStartIDUsesTheStore keeps the helper honest against the real store: what
// was written to disk is what the subscription resumes from.
func TestStartIDUsesTheStore(t *testing.T) {
	store, err := history.Open(t.TempDir(), history.DefaultLimit)
	if err != nil {
		t.Fatalf("history.Open() = %v; want no error", err)
	}
	if got := startID(store, []string{"ops"}); got != "" {
		t.Errorf("startID() on an empty store = %q; want it empty", got)
	}

	added := []ntfy.Message{
		{ID: "m1", Event: ntfy.EventMessage, Topic: "ops", Time: 100, Message: "one"},
		{ID: "m2", Event: ntfy.EventMessage, Topic: "ops", Time: 200, Message: "two"},
	}
	for _, m := range added {
		if err := store.Add(m); err != nil {
			t.Fatalf("Add(%q) = %v; want no error", m.ID, err)
		}
	}

	if got := startID(store, []string{"ops"}); got != "m2" {
		t.Errorf("startID() = %q; want %q", got, "m2")
	}
}
