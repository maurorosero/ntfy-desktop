// SPDX-License-Identifier: GPL-3.0-or-later

// Package queue answers decision requests one at a time.
//
// Two dialogs at once stack on top of each other, and the first one ends up
// hidden behind the second, so the queue owns the order: it asks one request,
// waits for the answer, runs the chosen action, and only then opens the next
// dialog. Closing a dialog without answering does not throw the request away:
// it goes back to the end of the line, so a person who is busy loses nothing
// and the queue does not reopen the same dialog in a loop. A request that
// nobody answers before its deadline is answered as expired.
//
// The queue reaches the desktop through two ports, Decider and Executor. It
// depends on the contract in internal/dialog, never on kdialog or zenity, so
// the tests run the whole order of requests without opening a window.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// DefaultTTL is how long a request waits for an answer before it is answered as
// expired: long enough for a person to come back to the desk.
const DefaultTTL = 10 * time.Minute

// resultsBuffer keeps the worker from stopping while the caller is busy with a
// previous result. It is not a place to park results: the caller drains it.
const resultsBuffer = 8

// ErrNoActions says the message asks nothing: either it carries no actions or
// none of the kinds the client can run.
var ErrNoActions = errors.New("message has no runnable action")

// Status says how a request ended.
type Status int

const (
	// Answered means a person chose an action. Result.Action carries it, and
	// Result.Err is the failure of running it, when there was one.
	Answered Status = iota
	// Expired means nobody answered before the deadline.
	Expired
	// Unavailable means the dialog could not be asked, or answered something
	// that was never offered.
	Unavailable
)

// String is the status as it appears in a log line.
func (s Status) String() string {
	switch s {
	case Answered:
		return "answered"
	case Expired:
		return "expired"
	case Unavailable:
		return "unavailable"
	default:
		return fmt.Sprintf("status(%d)", int(s))
	}
}

// Result is the end of one request. It is emitted once, whatever happens.
type Result struct {
	Message ntfy.Message
	Action  ntfy.Action // the chosen action; zero unless Status is Answered
	Status  Status
	Err     error // why the action failed, or why it could not be asked
}

// Decider is the dialog the queue drives. It is declared here, where it is
// used; the implementations live in internal/dialog.
type Decider interface {
	Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error)
}

// Executor runs the action a person chose. A nil Executor is allowed: the queue
// then only asks, which is what the wiring does until publishing exists.
type Executor interface {
	Run(ctx context.Context, a ntfy.Action) error
}

// request is one message waiting for an answer, with the actions the client can
// actually run and the instant after which the question is pointless.
type request struct {
	message  ntfy.Message
	actions  []ntfy.Action
	deadline time.Time // zero when the request does not expire
}

// Queue is the line of requests. Submit and Size can be called from any
// goroutine; Run owns the dialogs.
type Queue struct {
	decider  Decider
	executor Executor
	ttl      time.Duration
	results  chan Result
	wake     chan struct{}

	mu      sync.Mutex
	pending []request
	batch   int // requests admitted since the line last ran dry
	shown   int // dialogs opened in this batch, for the "1 of 2" counter
}

// New builds a queue that answers with the given decider and runs the chosen
// action with the given executor. A ttl of zero or less turns the deadline off.
// Use DefaultTTL unless there is a reason not to.
func New(decider Decider, executor Executor, ttl time.Duration) *Queue {
	return &Queue{
		decider:  decider,
		executor: executor,
		ttl:      ttl,
		results:  make(chan Result, resultsBuffer),
		wake:     make(chan struct{}, 1),
	}
}

// Results is where the answers come out. It is closed when Run returns.
func (q *Queue) Results() <-chan Result {
	return q.results
}

