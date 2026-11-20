// SPDX-License-Identifier: GPL-3.0-or-later

// Package action runs the action a person picked in a decision dialog: it
// opens a link in the session's browser, or calls a URL with the method, the
// headers and the body the sender declared.
//
// It knows nothing about dialogs or queues: it receives one action and either
// runs it or says why it cannot. The two kinds ntfy defines for a client are
// supported; "broadcast" and "copy" are out of scope, and Runnable tells the
// caller so before anything is offered to the user.
package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

const (
	// View opens a URL in the browser. The URL is what travels in the message.
	View = "view"
	// HTTP calls a URL: this is how a sender gets an answer back.
	HTTP = "http"

	// opener is the desktop's own launcher, present in KDE and GNOME.
	opener = "xdg-open"
	// callTimeout bounds one call; a sender's endpoint must not hang the queue.
	callTimeout = 15 * time.Second
	// drainLimit is how much of an answer is read before closing it, so the
	// connection can be reused without trusting the body's length.
	drainLimit = 4 << 10
)

var (
	// ErrUnsupported says the client does not know how to run the action.
	ErrUnsupported = errors.New("unsupported action")
	// ErrUnsafeURL says the URL is not http or https. Opening any other scheme
	// would hand a URL chosen by the sender to a local handler.
	ErrUnsafeURL = errors.New("url must be http or https")
)

// Label is the text a dialog shows for a: the sender's label, or the kind of
// action when the sender left it out.
func Label(a ntfy.Action) string {
	if a.Label != "" {
		return a.Label
	}
	return a.Action
}

// Runnable reports whether the client knows how to run a.
func Runnable(a ntfy.Action) bool {
	switch a.Action {
	case View, HTTP:
		return true
	default:
		return false
	}
}

// Filter returns the actions the client can run, in the order they came. It
// returns nil when there is nothing to offer.
func Filter(actions []ntfy.Action) []ntfy.Action {
	var out []ntfy.Action
	for _, a := range actions {
		if Runnable(a) {
			out = append(out, a)
		}
	}
	return out
}

// Runner runs one action. Calls go through its own client, with a deadline;
// links are handed to the desktop opener as a single argument, never to a shell.
type Runner struct {
	http *http.Client
	open func(ctx context.Context, target string) error
}

// New builds the runner the program uses.
func New() *Runner {
	return &Runner{
		http: &http.Client{Timeout: callTimeout},
		open: openWithDesktop,
	}
}

// Run carries out a. A failure to run the action at all wraps ErrUnsupported
// or ErrUnsafeURL, so the caller can tell it apart from a call that failed.
func (r *Runner) Run(ctx context.Context, a ntfy.Action) error {
	if !Runnable(a) {
		return fmt.Errorf("%w: %q", ErrUnsupported, a.Action)
	}
	target, err := checkURL(a.URL)
	if err != nil {
		return err
	}
	if a.Action == View {
		return r.open(ctx, target)
	}
	return r.call(ctx, a, target)
}

// call sends the HTTP action and treats a 4xx or 5xx answer as a failure: the
// sender asked for something that did not happen.
func (r *Runner) call(ctx context.Context, a ntfy.Action, target string) error {
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = http.MethodPost // ntfy's default when the action omits it
	}
	if !allowedMethod(method) {
		return fmt.Errorf("%w: method %q", ErrUnsupported, a.Method)
	}

	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("building the action request: %w", err)
	}
	for name, value := range a.Headers {
		req.Header.Set(name, value)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", target, err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit)); err != nil {
		return fmt.Errorf("reading the answer of %s: %w", target, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	return nil
}

// checkURL keeps the two schemes a browser and an HTTP call can use.
func checkURL(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", fmt.Errorf("%w: empty url", ErrUnsafeURL)
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("parsing the action url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: %q", ErrUnsafeURL, raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: %q", ErrUnsafeURL, raw)
	}
	return u.String(), nil
}

// allowedMethod limits the call to the methods an ntfy action uses in practice.
func allowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead:
		return true
	default:
		return false
	}
}

// openWithDesktop hands the URL to the session's launcher.
func openWithDesktop(ctx context.Context, target string) error {
	if err := exec.CommandContext(ctx, opener, target).Run(); err != nil {
		return fmt.Errorf("opening %s: %w", target, err)
	}
	return nil
}

// StatusError is an answer the client cannot accept. It keeps the status and
// never the URL's query, which may carry a token.
type StatusError struct {
	Code   int
	Status string // as the server wrote it, e.g. "403 Forbidden"
}

func (e *StatusError) Error() string {
	if e.Status == "" {
		return fmt.Sprintf("the action answered %d", e.Code)
	}
	return fmt.Sprintf("the action answered %s", e.Status)
}
