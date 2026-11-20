// SPDX-License-Identifier: GPL-3.0-or-later

package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// step is what the fake dialog does the nth time it is asked.
type step struct {
	answer string // label to return
	close  bool   // the person closed the dialog
	wait   bool   // nobody answers: wait for the deadline
	fail   error  // the tool itself failed
}

// fakeDialog answers from a script and records what it was asked. It never
// opens a window.
type fakeDialog struct {
	steps  []step
	notify chan struct{}

	mu      sync.Mutex
	asked   []string
	open    int
	maxOpen int
}

func (f *fakeDialog) Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error) {
	pos, total := dialog.Pending(ctx)

	f.mu.Lock()
	f.open++
	if f.open > f.maxOpen {
		f.maxOpen = f.open
	}
	f.asked = append(f.asked, fmt.Sprintf("%s %d/%d", m.ID, pos, total))
	index := len(f.asked) - 1
	var s step
	if index < len(f.steps) {
		s = f.steps[index]
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.open--
		f.mu.Unlock()
	}()

	select {
	case f.notify <- struct{}{}:
	default:
	}

	if s.wait {
		<-ctx.Done()
		return "", ctx.Err()
	}
	// A dialog stays open long enough for two of them to overlap if the queue
	// ever asked in parallel.
	time.Sleep(time.Millisecond)

	switch {
	case s.fail != nil:
		return "", s.fail
	case s.close:
		return "", dialog.ErrCancelled
	default:
		return s.answer, nil
	}
}

func (f *fakeDialog) askedAs() (asked []string, maxOpen int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...), f.maxOpen
}

// fakeExecutor records the actions the queue ran.
type fakeExecutor struct {
	err   error
	mu    sync.Mutex
	ran   []string
	calls int
}

func (e *fakeExecutor) Run(ctx context.Context, a ntfy.Action) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.ran = append(e.ran, action.Label(a))
	return e.err
}

func (e *fakeExecutor) summary() (ran []string, calls int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.ran...), e.calls
}

// message builds a message with one view action per label.
func message(id string, labels ...string) ntfy.Message {
	m := ntfy.Message{
		ID:      id,
		Event:   ntfy.EventMessage,
		Topic:   "decisions",
		Title:   "Title " + id,
		Message: "Body " + id,
	}
	for _, label := range labels {
		m.Actions = append(m.Actions, ntfy.Action{
			Action: action.View,
			Label:  label,
			URL:    "https://example.org/" + label,
		})
	}
	return m
}

func TestQueueOrder(t *testing.T) {
	cases := []struct {
		name  string
		ttl   time.Duration
		msgs  []ntfy.Message
		steps []step
		want  []string // "<id> <status>" or "<id> answered:<label>"
		asked []string // what the dialog saw, in order
		runs  []string // what the executor ran, in order
	}{
		{
			name: "two arrive and are answered in order",
			msgs: []ntfy.Message{message("A", "Approve", "Deny"), message("B", "Approve", "Deny")},
			steps: []step{
				{answer: "Approve"},
				{answer: "Deny"},
			},
			want:  []string{"A answered:Approve", "B answered:Deny"},
			asked: []string{"A 1/2", "B 2/2"},
			runs:  []string{"Approve", "Deny"},
		},
		{
			name: "three arrive and the first is closed",
			msgs: []ntfy.Message{message("A", "Approve"), message("B", "Approve"), message("C", "Approve")},
			steps: []step{
				{close: true}, // the person walks away: A goes back to the line
				{answer: "Approve"},
				{answer: "Approve"},
				{answer: "Approve"},
			},
			want:  []string{"B answered:Approve", "C answered:Approve", "A answered:Approve"},
			asked: []string{"A 1/3", "B 1/3", "C 2/3", "A 3/3"},
			runs:  []string{"Approve", "Approve", "Approve"},
		},
		{
			name:  "nobody answers before the deadline",
			ttl:   30 * time.Millisecond,
			msgs:  []ntfy.Message{message("A", "Approve")},
			steps: []step{{wait: true}},
			want:  []string{"A expired"},
			asked: []string{"A 1/1"},
		},
		{
			name:  "the deadline catches the request that was waiting",
			ttl:   30 * time.Millisecond,
			msgs:  []ntfy.Message{message("A", "Approve"), message("B", "Approve")},
			steps: []step{{wait: true}},
			want:  []string{"A expired", "B expired"},
			asked: []string{"A 1/2"},
		},
		{
			name:  "the dialog itself fails",
			msgs:  []ntfy.Message{message("A", "Approve")},
			steps: []step{{fail: errors.New("kdialog is not installed")}},
			want:  []string{"A unavailable"},
			asked: []string{"A 1/1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dial := &fakeDialog{
				steps:  c.steps,
				notify: make(chan struct{}, len(c.steps)+2),
			}
			exec := &fakeExecutor{}
			ttl := c.ttl
			if ttl == 0 {
				ttl = 2 * time.Second
			}
			q := New(dial, exec, ttl)

			for _, m := range c.msgs {
				if err := q.Submit(m); err != nil {
					t.Fatalf("Submit(%q) = %v, quiero nil", m.ID, err)
				}
			}

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				q.Run(ctx)
			}()

			got := collect(t, q.Results(), len(c.want))
			cancel()
			<-done

			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("resultados = %v, quiero %v", got, c.want)
			}

			asked, maxOpen := dial.askedAs()
			if fmt.Sprint(asked) != fmt.Sprint(c.asked) {
				t.Errorf("diálogos pedidos = %v, quiero %v", asked, c.asked)
			}
			if maxOpen != 1 {
				t.Errorf("se abrieron %d diálogos a la vez, quiero 1", maxOpen)
			}

			ran, calls := exec.summary()
			if fmt.Sprint(ran) != fmt.Sprint(c.runs) {
				t.Errorf("acciones ejecutadas = %v, quiero %v", ran, c.runs)
			}
			if calls != len(c.runs) {
				t.Errorf("se ejecutaron %d acciones, quiero %d", calls, len(c.runs))
			}
		})
	}
}

