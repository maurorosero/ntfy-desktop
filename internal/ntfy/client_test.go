// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

func TestEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		server string
		topics []string
		since  string
		want   string
	}{
		{
			name:   "public server",
			server: "https://ntfy.sh",
			topics: []string{"alerts"},
			want:   "https://ntfy.sh/alerts/json",
		},
		{
			name:   "trailing slash in the server",
			server: "https://ntfy.sh/",
			topics: []string{"alerts"},
			want:   "https://ntfy.sh/alerts/json",
		},
		{
			name:   "server with a path prefix",
			server: "https://example.org/ntfy",
			topics: []string{"builds"},
			want:   "https://example.org/ntfy/builds/json",
		},
		{
			// The comma is the separator of the protocol: it travels literal,
			// not as %2C, or the server would look for one topic with a comma.
			name:   "two topics joined with a literal comma",
			server: "https://ntfy.sh",
			topics: []string{"casa", "pruebas-multi"},
			want:   "https://ntfy.sh/casa,pruebas-multi/json",
		},
		{
			// Each name is escaped and the commas between them are not: a name
			// that needs escaping keeps its own %XX.
			name:   "two topics escape each name and not the separator",
			server: "https://ntfy.sh",
			topics: []string{"a b", "c#d"},
			want:   "https://ntfy.sh/a%20b,c%23d/json",
		},
		{
			name:   "two topics with a path prefix",
			server: "https://example.org/ntfy/",
			topics: []string{"casa", "pruebas"},
			want:   "https://example.org/ntfy/casa,pruebas/json",
		},
		{
			name:   "two topics and since",
			server: "https://ntfy.sh",
			topics: []string{"casa", "pruebas"},
			since:  "hwQ2YpKdmg",
			want:   "https://ntfy.sh/casa,pruebas/json?since=hwQ2YpKdmg",
		},
		{
			name:   "since the last message",
			server: "https://ntfy.sh",
			topics: []string{"alerts"},
			since:  "hwQ2YpKdmg",
			want:   "https://ntfy.sh/alerts/json?since=hwQ2YpKdmg",
		},
		{
			name:   "since with characters that need escaping",
			server: "https://ntfy.sh",
			topics: []string{"alerts"},
			since:  "a b&c",
			want:   "https://ntfy.sh/alerts/json?since=a+b%26c",
		},
		{
			name:   "local server with a port",
			server: "http://localhost:2586",
			topics: []string{"a_b-1"},
			want:   "http://localhost:2586/a_b-1/json",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := New(config.Config{ServerURL: c.server, Topics: c.topics})
			got, err := client.endpoint(c.since)
			if err != nil {
				t.Fatalf("endpoint(%q) = error %v; want no error", c.since, err)
			}
			if got != c.want {
				t.Errorf("endpoint(%q) with server %q and topics %q = %q; want %q",
					c.since, c.server, c.topics, got, c.want)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		name    string
		base    time.Duration
		attempt int
		want    time.Duration
	}{
		{name: "first retry", base: time.Second, attempt: 0, want: time.Second},
		{name: "second retry", base: time.Second, attempt: 1, want: 2 * time.Second},
		{name: "third retry", base: time.Second, attempt: 2, want: 4 * time.Second},
		{name: "fifth retry", base: time.Second, attempt: 4, want: 16 * time.Second},
		{name: "before the cap", base: time.Second, attempt: 5, want: 32 * time.Second},
		{name: "at the cap", base: time.Second, attempt: 6, want: maxWait},
		{name: "past the cap", base: time.Second, attempt: 20, want: maxWait},
		{name: "absurd attempt does not overflow", base: time.Second, attempt: 10_000, want: maxWait},
		{name: "negative attempt counts as the first", base: time.Second, attempt: -3, want: time.Second},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backoff(c.base, c.attempt); got != c.want {
				t.Errorf("backoff(%v, %d) = %v; want %v", c.base, c.attempt, got, c.want)
			}
		})
	}
}

