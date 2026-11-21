// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

const (
	// pathJSON is the endpoint that streams one JSON object per line.
	pathJSON = "json"
	// initialWait is how long the client waits before the first retry; the wait
	// doubles from there.
	initialWait = time.Second
	// maxWait caps the wait between attempts, so a dead server is retried once
	// a minute and not once an hour.
	maxWait = 60 * time.Second
	// headerWait bounds the time to get the response headers: a stream has no
	// total deadline, but connecting to a silent server must not hang forever.
	headerWait = 15 * time.Second
)

// Client subscribes to the topic of one configuration.
type Client struct {
	cfg   config.Config
	http  *http.Client
	base  time.Duration // first wait between retries; New sets initialWait
	since string        // where the first connection resumes from; empty is the live stream
}

// Option configures the client while it is built.
type Option func(*Client)

// WithSince makes the first connection resume after the message id, so what
// arrived while the program was not running is read back from the server. The
// id normally comes from the local history.
//
// The server keeps its cache for a limited time, so the id may be one it no
// longer holds. A first attempt refused because of it (a 400 or a 404) is
// answered by subscribing to the live stream instead: asking for the same
// missing message on every retry would leave the client deaf for good.
func WithSince(id string) Option {
	id = strings.TrimSpace(id)
	return func(c *Client) { c.since = id }
}

// New builds the client for a configuration that config.Load already validated.
func New(cfg config.Config, opts ...Option) *Client {
	return newClient(cfg, initialWait, opts...)
}

// newClient exists so the tests can shrink the wait between retries: a test
// that reconnects must not sleep a second, and the policy is the same.
func newClient(cfg config.Config, base time.Duration, opts ...Option) *Client {
	c := &Client{
		cfg:  cfg,
		base: base,
		http: &http.Client{
			// No Client.Timeout: the response is a stream that lives for hours,
			// so a total deadline would cut it. The header timeout covers the
			// connection and the ctx passed to Subscribe bounds the rest.
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ResponseHeaderTimeout: headerWait,
				IdleConnTimeout:       90 * time.Second,
				ForceAttemptHTTP2:     true,
			},
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Subscribe opens the stream and returns the events and the reasons of the
// failed attempts. Both channels stay open until ctx is cancelled, and both are
// closed when the goroutine that feeds them exits, so no goroutine survives the
// cancellation. The error channel is never dropped on the floor by the client:
// the caller decides whether a failed attempt is worth showing.
func (c *Client) Subscribe(ctx context.Context) (<-chan Message, <-chan error) {
	msgs := make(chan Message)
	errs := make(chan error, 1)
	go func() {
		defer close(msgs)
		defer close(errs)
		c.run(ctx, msgs, errs)
	}()
	return msgs, errs
}

// run is the loop: subscribe, report what went wrong, wait, subscribe again
// asking for everything after the last message seen.
func (c *Client) run(ctx context.Context, msgs chan<- Message, errs chan<- error) {
	since := c.since
	// fromHistory marks the since as the caller's. Only that one is dropped
	// when the server refuses it: an id seen on this very connection is one
	// the cache is holding right now.
	fromHistory := since != ""
	attempt := 0
	for {
		last, healthy, err := c.stream(ctx, since, msgs)
		if last != "" {
			since = last
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !emit(ctx, errs, err) {
				return
			}
			if fromHistory && refusedSince(err) {
				since, fromHistory = "", false
			}
		}
		fromHistory = false
		if ctx.Err() != nil {
			return
		}

		// A stream that delivered something resets the wait: the server is
		// there, the next failure starts the escalation over.
		if healthy {
			attempt = 0
		}
		wait := backoff(c.base, attempt)
		attempt++
		if !sleep(ctx, wait) {
			return
		}
	}
}

// stream reads one connection until the server closes it or ctx is cancelled.
// It returns the last message identifier seen on a "message" event, whether the
// connection delivered anything, and the reason it ended.
func (c *Client) stream(ctx context.Context, since string, msgs chan<- Message) (last string, healthy bool, err error) {
	endpoint, err := c.endpoint(since)
	if err != nil {
		return "", false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, fmt.Errorf("building the subscription request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.AuthMode() == config.AuthBasic {
		req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("subscribing to %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var m Message
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return last, healthy, nil
			}
			return last, healthy, fmt.Errorf("reading the stream: %w", err)
		}
		if m.IsMessage() && m.ID != "" {
			last = m.ID
		}
		healthy = true
		if !emit(ctx, msgs, m) {
			return last, healthy, ctx.Err()
		}
	}
}

// endpoint builds the JSON stream URL: <server>/<topic>/json, with since=<id>
// once there is something to resume from.
//
// Several topics travel as one path segment joined with a comma, which is the
// separator of the protocol: each name is escaped and the commas between them
// are not. Escaping the joined string would turn the separator into %2C and the
// server would read one topic with a comma in its name.
func (c *Client) endpoint(since string) (string, error) {
	u, err := url.Parse(c.cfg.ServerURL)
	if err != nil {
		return "", fmt.Errorf("parsing the server url: %w", err)
	}
	base := strings.TrimSuffix(u.Path, "/")
	u.Path = base + "/" + strings.Join(c.cfg.Topics, ",") + "/" + pathJSON
	// RawPath keeps the escaped names: url would escape the separators too, and
	// Path alone would lose the escaping of a name that needs it.
	u.RawPath = (&url.URL{Path: base}).EscapedPath() + "/" + escapedTopics(c.cfg.Topics) + "/" + pathJSON
	if since != "" {
		q := u.Query()
		q.Set("since", since)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// escapedTopics escapes each topic name, so the names can be joined with the
// comma the protocol expects.
func escapedTopics(topics []string) string {
	names := make([]string, len(topics))
	for i, topic := range topics {
		names[i] = url.PathEscape(topic)
	}
	return strings.Join(names, ",")
}

// StatusError is a response the client cannot use. It keeps the status and
// never the credentials: what goes to a log or to the user is this type.
type StatusError struct {
	Code   int
	Status string // as the server wrote it, e.g. "401 Unauthorized"
}

// refusedSince reports whether the answer is the server rejecting the resume
// point rather than being unreachable: a cache that no longer holds the id
// answers a bad request or a not found.
func refusedSince(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return false
	}
	return status.Code == http.StatusBadRequest || status.Code == http.StatusNotFound
}

func (e *StatusError) Error() string {
	if e.Status == "" {
		return fmt.Sprintf("ntfy answered %d", e.Code)
	}
	return fmt.Sprintf("ntfy answered %s", e.Status)
}

// backoff returns the wait before the given attempt, counting from zero:
// base, 2*base, 4*base, doubled until maxWait.
func backoff(base time.Duration, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	wait := base
	for i := 0; i < attempt; i++ {
		wait *= 2
		if wait >= maxWait {
			return maxWait
		}
	}
	if wait > maxWait {
		return maxWait
	}
	return wait
}

// sleep waits for d, and returns false when ctx is cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// emit sends v unless ctx is cancelled first, which is what keeps a blocked
// send from outliving the caller.
func emit[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
}
