// SPDX-License-Identifier: GPL-3.0-or-later

package notify

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	dbusnotify "github.com/esiqveland/notify"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// fakeSender stands in for the notification service: it records what would
// have gone over the bus and returns whatever the test told it to return.
type fakeSender struct {
	mu   sync.Mutex
	err  error
	sent []dbusnotify.Notification
}

func (f *fakeSender) SendNotification(n dbusnotify.Notification) (uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.sent = append(f.sent, n)
	return uint32(len(f.sent)), nil
}

// fail makes the next sends fail, the way a missing service does.
func (f *fakeSender) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// heal makes the sender work again, to prove the notifier recovers.
func (f *fakeSender) heal() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeSender) last(t *testing.T) dbusnotify.Notification {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("nothing was sent to the notification service")
	}
	return f.sent[len(f.sent)-1]
}

// fakeOpener records the URLs the desktop would have opened.
type fakeOpener struct {
	urls chan string
	mu   sync.Mutex
	err  error
}

func newFakeOpener() *fakeOpener {
	return &fakeOpener{urls: make(chan string, 4)}
}

func (o *fakeOpener) Open(_ context.Context, rawURL string) error {
	o.mu.Lock()
	err := o.err
	o.mu.Unlock()
	o.urls <- rawURL
	return err
}

// capture is a slog handler that keeps the records, so a test can assert that
// a failure was logged and not swallowed.
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

// warned reports whether anything was logged at warning level or above.
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

// newTestNotifier builds the notifier the way New would, but with doubles: no
// test opens a D-Bus connection.
func newTestNotifier(t *testing.T, s sender, o opener, tr *i18n.Translator) (*DBus, *capture) {
	t.Helper()
	logs := &capture{}
	if tr == nil {
		tr = i18n.New("en")
	}
	return &DBus{
		send:    s,
		open:    o,
		log:     slog.New(logs),
		tr:      tr,
		sending: make(chan struct{}, 1),
		targets: make(map[uint32]target),
	}, logs
}

func TestUrgencyFor(t *testing.T) {
	cases := []struct {
		name     string
		priority int
		want     dbusnotify.Urgency
	}{
		{"unset is normal", 0, dbusnotify.UrgencyNormal},
		{"1 min is low", 1, dbusnotify.UrgencyLow},
		{"2 low is low", 2, dbusnotify.UrgencyLow},
		{"3 default is normal", 3, dbusnotify.UrgencyNormal},
		{"4 high is critical", 4, dbusnotify.UrgencyCritical},
		{"5 max is critical", 5, dbusnotify.UrgencyCritical},
		{"negative is normal", -3, dbusnotify.UrgencyNormal},
		{"above the range is critical", 9, dbusnotify.UrgencyCritical},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := urgencyFor(c.priority); got != c.want {
				t.Errorf("urgencyFor(%d) = %d; want %d", c.priority, got, c.want)
			}
		})
	}
}

func TestTimeoutFor(t *testing.T) {
	cases := []struct {
		name     string
		priority int
		want     time.Duration
	}{
		{"low", 1, lowTimeout},
		{"low again", 2, lowTimeout},
		{"normal", 3, normalTimeout},
		{"unset", 0, normalTimeout},
		{"critical", 4, criticalTimeout},
		{"max", 5, criticalTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := timeoutFor(c.priority); got != c.want {
				t.Errorf("timeoutFor(%d) = %v; want %v", c.priority, got, c.want)
			}
		})
	}
}

// TestTimeoutNeverZero is the invariant the design depends on: Plasma closes
// the bubble at once when the timeout is zero.
func TestTimeoutNeverZero(t *testing.T) {
	for p := -10; p <= 20; p++ {
		got := timeoutFor(p)
		if got <= 0 {
			t.Errorf("timeoutFor(%d) = %v; want a positive duration", p, got)
		}
		if got < 30*time.Second || got > 60*time.Second {
			t.Errorf("timeoutFor(%d) = %v; want it between 30s and 60s", p, got)
		}
	}
}

