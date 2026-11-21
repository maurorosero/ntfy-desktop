# Using ntfy-desktop

`ntfy-desktop` is one binary with a few modes. With no arguments it reports what it detected about
the session and exits; the rest is the daemon the service unit starts, the `publish` command and the
`history` listing.

```sh
ntfy-desktop                        # what it detected: user, locale, desktop, dialog tool
ntfy-desktop --version              # the version of this binary
ntfy-desktop --daemon               # subscribe, notify, ask, until stopped
ntfy-desktop daemon --log-level debug
ntfy-desktop publish [topic]        # body from stdin
ntfy-desktop history [--unread]
ntfy-desktop config                 # wizard: server, credentials, topics
```

## Configuring it

The file and its keys are described in [configuration.md](configuration.md). It never has to be
written by hand: `config` reads it, verifies it and edits one thing at a time.

```sh
ntfy-desktop config                       # the wizard, one question at a time
ntfy-desktop config --gui                 # the same steps, one desktop dialog each
ntfy-desktop config --show                # the effective configuration and the path of the file
ntfy-desktop config --check               # verify file, server, credentials and every topic
ntfy-desktop config --set server=https://ntfy.sh
ntfy-desktop config topic --add avisos
ntfy-desktop config topic --remove avisos
```

The wizard writes only after showing the file it is about to write — password as `••••••••` — and
asking for confirmation. Answering nothing keeps the current value; `Ctrl-D` stops; cancelling
writes nothing. `--set`, `--add` and `--remove` validate the result with the same loader, write it
atomically and keep a `.bak` of the previous version.

`--gui` runs the same interview with the desktop's dialogs: `kdialog` on Plasma, `zenity` elsewhere.
With neither of them, or with no session to draw them in, it says so in one line and asks in the
terminal — it does not fail. On Plasma the buttons of the text dialogs carry the wording of the
toolkit in the language of the session, not the labels of the catalog: `kdialog` ignores the labels
the program passes to `--inputbox`, `--password` and `--checklist`, and has no `--button` option to
name them. The text inside each dialog, and every button of `zenity`, come from the catalog.

The password is never an argument, where `ps` and the shell history would see it: the wizard asks
for it without echoing, and `--password-stdin` reads it from the standard input for a script.
`--set password=…` is refused on purpose.

`--check` writes nothing and exits with a code that names what failed:

| Code | Meaning |
|---|---|
| 0 | everything verified |
| 1 | nothing was written: the wizard was cancelled |
| 2 | the file cannot be read, or the change would leave it invalid |
| 3 | the server does not answer, or it is not an ntfy server |
| 4 | the server rejected the credentials (401) |
| 5 | the credentials have no permission for a topic (403) |

The wizard and the commands that edit one key use the same numbers, with one deliberate difference:
a server that does not answer does not stop them — they save with a warning, because a configuration
can be written before the server is reachable. A refusal — not ntfy, 401, 403 — blocks and leaves the
file untouched.

What `--check` cannot tell you: whether a topic "exists" (in ntfy a topic exists once something is
published, and an empty one answers the same), whether the user may publish (that would leave a
message behind) or what the server's ACL allows beyond the 401 and the 403 it can see.


## The daemon

`--daemon` (or `daemon`) is the mode the user unit starts. It reads the configuration, subscribes to
the topic over the JSON stream, shows every message as a native notification, asks for decisions
through the desktop dialog, and stays in the foreground so systemd can supervise it: the log goes to
stderr, which is the journal.

- Reconnection is automatic, with a growing wait between attempts (1 s, doubling, capped at about a
  minute). A dropped connection is a warning in the log, not an exit.
- `SIGTERM` and `SIGINT` end it in well under a second, and any dialog it opened is killed with it.
- `--log-level` takes `debug`, `info`, `warn` or `error`, and wins over the `log-level` key.

Exit statuses:

| Status | Meaning |
|---|---|
| 0 | Ran and stopped normally |
| 1 | Could not read or validate the configuration, or the command failed |
| 2 | The command line itself was wrong |