// collect reads n results and fails when the queue stops delivering.
func collect(t *testing.T, results <-chan Result, n int) []string {
	t.Helper()

	out := make([]string, 0, n)
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case res, ok := <-results:
			if !ok {
				t.Fatalf("el canal se cerró con %d resultados, quiero %d", len(out), n)
			}
			out = append(out, describe(res))
		case <-deadline:
			t.Fatalf("esperaba %d resultados, llegaron %d: %v", n, len(out), out)
		}
	}
	return out
}

// describe is the shape the table compares.
func describe(res Result) string {
	if res.Status == Answered && res.Action.Label != "" {
		return fmt.Sprintf("%s %s:%s", res.Message.ID, res.Status, res.Action.Label)
	}
	return fmt.Sprintf("%s %s", res.Message.ID, res.Status)
}

func TestQueueNeverAsksTwoAtOnce(t *testing.T) {
	dial := &fakeDialog{
		steps:  []step{{wait: true}},
		notify: make(chan struct{}, 4),
	}
	q := New(dial, &fakeExecutor{}, 0)

	for i := 0; i < 3; i++ {
		if err := q.Submit(message(fmt.Sprintf("M%d", i), "Approve")); err != nil {
			t.Fatalf("Submit = %v, quiero nil", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()

	select {
	case <-dial.notify:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("el diálogo no se abrió")
	}

	if got := q.Size(); got != 2 {
		t.Errorf("Size() = %d con un diálogo abierto, quiero 2", got)
	}

	cancel()
	<-done
}

func TestQueueRunsTheActionOnce(t *testing.T) {
	dial := &fakeDialog{
		steps:  []step{{answer: "Deny"}},
		notify: make(chan struct{}, 2),
	}
	exec := &fakeExecutor{}
	q := New(dial, exec, 0)

	if err := q.Submit(message("A", "Approve", "Deny")); err != nil {
		t.Fatalf("Submit = %v, quiero nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()

	got := collect(t, q.Results(), 1)
	cancel()
	<-done

	if want := "A answered:Deny"; got[0] != want {
		t.Errorf("resultado = %q, quiero %q", got[0], want)
	}
	if _, calls := exec.summary(); calls != 1 {
		t.Errorf("la acción se ejecutó %d veces, quiero 1", calls)
	}
}

// TestClosingTheOnlyRequestDoesNotLoop covers the case the counter exists for:
// one request, closed once, must wait for company instead of reopening itself.
func TestClosingTheOnlyRequestDoesNotLoop(t *testing.T) {
	dial := &fakeDialog{
		steps:  []step{{close: true}, {answer: "Approve"}, {answer: "Approve"}},
		notify: make(chan struct{}, 4),
	}
	q := New(dial, &fakeExecutor{}, 0)

	if err := q.Submit(message("A", "Approve")); err != nil {
		t.Fatalf("Submit(A) = %v, quiero nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()

	select {
	case <-dial.notify: // the first dialog was opened and closed
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("el diálogo no se abrió")
	}

	select {
	case <-dial.notify:
		cancel()
		t.Fatal("el diálogo se reabrió solo después de cerrarlo")
	case <-time.After(100 * time.Millisecond):
	}

	if got := q.Size(); got != 1 {
		t.Errorf("Size() = %d con la petición cerrada en la cola, quiero 1", got)
	}

	if err := q.Submit(message("B", "Approve")); err != nil {
		t.Fatalf("Submit(B) = %v, quiero nil", err)
	}

	got := collect(t, q.Results(), 2)
	cancel()
	<-done

	want := []string{"A answered:Approve", "B answered:Approve"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("resultados = %v, quiero %v", got, want)
	}
}

func TestSubmitRejectsMessagesWithNothingToRun(t *testing.T) {
	cases := []struct {
		name string
		msg  ntfy.Message
	}{
		{name: "no actions", msg: ntfy.Message{ID: "A", Event: ntfy.EventMessage}},
		{
			name: "only kinds the client does not run",
			msg: ntfy.Message{
				ID:    "B",
				Event: ntfy.EventMessage,
				Actions: []ntfy.Action{
					{Action: "broadcast", Label: "Other device", URL: "topic"},
					{Action: "copy", Label: "Copy", URL: "text"},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := New(&fakeDialog{}, nil, 0)
			if err := q.Submit(c.msg); !errors.Is(err, ErrNoActions) {
				t.Errorf("Submit() = %v, quiero ErrNoActions", err)
			}
			if got := q.Size(); got != 0 {
				t.Errorf("Size() = %d después de rechazar, quiero 0", got)
			}
		})
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		Answered:    "answered",
		Expired:     "expired",
		Unavailable: "unavailable",
		Status(9):   "status(9)",
	}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q, quiero %q", int(status), got, want)
		}
	}
}

// TestSettleCountsTheBurst covers the counter of the title: three messages that
// arrive together must be asked as "1 of 3", not "1 of 1", so the person knows
// two more are waiting behind the first dialog.
func TestSettleCountsTheBurst(t *testing.T) {
	dial := &fakeDialog{steps: []step{{answer: "Aprobar"}, {answer: "Aprobar"}, {answer: "Aprobar"}}}
	q := New(dial, &fakeExecutor{}, 0).WithSettle(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	for i := 1; i <= 3; i++ {
		if err := q.Submit(message(fmt.Sprintf("m%d", i), "Aprobar")); err != nil {
			t.Fatalf("Submit(%d) = %v", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		select {
		case <-q.Results():
		case <-time.After(2 * time.Second):
			t.Fatalf("la cola no emitió el resultado %d", i+1)
		}
	}

	dial.mu.Lock()
	defer dial.mu.Unlock()
	want := []string{"m1 1/3", "m2 2/3", "m3 3/3"}
	if len(dial.asked) != len(want) {
		t.Fatalf("preguntó %q, quiero %q", dial.asked, want)
	}
	for i := range want {
		if dial.asked[i] != want[i] {
			t.Errorf("pregunta %d = %q, quiero %q", i+1, dial.asked[i], want[i])
		}
	}
}

// TestSettleOffOpensAtOnce keeps the default: without a settle window the first
// dialog of a batch opens with the count it had when it was popped.
func TestSettleOffOpensAtOnce(t *testing.T) {
	dial := &fakeDialog{steps: []step{{answer: "Aprobar"}}}
	q := New(dial, &fakeExecutor{}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	if err := q.Submit(message("solo", "Aprobar")); err != nil {
		t.Fatalf("Submit = %v", err)
	}
	select {
	case <-q.Results():
	case <-time.After(2 * time.Second):
		t.Fatal("la cola no emitió el resultado")
	}

	dial.mu.Lock()
	defer dial.mu.Unlock()
	if len(dial.asked) != 1 || dial.asked[0] != "solo 1/1" {
		t.Errorf("preguntó %q, quiero [\"solo 1/1\"]", dial.asked)
	}
}