func TestCompose(t *testing.T) {
	cases := []struct {
		name        string
		msg         ntfy.Message
		wantSummary string
		wantBody    string
		wantUrgency dbusnotify.Urgency
		wantTimeout time.Duration
	}{
		{
			name:        "title wins over topic",
			msg:         ntfy.Message{Title: "Deploy", Topic: "ops", Message: "done", Priority: 5},
			wantSummary: "Deploy",
			wantBody:    "done",
			wantUrgency: dbusnotify.UrgencyCritical,
			wantTimeout: 60 * time.Second,
		},
		{
			name:        "no title falls back to the topic",
			msg:         ntfy.Message{Topic: "ops", Message: "done", Priority: 1},
			wantSummary: "ops",
			wantBody:    "done",
			wantUrgency: dbusnotify.UrgencyLow,
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "blank title falls back to the topic",
			msg:         ntfy.Message{Title: "   ", Topic: "ops", Priority: 3},
			wantSummary: "ops",
			wantUrgency: dbusnotify.UrgencyNormal,
			wantTimeout: 45 * time.Second,
		},
		{
			name:        "nothing to name it with",
			msg:         ntfy.Message{Message: "hola"},
			wantSummary: appName,
			wantBody:    "hola",
			wantUrgency: dbusnotify.UrgencyNormal,
			wantTimeout: 45 * time.Second,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compose(c.msg)
			if got.Summary != c.wantSummary {
				t.Errorf("Summary = %q; want %q", got.Summary, c.wantSummary)
			}
			if got.Body != c.wantBody {
				t.Errorf("Body = %q; want %q", got.Body, c.wantBody)
			}
			if got.ExpireTimeout != c.wantTimeout {
				t.Errorf("ExpireTimeout = %v; want %v", got.ExpireTimeout, c.wantTimeout)
			}
			if got.AppName != appName {
				t.Errorf("AppName = %q; want %q", got.AppName, appName)
			}
			hint, ok := got.Hints["urgency"]
			if !ok {
				t.Fatal("the notification carries no urgency hint")
			}
			if gotUrgency, ok := hint.Value().(byte); !ok || dbusnotify.Urgency(gotUrgency) != c.wantUrgency {
				t.Errorf("urgency hint = %v; want %d", hint.Value(), c.wantUrgency)
			}
		})
	}
}

func TestShowSendsTheMessage(t *testing.T) {
	s := &fakeSender{}
	d, _ := newTestNotifier(t, s, nil, nil)
	msg := ntfy.Message{
		ID:       "abc",
		Event:    ntfy.EventMessage,
		Topic:    "ops",
		Title:    "Deploy",
		Message:  "done",
		Priority: 5,
	}
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() = %v; want nil", err)
	}
	if got := s.count(); got != 1 {
		t.Fatalf("sent %d notifications; want 1", got)
	}
	note := s.last(t)
	if note.Summary != "Deploy" || note.Body != "done" {
		t.Errorf("sent summary %q body %q; want %q and %q", note.Summary, note.Body, "Deploy", "done")
	}
	if note.ExpireTimeout != 60*time.Second {
		t.Errorf("ExpireTimeout = %v; want 60s", note.ExpireTimeout)
	}
}

// hungSender never answers, the way a notification service that stopped
// responding does. It holds the send until the test releases it.
type hungSender struct {
	started chan struct{}
	release chan struct{}
}

func newHungSender() *hungSender {
	return &hungSender{started: make(chan struct{}, 1), release: make(chan struct{})}
}

func (h *hungSender) SendNotification(dbusnotify.Notification) (uint32, error) {
	h.started <- struct{}{}
	<-h.release
	return 0, errors.New("the service came back")
}

