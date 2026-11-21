// SPDX-License-Identifier: GPL-3.0-or-later

// Package notify shows an ntfy message as a notification of the desktop.
//
// The contract is one method, Show. The implementation talks to the
// freedesktop notification service over the session bus, maps the message
// priority to the notification urgency, and always sends an explicit timeout:
// Plasma closes the bubble at once when the timeout is zero, so every
// notification carries a lifetime of its own.
//
// Showing a message is best effort. When the service is not there, the failure
// is logged and returned, and the program keeps running: a subscription does
// not stop because a notification could not be drawn.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	dbusnotify "github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

const (
	// appName is what the service shows as the sender. It is the program name,
	// not a translatable string.
	appName = "ntfy-desktop"
	// appIcon is the icon of the desktop entry installed with the program.
	appIcon = "ntfy-desktop"
	// actionOpen is the key the desktop sends when the bubble itself is
	// clicked. The freedesktop specification calls that action "default".
	actionOpen = "default"
	// openWait bounds the time the opener gets. A browser that keeps the
	// launcher in the foreground must not hold the goroutine forever.
	openWait = 15 * time.Second
	// maxTrackedLinks caps the links kept for a click. A service that never
	// sends NotificationClosed would otherwise grow the map without end.
	maxTrackedLinks = 128
	// Notification service of the freedesktop specification: its bus name and
	// its object path. The capability query below needs them to carry its own
	// deadline.
	serviceName = "org.freedesktop.Notifications"
	servicePath = "/org/freedesktop/Notifications"
	// capabilityWait bounds the capability query of the start-up. A bus without
	// the service, or a service that does not answer, must not hold the program
	// before it subscribes to anything.
	capabilityWait = 3 * time.Second
	// sendWait caps how long one notification waits for the service. A service
	// that has stopped answering keeps its one place and the caller moves on to
	// the next message.
	sendWait = 5 * time.Second
)

// Notifier shows a message as a notification of the desktop.
//
// Show is best effort: the implementation logs a failure and returns it, and
// the caller keeps running instead of stopping what it was doing.
type Notifier interface {
	Show(ctx context.Context, m ntfy.Message) error
}

// Option configures the notifier while it is built. It is the way the caller
// adds behaviour without widening Notifier.
type Option func(*DBus)

// WithClick makes the notifier report the message whose notification the
// person opened, so the program can mark it as read.
//
// fn runs on its own goroutine, never on the bus one, and its failure is only
// a logged line: the link still opens, because that is what the click asked
// for. A nil fn is the same as not passing the option.
func WithClick(fn func(ntfy.Message) error) Option {
	return func(d *DBus) { d.click = fn }
}

// errBusy says the service has not answered the previous notification yet.
var (
	errBusy = errors.New("the notification service is still busy")
	// errNoAnswer says the service took longer than sendWait to answer.
	errNoAnswer = errors.New("the notification service did not answer")
)

// sender is the single operation this package needs from the notification
// service. Keeping it this narrow is what lets the tests run without a bus.
type sender interface {
	SendNotification(n dbusnotify.Notification) (uint32, error)
}

// opener hands a URL to the desktop.
type opener interface {
	Open(ctx context.Context, rawURL string) error
}

// target is what a click on a notification needs: the link the desktop opens
// and the message it came from, so the history learns what was read.
type target struct {
	link    string
	message ntfy.Message
}

// DBus shows messages through the freedesktop notification service of the
// session bus. It keeps the link of every notification that carries one, so a
// click can open it later.
type DBus struct {
	conn  *dbus.Conn
	svc   dbusnotify.Notifier // nil in the tests: nothing to close there
	send  sender
	open  opener // nil when the desktop has no tool to open links
	click func(ntfy.Message) error
	log   *slog.Logger
	tr    *i18n.Translator

	// sending holds at most one send: a service that stops answering costs one
	// goroutine, not one per message.
	sending chan struct{}

	mu      sync.Mutex
	targets map[uint32]target
}

// New connects to the session bus and subscribes to the signals of the
// notification service.
//
// A session without a bus is the only failure: the caller can then run without
// notifications. A bus without a notification service is not detectable here,
// because the service is looked up on every call; Show reports it and the
// program goes on.
func New(log *slog.Logger, tr *i18n.Translator, opts ...Option) (*DBus, error) {
	if log == nil {
		log = slog.Default()
	}
	if tr == nil {
		tr = i18n.New("en")
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to the session bus: %w", err)
	}

	d := &DBus{
		conn:    conn,
		log:     log,
		tr:      tr,
		sending: make(chan struct{}, 1),
		targets: make(map[uint32]target),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	svc, err := dbusnotify.New(conn,
		dbusnotify.WithLogger(busLogger{log: log}),
		dbusnotify.WithOnAction(d.actionInvoked),
		dbusnotify.WithOnClosed(d.closed),
	)
	if err != nil {
		if cerr := conn.Close(); cerr != nil {
			log.Warn("closing the session bus connection", "error", cerr)
		}
		return nil, fmt.Errorf("subscribing to the notification signals: %w", err)
	}

	d.svc = svc
	d.send = svc
	d.open = findOpener(log)
	d.reportCapabilities()
	return d, nil
}

// Show draws the message as a notification of the desktop.
//
// Events that are not messages (open, keepalive) draw nothing. A link is only
// attached when it is http or https, so a message from the network never picks
// the program the desktop runs.
//
// The send is bounded by ctx: a service that stops answering returns the
// cancellation instead of holding the caller, which is what lets the program
// end when it is told to.
func (d *DBus) Show(ctx context.Context, m ntfy.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !m.IsMessage() {
		return nil
	}

	note := compose(m)
	link := linkFor(m.Click, d.open)
	if link != "" {
		note.Actions = append(note.Actions, dbusnotify.NewDefaultAction(d.tr.T("Open")))
	}

	id, err := d.deliver(ctx, note)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // the program is stopping: nothing to report
		}
		d.log.Warn("showing the notification", "topic", m.Topic, "message", m.ID, "error", err)
		return fmt.Errorf("showing the notification of %q: %w", m.ID, err)
	}
	if link != "" {
		d.remember(id, target{link: link, message: m})
	}
	return nil
}

