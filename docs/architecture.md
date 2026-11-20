# Architecture

The client is one Go binary built without cgo. It links neither Qt nor GTK: it talks to the desktop
through the session it already runs in — the D-Bus notification service for notifications, and the
desktop's own dialog tool as a child process for decisions. That is why one build looks native on
Plasma and on GNOME, and why there is nothing to compile per distribution.

## Packages

```
cmd/ntfy-desktop/     the command line: parsing, subcommands, wiring
internal/ntfy/        ntfy protocol: JSON stream, publishing, actions, reconnection
internal/config/      the configuration file: loading and validation
internal/notify/      notifications over org.freedesktop.Notifications (D-Bus)
internal/dialog/      asking a decision: the Decider contract, kdialog and zenity
internal/action/      running the chosen action: open a link, call a URL
internal/queue/       the line of requests: one dialog at a time, deadlines, retries
internal/history/     what arrived, kept under the user cache directory
internal/platform/    who and where: session user, locale, desktop, dialog tool
internal/i18n/        the catalog and the language
internal/version/     version injected at build time
```

A package is created when it changes for its own reason, can be tested on its own, or has more than
one implementation. Not before.

## The dependencies point one way

```
cmd/  ──> queue ──> dialog ──> ntfy
  │        │          └──> action ──> ntfy
  ├──> notify
  ├──> config
  ├──> history
  ├──> platform
  └──> i18n
```

- The core (`ntfy`, `config`) knows nothing about the desktop.
- The adapters (`notify`, `dialog`, `action`) declare at their consumer what they need: `queue`
  defines `Decider` and `Executor`, so `queue` can be tested with fakes and `dialog` knows nothing
  about lines or deadlines.
- `cmd/` is the only place where the pieces are wired together. There is one wiring, in `run`.
- No cycles: `go list -deps` on any package shows a straight line.

## The queue

`queue` owns the order of decisions, and it is the piece with the most rules:

- `Submit` never blocks and never asks; `Run` owns the dialogs.
- A request that comes back unanswered goes to the end of the line, and the queue waits before
  reopening it, so a person who is busy does not get the same dialog twice in a row.
- A deadline covers the dialog: when it passes, the child process is killed and the request is
  answered as expired. Expired is an answer the caller can see, not a silent drop.
- `WithSettle` waits a quarter of a second before the first dialog of a batch, so the counter in the
  title counts the burst whole. It is off by default; the daemon turns it on.

## Notifications

A notification is a D-Bus call to `org.freedesktop.Notifications`, not `notify-send`: the command
line tool drops the actions on the floor and prints that it does, and the desktop then draws a
notification with no buttons. The client speaks the protocol directly, with:

- the urgency that matches the priority (1–2 low, 3 normal, 4–5 critical);
- an explicit timeout (30, 45 or 60 seconds per urgency — **never** 0, which closes it at once);
- the hint that keeps a notification in the history until it is touched, so a message that arrived
  while the screen was off is still there.

Capability queries and the call itself are bounded by a deadline: a bus that does not answer must not
keep the client from closing.

## Dialogs

`kdialog` on Plasma, `zenity` elsewhere, chosen at startup from `XDG_CURRENT_DESKTOP` and from what
is installed. Two actions are asked with two real buttons — three, counting *Dismiss* — instead of a
list plus an OK button: one click, and the labels are ours, from the catalog, in the person's
language.

Two exit-code details are load-bearing and are commented where they are used:

- `kdialog` returns 0 for yes, 1 for no and 2 for cancel;
- `zenity` returns 1 for its extra button **and** for its cancel button, unless `ZENITY_EXTRA` is set,
  so the client sets it. Without that, a rejection on GNOME would be read as "no answer".

## Language

Every visible string is a msgid in English, extracted from the code, translated in
`locales/*/LC_MESSAGES/*.po` and compiled into the binary — there are no language files to install.
The fallback chain is `es_PA → es → en`. A missing translation shows the English text; a missing
msgid fails the test that compares the code against the catalogs.

## Tests

Nothing in the test suite opens a window, talks to a real ntfy server or sends a real notification:

- `internal/ntfy` uses `httptest` for the stream and the publish, plus a fuzz test for the JSON;
- `internal/queue` runs the whole order of requests through a fake dialog, including the deadline,
  the requeue and the counter;
- `internal/dialog` compares the command line each adapter builds, and the exit codes it reads;
- `cmd/ntfy-desktop` builds the daemon against a dead server and cancels it, which covers the wiring
  and the shutdown.

`make check` runs `gofmt`, `vet`, the tests, `staticcheck`, the SPDX header check, the permissions
hook, the secret scan, the build for both architectures and `govulncheck`.