// TestShowGivesUpWhenTheContextEnds is the shutdown guarantee: a hung
// notification service cannot keep the caller past its cancellation.
func TestShowGivesUpWhenTheContextEnds(t *testing.T) {
	h := newHungSender()
	d, _ := newTestNotifier(t, h, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Show(ctx, ntfy.Message{ID: "m1", Event: ntfy.EventMessage, Message: "hola"})
	}()

	select {
	case <-h.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the notification was never sent")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Show() after the cancellation = %v; want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Show() kept waiting for a service that never answered")
	}

	// The hung send still holds the one place, so the next one is refused
	// instead of piling up behind it.
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := d.Show(ctx2, ntfy.Message{ID: "m2", Event: ntfy.EventMessage}); !errors.Is(err, errBusy) {
		t.Errorf("Show() while the service is hung = %v; want errBusy", err)
	}

	close(h.release) // let the goroutine of the first send end
}

func TestShowSkipsEventsThatAreNotMessages(t *testing.T) {
	for _, event := range []string{ntfy.EventKeepalive, ntfy.EventOpen, ""} {
		t.Run(event, func(t *testing.T) {
			s := &fakeSender{}
			d, _ := newTestNotifier(t, s, nil, nil)
			if err := d.Show(context.Background(), ntfy.Message{Event: event}); err != nil {
				t.Fatalf("Show() = %v; want nil", err)
			}
			if got := s.count(); got != 0 {
				t.Errorf("sent %d notifications for a %q event; want 0", got, event)
			}
		})
	}
}

// TestShowSurvivesAMissingService is the acceptance criterion: the failure is
// logged, returned, and the next message is still attempted.
func TestShowSurvivesAMissingService(t *testing.T) {
	s := &fakeSender{}
	s.fail(errors.New("no such name org.freedesktop.Notifications"))
	d, logs := newTestNotifier(t, s, nil, nil)
	msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Topic: "ops", Message: "done"}

	err := d.Show(context.Background(), msg)
	if err == nil {
		t.Fatal("Show() = nil; want the failure of the service")
	}
	if !logs.warned() {
		t.Error("the failure was not logged at warning level")
	}

	s.heal()
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() after the service came back = %v; want nil", err)
	}
	if got := s.count(); got != 1 {
		t.Errorf("sent %d notifications; want 1", got)
	}
}

func TestShowHonoursACancelledContext(t *testing.T) {
	s := &fakeSender{}
	d, _ := newTestNotifier(t, s, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Show(ctx, ntfy.Message{Event: ntfy.EventMessage, Message: "done"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Show() = %v; want context.Canceled", err)
	}
	if got := s.count(); got != 0 {
		t.Errorf("sent %d notifications; want 0", got)
	}
}

func TestShowAttachesTheLinkOnlyWhenItCanBeOpened(t *testing.T) {
	cases := []struct {
		name string
		link string
		open bool // whether the desktop has a tool at all
		want bool
	}{
		{"https link", "https://example.com/a", true, true},
		{"http link", "http://example.com", true, true},
		{"link with spaces around", "  https://example.com/a  ", true, true},
		{"javascript is refused", "javascript:alert(1)", true, false},
		{"file scheme is refused", "file:///etc/passwd", true, false},
		{"no host", "https://", true, false},
		{"no link", "", true, false},
		{"desktop without a handler", "https://example.com/a", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &fakeSender{}
			var o opener
			if c.open {
				o = newFakeOpener()
			}
			d, _ := newTestNotifier(t, s, o, nil)
			msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Message: "done", Click: c.link}
			if err := d.Show(context.Background(), msg); err != nil {
				t.Fatalf("Show() = %v; want nil", err)
			}
			note := s.last(t)
			if got := hasDefaultAction(note); got != c.want {
				t.Errorf("notification carries a default action = %v; want %v", got, c.want)
			}
		})
	}
}