// deliver sends the notification and returns its identifier, unless ctx ends or
// the service takes longer than sendWait. A send that never comes back keeps its
// place, so the next one is refused with errBusy instead of stacking up behind
// it.
func (d *DBus) deliver(ctx context.Context, note dbusnotify.Notification) (uint32, error) {
	select {
	case d.sending <- struct{}{}:
	default:
		return 0, errBusy
	}

	type answer struct {
		id  uint32
		err error
	}
	done := make(chan answer, 1)
	go func() {
		defer func() { <-d.sending }()
		id, err := d.send.SendNotification(note)
		done <- answer{id: id, err: err}
	}()

	timer := time.NewTimer(sendWait)
	defer timer.Stop()
	select {
	case got := <-done:
		return got.id, got.err
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return 0, errNoAnswer
	}
}

// Close stops the signal loop and releases the connection to the bus.
func (d *DBus) Close() error {
	var errs []error
	if d.svc != nil {
		if err := d.svc.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing the notification service: %w", err))
		}
	}
	if d.conn != nil {
		if err := d.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing the session bus: %w", err))
		}
	}
	return errors.Join(errs...)
}

// actionInvoked runs on the signal goroutine of the library, so it must not
// block: opening the link happens somewhere else.
func (d *DBus) actionInvoked(sig *dbusnotify.ActionInvokedSignal) {
	if sig == nil || sig.ActionKey != actionOpen {
		return
	}
	t, ok := d.take(sig.ID)
	if !ok {
		return
	}
	go d.opened(t)
}

// opened is the click: the message is reported as read and its link is handed
// to the desktop. It runs off the bus goroutine, because both steps may wait
// on a file or on a browser.
func (d *DBus) opened(t target) {
	if d.click != nil {
		if err := d.click(t.message); err != nil {
			d.log.Warn("marking the clicked message as read", "message", t.message.ID, "error", err)
		}
	}
	d.openLink(t.link)
}

// openLink hands the link to the desktop and reports what went wrong.
func (d *DBus) openLink(link string) {
	if d.open == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), openWait)
	defer cancel()
	if err := d.open.Open(ctx, link); err != nil {
		d.log.Warn("opening the link of a notification", "url", link, "error", err)
	}
}

// closed forgets the link of a notification the service already dismissed.
func (d *DBus) closed(sig *dbusnotify.NotificationClosedSignal) {
	if sig == nil {
		return
	}
	d.forget(sig.ID)
}

// remember stores what a click of the notification needs, so it can open the
// link and report the message later.
func (d *DBus) remember(id uint32, t target) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.targets) >= maxTrackedLinks {
		// Losing the older links only means their click does nothing, which is
		// better than a map that never stops growing.
		d.targets = make(map[uint32]target, maxTrackedLinks)
	}
	d.targets[id] = t
}

// take returns what the notification needs and forgets it: one notification,
// one click.
func (d *DBus) take(id uint32) (target, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.targets[id]
	delete(d.targets, id)
	return t, ok
}

// forget drops the link of a notification that is no longer on screen.
func (d *DBus) forget(id uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.targets, id)
}

// findOpener looks for the tool the desktop uses to open a URL. Without it the
// notification still shows and only the click does nothing.
func findOpener(log *slog.Logger) opener {
	path, err := exec.LookPath("xdg-open")
	if err != nil {
		log.Info("no xdg-open in PATH: clicking a notification will not open its link")
		return nil
	}
	return xdgOpen{path: path}
}

// reportCapabilities tells the operator what the service will not do. A click
// only reaches the program when the server announces the "actions" capability.
//
// The query is a nicety of the start-up, so it carries a deadline: a bus
// without the service, or a service that does not answer, must not hold the
// program before it subscribes.
func (d *DBus) reportCapabilities() {
	ctx, cancel := context.WithTimeout(context.Background(), capabilityWait)
	defer cancel()

	call := d.conn.Object(serviceName, servicePath).
		CallWithContext(ctx, serviceName+".GetCapabilities", 0)
	if call.Err != nil {
		d.log.Debug("reading the capabilities of the notification service", "error", call.Err)
		return
	}
	var caps []string
	if err := call.Store(&caps); err != nil {
		d.log.Debug("reading the capabilities of the notification service", "error", err)
		return
	}
	if !hasCapability(caps, "actions") {
		d.log.Info("the notification service does not announce actions: clicking a notification will not open its link")
	}
}

// hasCapability reports whether the service announces the given capability.
func hasCapability(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// busLogger sends the library's diagnostics to slog, so the signal loop writes
// where the rest of the program writes.
type busLogger struct{ log *slog.Logger }

func (l busLogger) Printf(format string, v ...any) {
	l.log.Debug("notification service: " + strings.TrimSpace(fmt.Sprintf(format, v...)))
}