## Publishing

```sh
echo "Backup done" | ntfy-desktop publish casa
echo "Disk almost full" | ntfy-desktop publish casa --title "Alert" --priority 5 --tags warning,disk
printf 'line one\nline two\n' | ntfy-desktop publish --topic alerts --markdown
```

The body is **always** read from stdin, so a message that starts with a dash is never taken for a
flag. The topic is the argument, `--topic`, or the one in the configuration — in that order. A
publish goes to one topic: when the configuration names several and neither the argument nor
`--topic` says which, the command stops and says how to choose instead of picking one.

| Flag | What it sets |
|---|---|
| `--title` | the message title |
| `--priority` | 1 to 5, or `min`, `low`, `default`, `high`, `max`, `urgent` |
| `--tags` | comma separated tags |
| `--click` | the URL the notification opens when tapped |
| `--markdown` | ask the client to render the body as Markdown |

On success the command prints the id the server assigned and nothing else, so a script can keep it.
A publish the server refuses exits with status 1 and says what happened — a 403 names the topic and
what to look at, never the password.

## History

```sh
ntfy-desktop history            # one line per message, newest first
ntfy-desktop history --unread   # only the ones that have not been read
```

Each line is the date, the topic, the title and the first line of the body; a star marks what has
not been read. The store lives in `$XDG_CACHE_HOME/ntfy-desktop/history`, one append-only file per
topic, and keeps the last 500 messages: older ones are dropped as new ones arrive, and the file is
compacted once it grows past that.

A file that cannot be read is reported and replaced instead of stopping the program, so
`rm -rf` on that directory is a safe way to start over.

The daemon writes the history: every `message` event it receives is stored as it arrives — the
`open` and `keepalive` events of the stream are not — and a message that was published while the
program was not running is read back from the server on the next start, because the subscription
resumes after the last id the store knows. A message stops being unread when the person answers its
decision, and when the notification is clicked to open its link.

## Running with the session

Install the unit with `./install.sh`, or by hand:

```sh
install -Dm644 packaging/ntfy-desktop.service ~/.config/systemd/user/ntfy-desktop.service
systemctl --user daemon-reload
systemctl --user enable --now ntfy-desktop.service
```

```sh
systemctl --user status ntfy-desktop
journalctl --user -u ntfy-desktop -f
systemctl --user restart ntfy-desktop
```

**After an upgrade, restart it.** A package cannot restart another person's user service, so the
client keeps running the previous binary — from a file that no longer exists — until its owner does:

```sh
systemctl --user daemon-reload
systemctl --user restart ntfy-desktop
```

The package prints those two lines when it finishes installing.

The unit declares `PartOf=graphical-session.target`, so it starts and stops with the session, and
`Restart=on-failure` with `StartLimitBurst=5` per minute: a configuration that is broken fails five
times and stops, instead of filling the journal forever.

It runs hardened (`ProtectSystem=strict`, `ProtectHome=read-only`) and may write only to
`~/.config/ntfy-desktop`, `~/.local/share/ntfy-desktop` and `~/.cache/ntfy-desktop`.

## Installing

```sh
./install.sh              # binary, menu entry, icons and the user service
./install.sh --bin-only   # just the binary
./install.sh --prefix DIR # somewhere else than ~/.local
./install.sh --dry-run    # what it would do
./install.sh --uninstall  # remove it, leaving the configuration alone
```

No root is needed for the default prefix. The unit is written with the real path of the installed
binary, so a different prefix starts too.

## Environment

| Variable | Used for |
|---|---|
| `XDG_CONFIG_HOME` | where `ntfy-desktop/config.toml` is read |
| `XDG_CACHE_HOME` | where `ntfy-desktop/history` is written |
| `XDG_CURRENT_DESKTOP` | which dialog tool to use: `kdialog` on KDE, `zenity` elsewhere |
| `LC_ALL`, `LC_MESSAGES`, `LANG` | the language, with `C` and `POSIX` falling back to `en` |