// TestShowTranslatesTheActionLabel checks the catalog is wired: the label the
// server may draw goes through T, not written by hand.
func TestShowTranslatesTheActionLabel(t *testing.T) {
	s := &fakeSender{}
	d, _ := newTestNotifier(t, s, newFakeOpener(), i18n.New("es"))
	msg := ntfy.Message{Event: ntfy.EventMessage, Message: "done", Click: "https://example.com/a"}
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() = %v; want nil", err)
	}
	note := s.last(t)
	if len(note.Actions) != 1 {
		t.Fatalf("notification carries %d actions; want 1", len(note.Actions))
	}
	if note.Actions[0].Label != "Abrir" {
		t.Errorf("action label = %q; want %q", note.Actions[0].Label, "Abrir")
	}
}

func TestClickOpensTheLink(t *testing.T) {
	s := &fakeSender{}
	o := newFakeOpener()
	d, _ := newTestNotifier(t, s, o, nil)
	msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Message: "done", Click: "https://example.com/a"}
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() = %v; want nil", err)
	}

	d.actionInvoked(&dbusnotify.ActionInvokedSignal{ID: 1, ActionKey: actionOpen})
	select {
	case got := <-o.urls:
		if got != "https://example.com/a" {
			t.Errorf("opened %q; want %q", got, "https://example.com/a")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the opener was not called within 2s")
	}
}

