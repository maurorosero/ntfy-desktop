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
)

// Notifier shows a message as a notification of the desktop.
//
// Show is best effort: the implementation logs a failure and returns it, and
// the caller keeps running instead of stopping what it was doing.
type Notifier interface {
	Show(ctx context.Context, m ntfy.Message) error
}

// sender is the single operation this package needs from the notification
// service. Keeping it this narrow is what lets the tests run without a bus.
type sender interface {
	SendNotification(n dbusnotify.Notification) (uint32, error)
}

// opener hands a URL to the desktop.
type opener interface {
	Open(ctx context.Context, rawURL string) error
}

// DBus shows messages through the freedesktop notification service of the
// session bus. It keeps the link of every notification that carries one, so a
// click can open it later.
type DBus struct {
	conn *dbus.Conn
	svc  dbusnotify.Notifier // nil in the tests: nothing to close there
	send sender
	open opener // nil when the desktop has no tool to open links
	log  *slog.Logger
	tr   *i18n.Translator

	mu    sync.Mutex
	links map[uint32]string
}

// New connects to the session bus and subscribes to the signals of the
// notification service.
//
// A session without a bus is the only failure: the caller can then run without
// notifications. A bus without a notification service is not detectable here,
// because the service is looked up on every call; Show reports it and the
// program goes on.
func New(log *slog.Logger, tr *i18n.Translator) (*DBus, error) {
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
		conn:  conn,
		log:   log,
		tr:    tr,
		links: make(map[uint32]string),
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
	d.reportCapabilities(svc)
	return d, nil
}

// Show draws the message as a notification of the desktop.
//
// Events that are not messages (open, keepalive) draw nothing. A link is only
// attached when it is http or https, so a message from the network never picks
// the program the desktop runs.
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

	id, err := d.send.SendNotification(note)
	if err != nil {
		d.log.Warn("showing the notification", "topic", m.Topic, "message", m.ID, "error", err)
		return fmt.Errorf("showing the notification of %q: %w", m.ID, err)
	}
	if link != "" {
		d.remember(id, link)
	}
	return nil
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
	link := d.take(sig.ID)
	if link == "" {
		return
	}
	go d.openLink(link)
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

// remember stores the link of a notification so a click can open it.
func (d *DBus) remember(id uint32, link string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.links) >= maxTrackedLinks {
		// Losing the older links only means their click does nothing, which is
		// better than a map that never stops growing.
		d.links = make(map[uint32]string, maxTrackedLinks)
	}
	d.links[id] = link
}

// take returns the link of a notification and forgets it: one notification,
// one click.
func (d *DBus) take(id uint32) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	link := d.links[id]
	delete(d.links, id)
	return link
}

// forget drops the link of a notification that is no longer on screen.
func (d *DBus) forget(id uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.links, id)
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
func (d *DBus) reportCapabilities(svc dbusnotify.Notifier) {
	caps, err := svc.GetCapabilities()
	if err != nil {
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
