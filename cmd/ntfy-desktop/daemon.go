// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/history"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/notify"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/queue"
)

// recorder is the history the daemon writes. It is declared here, where it is
// used, and internal/history is the implementation.
type recorder interface {
	Add(m ntfy.Message) error
	MarkRead(topic, id string) error
}

// resumable is the part of the history that tells a subscription where to
// start: the last message it knows of each topic.
type resumable interface {
	Latest(topic string) (history.Entry, bool)
}

// ports are what the supervisor reaches: the notification of the desktop, the
// decision queue and the history. A nil field is a piece this session does not
// have, and the daemon keeps working without it.
type ports struct {
	show   func(context.Context, ntfy.Message) error
	submit func(ntfy.Message) error
	hist   recorder
}

// runDaemon is the command the service unit starts: it reads the configuration,
// subscribes to the topic and stays alive until ctx is cancelled.
func runDaemon(ctx context.Context, opts options, env platform.Env, tr *i18n.Translator, stderr io.Writer) int {
	path, err := config.DefaultPath()
	if err != nil {
		newLogger(stderr, opts.level).Error("cannot locate the configuration", "error", err)
		return 1
	}
	cfg, err := config.Load(path)
	if err != nil {
		reportConfigError(newLogger(stderr, opts.level), tr, path, err)
		return 1
	}

	// The flag wins over the file: it is the one the operator wrote last, in the
	// unit or in the terminal.
	level := opts.level
	if !opts.levelSet {
		if fromFile, ok := parseLevel(cfg.LogLevel); ok {
			level = fromFile
		}
	}
	log := newLogger(stderr, level)

	log.Info("subscribing",
		"config", path, "server", cfg.ServerURL, "topics", strings.Join(cfg.Topics, ","),
		"auth", cfg.AuthMode().String(), "log_level", level.String())

	serve(ctx, log, tr, env, *cfg)

	log.Info("stopped")
	return 0
}

// serve runs the subscription, the notifications, the decision queue and the
// history until ctx is cancelled, and waits for everything it started before
// returning: a dialog that was open is killed with its context, never left
// behind.
func serve(ctx context.Context, log *slog.Logger, tr *i18n.Translator, env platform.Env, cfg config.Config) {
	// The history is a cache of what the person missed, so a cache directory
	// that cannot be used costs the feature and not the program.
	store := openHistory(log)

	// A nil recorder and a nil *history.Store are two different things: the
	// interface only becomes "no history" once the pointer has been checked.
	var hist recorder
	if store != nil {
		hist = store
	}

	// The session bus is not always there (a session started without one, a
	// connection refused): the program then keeps answering decisions instead of
	// dying on a retry loop.
	var notifier notify.Notifier
	var opts []notify.Option
	if hist != nil {
		// The click of a notification marks its message as read: the notifier
		// runs it on its own goroutine, before it opens the link.
		click := hist
		opts = append(opts, notify.WithClick(func(m ntfy.Message) error {
			return click.MarkRead(m.Topic, m.ID)
		}))
	}
	bus, err := notify.New(log, tr, opts...)
	if err != nil {
		log.Error("running without notifications", "error", err)
	} else {
		notifier = bus
	}

	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	// Cancelling first, then waiting, then closing the session bus: the order is
	// the guarantee that nothing draws or answers after the connection is gone.
	defer func() {
		cancel()
		wg.Wait()
		if bus != nil {
			if err := bus.Close(); err != nil {
				log.Warn("closing the notification service", "error", err)
			}
		}
	}()

	// The subscription starts after the last message the history kept, so what
	// arrived while the program was not running is read back from the server.
	since := ""
	if store != nil {
		since = startID(store, cfg.Topics)
	}
	if since != "" {
		log.Info("resuming the subscription from the history", "since", since)
	}
	client := ntfy.New(cfg, ntfy.WithSince(since))
	msgs, failures := client.Subscribe(runCtx)

	// Without a dialog tool the messages are still shown; the decisions they
	// carry are logged instead of asked, because there is nowhere to ask them.
	var submit func(ntfy.Message) error
	decider, err := dialog.New(env.DialogTool, tr)
	if err != nil {
		log.Warn("no desktop dialog: decisions will not be asked", "error", err)
	} else {
		q := queue.New(decider, action.New(), queue.DefaultTTL).WithSettle(queue.SettleWindow)
		wg.Add(2)
		go func() {
			defer wg.Done()
			q.Run(runCtx)
		}()
		go func() {
			defer wg.Done()
			logResults(log, q.Results(), hist)
		}()
		submit = q.Submit
	}

	var show func(context.Context, ntfy.Message) error
	if notifier != nil {
		show = notifier.Show
	}

	supervise(runCtx, log, msgs, failures, ports{show: show, submit: submit, hist: hist})
}