func TestClickIgnoresForeignSignals(t *testing.T) {
	cases := []struct {
		name string
		sig  *dbusnotify.ActionInvokedSignal
	}{
		{"another key", &dbusnotify.ActionInvokedSignal{ID: 1, ActionKey: "cancel"}},
		{"another notification", &dbusnotify.ActionInvokedSignal{ID: 99, ActionKey: actionOpen}},
		{"no signal", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &fakeSender{}
			o := newFakeOpener()
			d, _ := newTestNotifier(t, s, o, nil)
			msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Message: "done", Click: "https://example.com/a"}
			if err := d.Show(context.Background(), msg); err != nil {
				t.Fatalf("Show() = %v; want nil", err)
			}
			d.actionInvoked(c.sig)
			select {
			case got := <-o.urls:
				t.Fatalf("opened %q; want nothing", got)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func TestClosedForgetsTheLink(t *testing.T) {
	s := &fakeSender{}
	o := newFakeOpener()
	d, _ := newTestNotifier(t, s, o, nil)
	msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Message: "done", Click: "https://example.com/a"}
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() = %v; want nil", err)
	}

	d.closed(&dbusnotify.NotificationClosedSignal{ID: 1, Reason: dbusnotify.ReasonDismissedByUser})
	d.actionInvoked(&dbusnotify.ActionInvokedSignal{ID: 1, ActionKey: actionOpen})
	select {
	case got := <-o.urls:
		t.Fatalf("opened %q after the notification was closed; want nothing", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestClickReportsTheMessage is the wiring the daemon needs: opening the
// notification tells which message it came from, and the link still opens.
func TestClickReportsTheMessage(t *testing.T) {
	s := &fakeSender{}
	o := newFakeOpener()
	d, _ := newTestNotifier(t, s, o, nil)

	clicked := make(chan ntfy.Message, 1)
	d.click = func(m ntfy.Message) error {
		clicked <- m
		return nil
	}

	msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Topic: "ops", Message: "done", Click: "https://example.com/a"}
	if err := d.Show(context.Background(), msg); err != nil {
		t.Fatalf("Show() = %v; want nil", err)
	}

	d.actionInvoked(&dbusnotify.ActionInvokedSignal{ID: 1, ActionKey: actionOpen})

	select {
	case got := <-clicked:
		if got.ID != "abc" || got.Topic != "ops" {
			t.Errorf("click reported message %q of topic %q; want %q of %q", got.ID, got.Topic, "abc", "ops")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the click was not reported within 2s")
	}

	select {
	case got := <-o.urls:
		if got != "https://example.com/a" {
			t.Errorf("opened %q; want %q", got, "https://example.com/a")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the opener was not called within 2s")
	}
}

// TestClickOutcomes checks that a failing or missing callback never costs the
// click: the link opens either way, and a failure is only a logged line.
func TestClickOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		click   func(ntfy.Message) error
		wantLog bool
	}{
		{name: "no callback", click: nil, wantLog: false},
		{name: "callback succeeds", click: func(ntfy.Message) error { return nil }, wantLog: false},
		{name: "callback fails", click: func(ntfy.Message) error { return errors.New("the history is read-only") }, wantLog: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &fakeSender{}
			o := newFakeOpener()
			d, logs := newTestNotifier(t, s, o, nil)
			d.click = c.click

			msg := ntfy.Message{ID: "abc", Event: ntfy.EventMessage, Message: "done", Click: "https://example.com/a"}
			if err := d.Show(context.Background(), msg); err != nil {
				t.Fatalf("Show() = %v; want nil", err)
			}
			d.actionInvoked(&dbusnotify.ActionInvokedSignal{ID: 1, ActionKey: actionOpen})

			select {
			case <-o.urls:
			case <-time.After(2 * time.Second):
				t.Fatal("the link was not opened within 2s")
			}
			if got := logs.warned(); got != c.wantLog {
				t.Errorf("failure logged = %v; want %v", got, c.wantLog)
			}
		})
	}
}

func TestRememberedLinksStayBounded(t *testing.T) {
	s := &fakeSender{}
	d, _ := newTestNotifier(t, s, newFakeOpener(), nil)
	for i := 0; i < maxTrackedLinks+10; i++ {
		d.remember(uint32(i+1), target{link: "https://example.com/a"})
	}
	d.mu.Lock()
	got := len(d.targets)
	d.mu.Unlock()
	if got > maxTrackedLinks {
		t.Errorf("tracked %d links; want at most %d", got, maxTrackedLinks)
	}
}

func TestHasCapability(t *testing.T) {
	cases := []struct {
		name string
		caps []string
		want string
		got  bool
	}{
		{"announced", []string{"body", "actions", "persistence"}, "actions", true},
		{"not announced", []string{"body", "persistence"}, "actions", false},
		{"empty", nil, "actions", false},
		{"exact match only", []string{"action"}, "actions", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasCapability(c.caps, c.want); got != c.got {
				t.Errorf("hasCapability(%v, %q) = %v; want %v", c.caps, c.want, got, c.got)
			}
		})
	}
}

func TestOpenableURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"https", "https://example.com/a?b=c", true},
		{"http", "http://example.com", true},
		{"upper case scheme", "HTTPS://example.com/a", true},
		{"javascript", "javascript:alert(1)", false},
		{"file", "file:///etc/passwd", false},
		{"ssh", "ssh://host/x", false},
		{"no scheme", "example.com/a", false},
		{"no host", "https://", false},
		{"empty", "", false},
		{"control characters", "http://exa\x00mple.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := openable(c.raw); got != c.want {
				t.Errorf("openable(%q) = %v; want %v", c.raw, got, c.want)
			}
		})
	}
}

// FuzzOpenableURL feeds the link parser the worst the network can send: it must
// never panic, and it must never accept a scheme other than http or https.
func FuzzOpenableURL(f *testing.F) {
	f.Add("https://example.com/a")
	f.Add("http://example.com")
	f.Add("javascript:alert(1)")
	f.Add("file:///etc/passwd")
	f.Add("https://")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		if !openable(raw) {
			return
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			t.Fatalf("openable(%q) = true but the link does not parse: %v", raw, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			t.Fatalf("openable(%q) = true with scheme %q", raw, u.Scheme)
		}
		if u.Host == "" {
			t.Fatalf("openable(%q) = true without a host", raw)
		}
	})
}

// hasDefaultAction reports whether the notification offers the click action.
func hasDefaultAction(note dbusnotify.Notification) bool {
	for _, a := range note.Actions {
		if a.Key == actionOpen {
			return true
		}
	}
	return false
}
