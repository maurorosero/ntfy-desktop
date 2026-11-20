// SPDX-License-Identifier: GPL-3.0-or-later

// ntfy-desktop is an ntfy client for the Linux desktop: it subscribes to a
// topic, shows what arrives as native notifications, and asks for a decision
// through the desktop's own dialog when a message carries actions.
//
// main does two things: it sets up the cancellation that a signal triggers, and
// it hands the command line to run, which dispatches on the subcommand.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// SIGINT and SIGTERM cancel the context; every goroutine of the program
	// watches it, so the process ends after the dialogs it opened are gone.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