// Size is how many requests are waiting, not counting the one being asked.
func (q *Queue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Submit adds a request and returns at once: it never waits for the dialog, so
// the caller can keep reading the stream. It fails with ErrNoActions when the
// message carries nothing this client can run.
func (q *Queue) Submit(m ntfy.Message) error {
	runnable := action.Filter(m.Actions)
	if len(runnable) == 0 {
		return fmt.Errorf("%w: message %q", ErrNoActions, m.ID)
	}

	r := request{message: m, actions: runnable}
	if q.ttl > 0 {
		r.deadline = time.Now().Add(q.ttl)
	}

	q.mu.Lock()
	q.pending = append(q.pending, r)
	q.batch++
	q.mu.Unlock()

	q.signal()
	return nil
}

// Run asks the pending requests until ctx is cancelled, and closes Results on
// the way out. Requests still waiting when ctx ends are dropped with the
// process that would have asked them.
func (q *Queue) Run(ctx context.Context) {
	defer close(q.results)

	for {
		r, pos, total, ok := q.pop()
		if !ok {
			if !q.sleep(ctx) {
				return
			}
			continue
		}
		if !q.serve(ctx, r, pos, total) {
			return
		}
	}
}

// pop takes the oldest request and its place in the batch. A line that runs dry
// starts a new batch, so the counter of the next group begins at one.
func (q *Queue) pop() (r request, pos, total int, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		q.batch, q.shown = 0, 0
		return request{}, 0, 0, false
	}

	r = q.pending[0]
	q.pending = q.pending[1:]
	q.shown++
	total = q.batch
	pos = q.shown
	if pos > total {
		// A request that came back into the line is asked again as the last
		// one of the batch; the counter does not grow past it.
		pos = total
	}
	return r, pos, total, true
}

// serve asks one request and reports whether the queue keeps running.
func (q *Queue) serve(ctx context.Context, r request, pos, total int) bool {
	if q.expired(r) {
		return q.emit(ctx, Result{Message: r.message, Status: Expired})
	}

	askCtx, cancel := ctx, func() {}
	if !r.deadline.IsZero() {
		// The deadline covers the dialog: when it passes, the child process is
		// killed and the request is answered as expired.
		askCtx, cancel = context.WithDeadline(ctx, r.deadline)
	}
	answer, err := q.decider.Ask(dialog.WithPending(askCtx, pos, total), r.message, r.actions)
	cancel()

	switch {
	case err == nil:
		return q.answer(ctx, r, answer)
	case ctx.Err() != nil:
		return false
	case q.expired(r):
		return q.emit(ctx, Result{Message: r.message, Status: Expired})
	case errors.Is(err, dialog.ErrCancelled):
		q.requeue(r)
		if q.Size() == 1 {
			// Only the request that was just closed is left: reopening it at
			// once would turn one "not now" into a loop of dialogs. It waits
			// for the next request to keep it company.
			q.drain()
			return q.sleep(ctx)
		}
		return true
	default:
		return q.emit(ctx, Result{Message: r.message, Status: Unavailable, Err: err})
	}
}

// answer runs the chosen action and reports it once. The label is what the
// dialog printed: it is looked up in the actions that were offered, in order.
func (q *Queue) answer(ctx context.Context, r request, label string) bool {
	chosen, ok := find(r.actions, label)
	if !ok {
		return q.emit(ctx, Result{
			Message: r.message,
			Status:  Unavailable,
			Err:     fmt.Errorf("the dialog answered %q, which was not offered", label),
		})
	}

	res := Result{Message: r.message, Action: chosen, Status: Answered}
	if q.executor != nil {
		res.Err = q.executor.Run(ctx, chosen)
	}
	return q.emit(ctx, res)
}

// requeue puts a request that was not answered back at the end of the line, so
// the dialogs of the others are not held up by one that was closed. The
// counter of the batch does not advance: nothing was answered.
func (q *Queue) requeue(r request) {
	q.mu.Lock()
	q.pending = append(q.pending, r)
	if q.shown > 0 {
		q.shown--
	}
	q.mu.Unlock()
}

// emit hands the result to the caller, and reports false when ctx ended first.
func (q *Queue) emit(ctx context.Context, res Result) bool {
	select {
	case q.results <- res:
		return true
	case <-ctx.Done():
		return false
	}
}

// sleep waits for a request and reports false when ctx ends first.
func (q *Queue) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-q.wake:
		return true
	}
}

// signal wakes the worker without ever blocking: a signal already pending is
// enough, because the worker rechecks the line after every wakeup.
func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// drain drops the signals that arrived while the worker was busy, so the next
// sleep really waits for a request and does not run on a stale wakeup.
func (q *Queue) drain() {
	for {
		select {
		case <-q.wake:
		default:
			return
		}
	}
}

// expired reports whether the deadline of r has passed.
func (q *Queue) expired(r request) bool {
	return !r.deadline.IsZero() && !time.Now().Before(r.deadline)
}

// find returns the action whose label the dialog answered.
func find(actions []ntfy.Action, label string) (ntfy.Action, bool) {
	for _, a := range actions {
		if action.Label(a) == label {
			return a, true
		}
	}
	return ntfy.Action{}, false
}
