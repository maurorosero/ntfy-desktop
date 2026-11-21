// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// shutdownBudget is how long the program may take to stop after its context is
// cancelled. The issue asks for less than two seconds, and a dialog child is
// killed as soon as the context ends.
const shutdownBudget = 2 * time.Second

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     command
		wantLvl  slog.Level
		wantSet  bool
		wantFail bool
	}{
		{name: "no arguments prints the environment", want: commandEnv, wantLvl: slog.LevelInfo},
		{name: "the env subcommand", args: []string{"env"}, want: commandEnv, wantLvl: slog.LevelInfo},
		{name: "the daemon flag", args: []string{"--daemon"}, want: commandDaemon, wantLvl: slog.LevelInfo},
		{name: "the daemon subcommand", args: []string{"daemon"}, want: commandDaemon, wantLvl: slog.LevelInfo},
		{name: "the version flag", args: []string{"--version"}, want: commandVersion, wantLvl: slog.LevelInfo},
		{name: "the version subcommand", args: []string{"version"}, want: commandVersion, wantLvl: slog.LevelInfo},
		{
			name:    "a debug level",
			args:    []string{"daemon", "--log-level", "debug"},
			want:    commandDaemon,
			wantLvl: slog.LevelDebug,
			wantSet: true,
		},
		{
			name:    "a level in another case and with an equals sign",
			args:    []string{"--daemon", "--log-level=WARN"},
			want:    commandDaemon,
			wantLvl: slog.LevelWarn,
			wantSet: true,
		},
		{name: "two commands at once", args: []string{"daemon", "--version"}, wantFail: true},
		{name: "an unknown level", args: []string{"daemon", "--log-level", "trace"}, wantFail: true},
		{name: "an unknown command", args: []string{"serve"}, wantFail: true},
		{name: "an argument after the subcommand", args: []string{"env", "extra"}, wantFail: true},
		{name: "an unknown flag", args: []string{"--nope"}, wantFail: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse(c.args, testTranslator(), io.Discard)
			if c.wantFail {
				if err == nil {
					t.Fatalf("parse(%q) = %+v; want an error", c.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q) = error %v; want no error", c.args, err)
			}
			if got.command != c.want {
				t.Errorf("parse(%q).command = %q; want %q", c.args, got.command, c.want)
			}
			if got.level != c.wantLvl {
				t.Errorf("parse(%q).level = %v; want %v", c.args, got.level, c.wantLvl)
			}
			if got.levelSet != c.wantSet {
				t.Errorf("parse(%q).levelSet = %v; want %v", c.args, got.levelSet, c.wantSet)
			}
		})
	}
}

func TestRunWithoutFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, &out, &errOut); code != 0 {
		t.Fatalf("run() = %d; want 0. stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ntfy-desktop") {
		t.Errorf("run() printed %q; want the environment report", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("run() wrote %q to stderr; want nothing", errOut.String())
	}
}

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("run(--version) = %d; want 0. stderr: %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "ntfy-desktop ") {
		t.Errorf("run(--version) printed %q", out.String())
	}
}

func TestRunPrintsTheUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"-h"}, &out, &errOut); code != 0 {
		t.Errorf("run(-h) = %d; want 0", code)
	}
	if !strings.Contains(errOut.String(), "usage: ntfy-desktop") {
		t.Errorf("run(-h) wrote %q; want the usage", errOut.String())
	}
}

func TestRunRejectsABadCommandLine(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"daemon", "extra"}, &out, &errOut); code != 2 {
		t.Errorf("run(daemon extra) = %d; want 2", code)
	}
	if !strings.Contains(errOut.String(), "extra") {
		t.Errorf("run(daemon extra) wrote %q to stderr; want the offending argument", errOut.String())
	}
}

