# Troubleshooting

Start with the journal: the daemon logs to stderr, which systemd turns into the journal.

```sh
systemctl --user status ntfy-desktop
journalctl --user -u ntfy-desktop -n 50
journalctl --user -u ntfy-desktop -f
```

For a one-off run with everything on: `ntfy-desktop daemon --log-level debug`.

## Nothing arrives

1. **Is it running?** `systemctl --user is-active ntfy-desktop` → `active`.
2. **Did it subscribe?** The first line of the journal names the server, the topic and the mode:
   ```
   msg=subscribing server=https://ntfy.example.org topic=casa auth=basic
   ```
   If it is not there, the configuration could not be read — the line right before says which key.
3. **Is the topic the same one you published to?** Topics in ntfy are created by publishing, so a
   typo on either side gives a perfectly working subscription to nothing.
4. **Are the credentials right?** A wrong pair is a 403 the client logs as
   `the subscription failed, retrying` — and it will keep retrying, so the journal fills with the
   same line rather than going quiet.
5. **Publish by hand** and watch: `echo hola | ntfy-desktop publish casa`.

## The notification appears but the dialog does not

The dialog needs the desktop's own tool. With no tool the client says so once, at startup:

```
level=WARN msg="no desktop dialog: decisions will not be asked"
```

| Desktop | Tool | Package |
|---|---|---|
| Plasma | `kdialog` | `kde-cli-tools` (usually already installed) |
| GNOME | `zenity` | `zenity` |

`ntfy-desktop` with no arguments prints which one it found:

```
  desktop    kde
  diálogos   /usr/bin/kdialog
```

If it says `no dialog tool found`, install one. Notifications still work; decisions are logged as
unavailable instead of being answered blindly.

## A decision ran but the other end did not get it

The journal records the outcome of every action:

```sh
journalctl --user -u ntfy-desktop | grep -E "action run|action failed"
```

`action run` means the call returned. `the chosen action failed` carries the real error, and the two
usual ones are:

- **`context deadline exceeded`** — the endpoint is unreachable from where the client runs. A URL on
  a private network only works on that network; this is the classic one when testing away from home.
- **`ntfy answered 403`** — the action endpoint itself refused the call, not the client.

## The dialog stays on screen after stopping the service

It should not: the dialog is a child process and is killed with the context. If one is left behind,
it belongs to a process that was killed with `SIGKILL` (`kill -9`), which leaves no chance to clean
up. Close it and start the service again.

## "plasmashell crashed" right after a command

That report comes from running the desktop's own binaries in a session that cannot host them — for
example `plasmashell --version` over SSH — not from the client. Ask the package instead:

```sh
dpkg-query -W -f='${Version}\n' plasma-workspace
```

## The service does not start with the session

- `systemctl --user is-enabled ntfy-desktop` → `enabled`.
- The unit is `PartOf=graphical-session.target`: it starts with the session, so it needs one. On a
  machine where you log in through a display manager, that is automatic.
- Five failures in a minute stop it (`StartLimitBurst=5`); after fixing the cause,
  `systemctl --user reset-failed ntfy-desktop` and start it again.

## History is empty

`ntfy-desktop history` reads `$XDG_CACHE_HOME/ntfy-desktop/history`. Under the service the unit may
write only to the paths it declares, and `~/.cache/ntfy-desktop` is one of them; if you changed the
unit, keep it.

Note that **the daemon does not write the history yet** — the store and the command are in place, and
wiring them into the subscription is a separate task.

## A file of the history is broken

The client reports it and starts that topic empty, without stopping:

```
level=WARN msg="a stored history was discarded" error="…: not a history file"
```

Deleting `$XDG_CACHE_HOME/ntfy-desktop` is always safe.

## The configuration is rejected

Every error names the key and the reason:

```
msg="the configuration is not usable" key=password reason="requires a password"
```

See [configuration](configuration.md) for what is accepted. The five keys are the whole file: an
unknown key is an error, not something to ignore.

## Still stuck

Collect these and open an issue:

```sh
ntfy-desktop --version
systemctl --user status ntfy-desktop
journalctl --user -u ntfy-desktop -n 100 --no-pager
echo $XDG_CURRENT_DESKTOP; command -v kdialog zenity
```

The configuration file: **do not paste it** if it holds a password. Paste the journal line that names
the key instead.