func TestSubscribeDeliversMessages(t *testing.T) {
	const (
		open = `{"id":"","time":1,"event":"open","topic":"alerts"}`
		m1   = `{"id":"m1","time":2,"event":"message","topic":"alerts","title":"Deploy","message":"one","priority":4,"tags":["rocket"]}`
		m2   = `{"id":"m2","time":3,"event":"message","topic":"alerts","message":"two"}`
		m3   = `{"id":"m3","time":4,"event":"message","topic":"alerts","message":"three"}`
	)

	type request struct {
		path, accept string
	}
	probe := make(chan request, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case probe <- request{path: r.URL.Path, accept: r.Header.Get("Accept")}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		for _, line := range []string{open, m1, m2, m3} {
			fmt.Fprintln(w, line)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := newClient(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, time.Hour)
	msgs, errs := client.Subscribe(ctx)

	var got []Message
	for len(got) < 4 {
		select {
		case m, ok := <-msgs:
			if !ok {
				t.Fatalf("the message channel closed after %d events; want 4", len(got))
			}
			got = append(got, m)
		case err := <-errs:
			t.Fatalf("unexpected error: %v", err)
		case <-ctx.Done():
			t.Fatalf("timed out after %d events; want 4", len(got))
		}
	}

	if got[0].Event != EventOpen {
		t.Errorf("first event = %q; want %q", got[0].Event, EventOpen)
	}
	want := Message{
		ID:       "m1",
		Time:     2,
		Event:    EventMessage,
		Topic:    "alerts",
		Message:  "one",
		Title:    "Deploy",
		Priority: 4,
		Tags:     []string{"rocket"},
	}
	if want := []string{"m1", "m2", "m3"}; !slices.Equal(ids(got), want) {
		t.Errorf("message ids = %v; want %v", ids(got), want)
	}
	if got[1].Message != want.Message || got[1].Title != want.Title || got[1].Priority != want.Priority {
		t.Errorf("first message = %+v; want %+v", got[1], want)
	}
	if !slices.Equal(got[1].Tags, want.Tags) {
		t.Errorf("first message tags = %v; want %v", got[1].Tags, want.Tags)
	}

	select {
	case req := <-probe:
		if want := "/alerts/json"; req.path != want {
			t.Errorf("request path = %q; want %q", req.path, want)
		}
		if want := "application/json"; req.accept != want {
			t.Errorf("Accept header = %q; want %q", req.accept, want)
		}
	case <-ctx.Done():
		t.Fatal("the server did not see the request")
	}
}

// TestSubscribeReconnectsWithSince is the acceptance criterion: the server
// sends three messages and cuts the connection, the client comes back asking
// for everything after the last one and does not repeat what it already read.
func TestSubscribeReconnectsWithSince(t *testing.T) {
	const (
		open = `{"id":"","time":1,"event":"open","topic":"alerts"}`
		m1   = `{"id":"m1","time":2,"event":"message","topic":"alerts","message":"one"}`
		m2   = `{"id":"m2","time":3,"event":"message","topic":"alerts","message":"two"}`
		m3   = `{"id":"m3","time":4,"event":"message","topic":"alerts","message":"three"}`
	)

	var (
		mu    sync.Mutex
		since []string
	)
	reconnected := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		since = append(since, r.URL.Query().Get("since"))
		attempt := len(since)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		flush := w.(http.Flusher)
		if attempt == 1 {
			for _, line := range []string{open, m1, m2, m3} {
				fmt.Fprintln(w, line)
			}
			flush.Flush()
			return // the server cuts the connection
		}

		once.Do(func() { close(reconnected) })
		fmt.Fprintln(w, open)
		flush.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := newClient(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, 5*time.Millisecond)
	msgs, errs := client.Subscribe(ctx)

	var got []Message
	for len(ids(got)) < 3 {
		select {
		case m, ok := <-msgs:
			if !ok {
				t.Fatalf("the message channel closed after %v; want the three messages", ids(got))
			}
			got = append(got, m)
		case err := <-errs:
			t.Fatalf("unexpected error: %v", err)
		case <-ctx.Done():
			t.Fatalf("timed out after %v; want the three messages", ids(got))
		}
	}

	select {
	case <-reconnected:
	case <-ctx.Done():
		t.Fatalf("the client did not reconnect after the server closed the connection")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(since) < 2 {
		t.Fatalf("requests = %d; want at least two", len(since))
	}
	if since[0] != "" {
		t.Errorf("first request since = %q; want no since on the first connection", since[0])
	}
	if since[1] != "m3" {
		t.Errorf("second request since = %q; want %q", since[1], "m3")
	}
	if want := []string{"m1", "m2", "m3"}; !slices.Equal(ids(got), want) {
		t.Errorf("message ids = %v; want %v, without repeating what was already read", ids(got), want)
	}
}

func TestSubscribeAuth(t *testing.T) {
	cases := []struct {
		name string
		user string
		pass string
	}{
		{name: "anonymous", user: "", pass: ""},
		{name: "user and password", user: "andrea", pass: "s3cr3t"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			type seen struct {
				path, auth string
			}
			requests := make(chan seen, 1)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- seen{path: r.URL.Path, auth: r.Header.Get("Authorization")}:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintln(w, `{"id":"","time":1,"event":"open","topic":"alerts"}`)
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cfg := config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}, User: c.user, Password: c.pass}
			// A long wait keeps the client on its first connection.
			msgs, errs := newClient(cfg, time.Hour).Subscribe(ctx)
			select {
			case <-msgs:
			case err := <-errs:
				t.Fatalf("unexpected error: %v", err)
			case <-ctx.Done():
				t.Fatal("timed out waiting for the open event")
			}

			var got seen
			select {
			case got = <-requests:
			case <-ctx.Done():
				t.Fatal("the server did not see the request")
			}

			if want := "/alerts/json"; got.path != want {
				t.Errorf("request path = %q; want %q", got.path, want)
			}
			want := ""
			if c.user != "" {
				want = "Basic " + base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass))
			}
			if got.auth != want {
				t.Errorf("Authorization header = %q; want %q", got.auth, want)
			}
		})
	}
}

func TestSubscribeReportsRejectedCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}, User: "andrea", Password: "s3cr3t"}
	_, errs := newClient(cfg, 5*time.Millisecond).Subscribe(ctx)

	select {
	case err := <-errs:
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error %v is %T; want *StatusError", err, err)
		}
		if statusErr.Code != http.StatusUnauthorized {
			t.Errorf("StatusError.Code = %d; want %d", statusErr.Code, http.StatusUnauthorized)
		}
		if strings.Contains(err.Error(), cfg.Password) {
			t.Errorf("error %q exposes the password", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the error of the failed attempt")
	}
}

func TestSubscribeReportsServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, errs := newClient(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, 5*time.Millisecond).Subscribe(ctx)

	select {
	case err := <-errs:
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error %q; want the status of the server", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the error of the failed attempt")
	}
}

func TestSubscribeStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"","time":1,"event":"open","topic":"alerts"}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := newClient(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, time.Hour)
	msgs, errs := client.Subscribe(ctx)

	select {
	case <-msgs:
	case <-time.After(5 * time.Second):
		t.Fatal("the open event never arrived")
	}

	cancel()

	if !closedWithin(t, msgs, 5*time.Second) {
		t.Error("the message channel is still open after cancelling the context")
	}
	if !closedWithin(t, errs, 5*time.Second) {
		t.Error("the error channel is still open after cancelling the context")
	}
}

func TestSubscribeStopsWhenTheCallerDoesNotRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for range 100 {
			fmt.Fprintln(w, `{"id":"a","time":1,"event":"message","topic":"alerts","message":"m"}`)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Nobody reads the messages: the client must block on the send, not grow a
	// queue, and it must exit when the context ends.
	msgs, _ := newClient(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, time.Hour).Subscribe(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	if !closedWithin(t, msgs, 5*time.Second) {
		t.Error("the message channel is still open after cancelling the context")
	}
}

// ids returns the identifiers of the events that are real messages.
func ids(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.IsMessage() {
			out = append(out, m.ID)
		}
	}
	return out
}

// closedWithin drains ch and reports whether it got closed before the timeout.
func closedWithin[T any](t *testing.T, ch <-chan T, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