// openHistory opens the store under the user's cache directory. A store that
// cannot be opened is reported and returns nil: the daemon then notifies and
// answers without a history instead of refusing to start.
func openHistory(log *slog.Logger) *history.Store {
	dir, err := history.DefaultDir()
	if err != nil {
		log.Error("running without history", "error", err)
		return nil
	}
	store, err := history.Open(dir, history.DefaultLimit)
	if err != nil {
		log.Error("running without history", "path", dir, "error", err)
		return nil
	}
	// A file that is not a history is already out of the way: say so and go on
	// with what is left.
	for _, warning := range store.Warnings() {
		log.Warn("a stored history was discarded", "error", warning)
	}
	log.Info("history open", "path", dir)
	return store
}

// startID is where the subscription resumes from: the newest message the
// history kept among the configured topics.
//
// The protocol takes one since for a whole subscription, so several topics
// share it. The oldest of their newest ids is the one that cannot skip a topic;
// the price is re-reading some messages, which the store folds by id. A topic
// without anything stored contributes nothing, and an empty history yields an
// empty id: the subscription then starts live.
func startID(store resumable, topics []string) string {
	if store == nil {
		return ""
	}
	var oldest history.Entry
	found := false
	for _, topic := range topics {
		e, ok := store.Latest(topic)
		if !ok {
			continue
		}
		if !found || e.Time.Before(oldest.Time) {
			oldest, found = e, true
		}
	}
	if !found {
		return ""
	}
	return oldest.ID
}

// supervise drains the subscription until ctx ends. Both channels are closed by
// the client when it gives up, so a nil channel is one that will never speak
// again and the loop ends when there is nothing left to read.
func supervise(
	ctx context.Context,
	log *slog.Logger,
	msgs <-chan ntfy.Message,
	failures <-chan error,
	p ports,
) {
	for msgs != nil || failures != nil {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case err, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			// The client retries on its own: this is a note, not a reason to stop.
			log.Warn("the subscription failed, retrying", "error", err)
		case m, ok := <-msgs:
			if !ok {
				msgs = nil
				continue
			}
			deliver(ctx, log, m, p)
		}
	}
}

// deliver records one message in the history and shows it; when it asks
// something this client can run, it hands it to the queue too. A failure in any
// step is logged: one notification that could not be drawn does not stop the
// subscription.
func deliver(ctx context.Context, log *slog.Logger, m ntfy.Message, p ports) {
	if !m.IsMessage() {
		// Open, keepalive and the rest are not messages: the history is for what
		// a person could have read.
		log.Debug("event without a message", "event", m.Event)
		return
	}

	// The history comes first, so a message is remembered even when the desktop
	// refuses to draw it or nobody answers it.
	if p.hist != nil {
		if err := p.hist.Add(m); err != nil {
			log.Warn("storing the message in the history", "message", m.ID, "error", err)
		}
	}

	if p.show != nil {
		// A cancelled context is the program stopping, not a failure worth a line.
		if err := p.show(ctx, m); err != nil && ctx.Err() == nil {
			log.Warn("showing the notification", "message", m.ID, "error", err)
		}
	}

	if len(m.Actions) == 0 {
		return
	}
	if p.submit == nil {
		log.Debug("the message asks a decision and there is no dialog tool", "message", m.ID)
		return
	}
	if err := p.submit(m); err != nil {
		if errors.Is(err, queue.ErrNoActions) {
			log.Debug("the message carries no action this client can run", "message", m.ID)
			return
		}
		log.Warn("queueing the decision", "message", m.ID, "error", err)
	}
}

// logResults reports what happened to every request the queue asked, until the
// channel closes with the queue. An answered message is marked as read: the
// person saw it and chose, even when the action itself then failed.
func logResults(log *slog.Logger, results <-chan queue.Result, hist recorder) {
	for r := range results {
		switch r.Status {
		case queue.Answered:
			if hist != nil {
				if err := hist.MarkRead(r.Message.Topic, r.Message.ID); err != nil {
					log.Warn("marking the answered message as read",
						"message", r.Message.ID, "error", err)
				}
			}
			if r.Err != nil {
				log.Warn("the chosen action failed",
					"message", r.Message.ID, "action", action.Label(r.Action), "error", r.Err)
				continue
			}
			log.Info("action run", "message", r.Message.ID, "action", action.Label(r.Action))
		case queue.Expired:
			log.Info("decision expired without an answer", "message", r.Message.ID)
		case queue.Unavailable:
			log.Warn("the decision could not be asked", "message", r.Message.ID, "error", r.Err)
		default:
			log.Warn("unknown result", "message", r.Message.ID, "status", r.Status.String())
		}
	}
}