// TestSuperviseShutsDown is the clean end of the loop: once the context is
// cancelled, supervise returns at once even though the subscription is still
// open, and every message it read was handled exactly once.
func TestSuperviseShutsDown(t *testing.T) {
	msgs := make(chan ntfy.Message, 4)
	failures := make(chan error, 4)
	shown := make(chan ntfy.Message, 4)
	submitted := make(chan ntfy.Message, 4)

	show := func(_ context.Context, m ntfy.Message) error {
		shown <- m
		return errors.New("no notification service") // a failure must not stop the loop
	}
	submit := func(m ntfy.Message) error {
		submitted <- m
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		supervise(ctx, discardLogger(), msgs, failures, ports{show: show, submit: submit})
		close(returned)
	}()

	msgs <- ntfy.Message{ID: "open", Event: ntfy.EventOpen}
	msgs <- ntfy.Message{ID: "m1", Event: ntfy.EventMessage, Title: "deploy?", Actions: []ntfy.Action{
		{Action: action.HTTP, Label: "Approve", URL: "https://example.org/yes"},
	}}
	failures <- errors.New("connection refused")

	select {
	case m := <-shown:
		if m.ID != "m1" {
			t.Fatalf("Show() got %q; want the message m1", m.ID)
		}
	case <-time.After(shutdownBudget):
		t.Fatal("Show() was never called for the message")
	}
	select {
	case m := <-submitted:
		if m.ID != "m1" {
			t.Fatalf("Submit() got %q; want the message m1", m.ID)
		}
	case <-time.After(shutdownBudget):
		t.Fatal("Submit() was never called for the message with actions")
	}
	select {
	case m := <-shown:
		t.Fatalf("Show() was called for the %q event; want only messages", m.Event)
	default:
	}

	start := time.Now()
	cancel()
	select {
	case <-returned:
	case <-time.After(shutdownBudget):
		t.Fatalf("supervise() did not return within %v of the cancellation", shutdownBudget)
	}
	if took := time.Since(start); took > shutdownBudget {
		t.Errorf("supervise() took %v to stop; want less than %v", took, shutdownBudget)
	}
}

// TestSuperviseEndsWithTheChannels covers a subscription that closes on its own:
// the loop ends without a cancellation, so a broken client cannot hang the
// program.
func TestSuperviseEndsWithTheChannels(t *testing.T) {
	msgs := make(chan ntfy.Message)
	failures := make(chan error)
	close(msgs)
	close(failures)

	returned := make(chan struct{})
	go func() {
		supervise(context.Background(), discardLogger(), msgs, failures, ports{})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(shutdownBudget):
		t.Fatal("supervise() did not return after both channels were closed")
	}
}

// TestDaemonStopsOnCancel runs the daemon the unit starts, with a configuration
// that points at a server nobody listens on, and stops it the way a SIGTERM
// does. It is the lifecycle test of the issue: start, keep running while the
// subscription fails, and end inside the budget.
func TestDaemonStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// No session bus: the daemon has to keep running without notifications.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "absent"))

	cfgDir := filepath.Join(dir, "ntfy-desktop")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("creating the configuration directory: %v", err)
	}
	cfg := "server = \"http://127.0.0.1:1\"\ntopic = \"alerts\"\nlog-level = \"debug\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	stderr := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	code := make(chan int, 1)
	go func() {
		code <- run(ctx, []string{"daemon"}, io.Discard, stderr)
	}()

	// Once the daemon logged the start and survived a failed connection, the
	// subscription is running and the only thing left is to stop it.
	stderr.waitFor(t, "subscribing")
	stderr.waitFor(t, "the subscription failed")

	start := time.Now()
	cancel()
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("run(daemon) = %d after the cancellation; want 0. stderr:\n%s", got, stderr.String())
		}
	case <-time.After(shutdownBudget):
		t.Fatalf("run(daemon) did not return within %v of the cancellation. stderr:\n%s",
			shutdownBudget, stderr.String())
	}
	if took := time.Since(start); took > shutdownBudget {
		t.Errorf("the daemon took %v to stop; want less than %v", took, shutdownBudget)
	}
	if !strings.Contains(stderr.String(), "stopped") {
		t.Errorf("the daemon did not log that it stopped; stderr:\n%s", stderr.String())
	}
}

// TestDaemonReportsABadConfiguration covers the path the service unit takes when
// a user has not written the file yet: report and exit, do not loop.
func TestDaemonReportsABadConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	stderr := &syncBuffer{}
	if code := run(context.Background(), []string{"daemon"}, io.Discard, stderr); code != 1 {
		t.Fatalf("run(daemon) with no configuration = %d; want 1", code)
	}
	if !strings.Contains(stderr.String(), "config.toml") {
		t.Errorf("stderr = %q; want the path of the configuration", stderr.String())
	}
}

// testTranslator is the source language: it needs no catalogues.
func testTranslator() *i18n.Translator {
	return i18n.New("en")
}

// syncBuffer is a bytes.Buffer the program writes to from its own goroutine
// while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor waits until the buffer holds want.
func (b *syncBuffer) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the log never contained %q; it holds:\n%s", want, b.String())
}

// discardLogger is a logger that writes nowhere: the tests care about the calls,
// not about the lines.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
