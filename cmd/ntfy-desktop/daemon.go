// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/notify"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/queue"
)

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
		"config", path, "server", cfg.ServerURL, "topic", cfg.Topic,
		"auth", cfg.AuthMode().String(), "log_level", level.String())

	serve(ctx, log, tr, env, *cfg)

	log.Info("stopped")
	return 0
}

// serve runs the subscription, the notifications and the decision queue until
// ctx is cancelled, and waits for everything it started before returning: a
// dialog that was open is killed with its context, never left behind.
func serve(ctx context.Context, log *slog.Logger, tr *i18n.Translator, env platform.Env, cfg config.Config) {
	// The session bus is not always there (a session started without one, a
	// connection refused): the program then keeps answering decisions instead of
	// dying on a retry loop.
	var notifier notify.Notifier
	bus, err := notify.New(log, tr)
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

	client := ntfy.New(cfg)
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
			logResults(log, q.Results())
		}()
		submit = q.Submit
	}

	var show func(context.Context, ntfy.Message) error
	if notifier != nil {
		show = notifier.Show
	}

	supervise(runCtx, log, msgs, failures, show, submit)
}

// supervise drains the subscription until ctx ends. Both channels are closed by
// the client when it gives up, so a nil channel is one that will never speak
// again and the loop ends when there is nothing left to read.
func supervise(
	ctx context.Context,
	log *slog.Logger,
	msgs <-chan ntfy.Message,
	failures <-chan error,
	show func(context.Context, ntfy.Message) error,
	submit func(ntfy.Message) error,
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
			deliver(ctx, log, m, show, submit)
		}
	}
}

// deliver shows one message and, when it asks something this client can run,
// hands it to the queue. A failure in either step is logged: one notification
// that could not be drawn does not stop the subscription.
func deliver(
	ctx context.Context,
	log *slog.Logger,
	m ntfy.Message,
	show func(context.Context, ntfy.Message) error,
	submit func(ntfy.Message) error,
) {
	if !m.IsMessage() {
		log.Debug("event without a message", "event", m.Event)
		return
	}

	if show != nil {
		// A cancelled context is the program stopping, not a failure worth a line.
		if err := show(ctx, m); err != nil && ctx.Err() == nil {
			log.Warn("showing the notification", "message", m.ID, "error", err)
		}
	}

	if len(m.Actions) == 0 {
		return
	}
	if submit == nil {
		log.Debug("the message asks a decision and there is no dialog tool", "message", m.ID)
		return
	}
	if err := submit(m); err != nil {
		if errors.Is(err, queue.ErrNoActions) {
			log.Debug("the message carries no action this client can run", "message", m.ID)
			return
		}
		log.Warn("queueing the decision", "message", m.ID, "error", err)
	}
}

// logResults reports what happened to every request the queue asked, until the
// channel closes with the queue.
func logResults(log *slog.Logger, results <-chan queue.Result) {
	for r := range results {
		switch r.Status {
		case queue.Answered:
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
