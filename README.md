# ntfy-desktop

![ntfy-desktop](assets/banner.png)

An [ntfy](https://ntfy.sh) client for the Linux desktop.

ntfy has a web app, a mobile app and a CLI — but no desktop client for Linux. This project is that
client: it subscribes to a topic, shows what arrives as native notifications, lets you answer
requests from a desktop dialog, and publishes messages back.

**Status: pre-release (0.3.0).** The daemon subscribes, notifies, asks for decisions one at a time
and runs the chosen action; `publish` and `history` work from the terminal; the client installs as a
user service that starts with the graphical session. Verified end to end on KDE Plasma 6.6.7
(Wayland): a message published from another machine opened a dialog with three labelled buttons, the
click called the endpoint in the message, and the endpoint received it. See the
[roadmap](#roadmap) for what is still open.

## What it does

- Subscribes to one or more ntfy topics over the JSON stream, and reconnects with a growing wait
  when the server goes away.
- Shows each message as a native desktop notification, with the urgency that matches its priority.
- Opens a desktop dialog for messages that carry actions, one at a time, and runs the chosen action:
  open its link or call its URL. One action is one button, two actions are three buttons labelled
  from the catalog, and the dialog wears the urgency of the message as its icon.
- Publishes a message to a topic, with the body read from stdin and a title, priority, tags, click
  target and Markdown when they are asked for.
- Keeps a local store for the messages received, listed by `ntfy-desktop history`.
- Runs as a user service from the graphical session, so messages arrive without a terminal open.

## Platforms

| Desktop | Status |
|---|---|
| KDE Plasma and derivatives (Kubuntu, KDE neon) | first target, verified on Plasma 6.6.7 |
| GNOME and derivatives (Ubuntu, Fedora Workstation) | adapter written, not yet verified on a real session |
| Others (XFCE, Cinnamon, Sway) | out of scope for now |

The client does not link Qt or GTK. It delegates dialogs to the desktop's own tool — `kdialog` on
Plasma, `zenity` on GNOME — and detects which one to use at startup. One build, native look where it
runs. Notifications go over the freedesktop D-Bus service, so they are the desktop's own.

## Requirements

- Linux, with a graphical session.
- Nothing else: the binary is built without cgo, so there are no shared library dependencies.

On KDE, dialogs need `kdialog`, which ships with `kde-cli-tools` — a dependency of `plasma-desktop`,
so it is already there on any Plasma install. On GNOME the equivalent is `zenity`.

## Build from source

```sh
git clone https://github.com/maurorosero/ntfy-desktop
cd ntfy-desktop

make bootstrap     # downloads go1.27.4 into .toolchain/ and verifies its sha256
make check         # gofmt + vet + tests + staticcheck + spdx + build
```

No Go installation is needed: `make` uses the toolchain inside the repository.

## Install

Every release publishes an apt and an rpm repository, so the distribution's package manager keeps the
client up to date:

```sh
# Debian, Ubuntu and derivatives
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://maurorosero.github.io/ntfy-desktop/keys/repo-key.asc \
  | sudo tee /etc/apt/keyrings/ntfy-desktop.asc >/dev/null
echo "deb [signed-by=/etc/apt/keyrings/ntfy-desktop.asc] https://maurorosero.github.io/ntfy-desktop/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/ntfy-desktop.list
sudo apt update && sudo apt install ntfy-desktop
```

```sh
# Fedora, openSUSE and derivatives
sudo curl -fsSL https://maurorosero.github.io/ntfy-desktop/keys/repo-key.asc \
  -o /etc/pki/rpm-gpg/RPM-GPG-KEY-ntfy-desktop
sudo dnf config-manager --add-repo https://maurorosero.github.io/ntfy-desktop/rpm/x86_64
sudo dnf install ntfy-desktop
```

The `.deb` and `.rpm` files are also attached to each release, for a one-off install:

```sh
sudo dpkg -i ntfy-desktop_0.3.0_linux_amd64.deb     # Debian, Ubuntu and derivatives
sudo rpm -i  ntfy-desktop-0.3.0-1.x86_64.rpm        # Fedora, openSUSE and derivatives
```

From source, without root, everything goes to `~/.local`:

```sh
make build
./install.sh              # binary, menu entry, icons and the user service
./install.sh --bin-only   # just the binary, nothing else
./install.sh --dry-run    # prints what it would do
./install.sh --uninstall  # removes what it installed, leaves the configuration alone
```

No root is needed for the default prefix (`~/.local`). The unit is written with the real path of the
installed binary, so a `--prefix` other than `~/.local` still starts, and if the script runs where
there is no user bus — over SSH, for instance — it leaves the unit in place and prints the two
commands that enable it from the session.

Details: [docs/usage.md](docs/usage.md).

## Run

```sh
ntfy-desktop                        # prints the detected environment
ntfy-desktop --version              # version and commit
ntfy-desktop --daemon               # subscribes until it is stopped
ntfy-desktop publish [topic]        # body from stdin
ntfy-desktop history [--unread]
```

With no arguments it prints what it detects about the session and exits:

```sh
ntfy-desktop 0.3.0
  user       mrosero
  locale     en
  desktop    kde
  dialogs    /usr/bin/kdialog
```

`--daemon` is the mode the service unit starts: it reads the configuration, subscribes, shows what
arrives, asks for decisions and stays in the foreground so systemd can supervise it. `SIGTERM` ends
it in well under a second, and any dialog it opened is killed with it.

The log level is `info` unless `--log-level` or the `log-level` key says otherwise (`debug`, `info`,
`warn`, `error`); the flag wins over the file.

### Publish

```sh
echo "Backup done" | ntfy-desktop publish casa
echo "Disk almost full" | ntfy-desktop publish casa --title "Alert" --priority 5 --tags warning,disk
```

The body is always read from stdin, so a message is never taken for a flag. The topic is the
argument, `--topic`, or the one in the configuration. `--title`, `--priority`, `--tags`, `--click`
and `--markdown` set what the message carries. On success the command prints the id the server
assigned and nothing else; a refused publish exits with status 1 and says why.

### Decisions

A message with actions opens a dialog. The answer is the action itself: choosing *Aprobar* calls the
URL the message carried, so whatever can receive an HTTP request can be the other half of a
decision. One dialog at a time, ten minutes to answer, and closing it without answering puts the
request back in line instead of losing it.

The contract — how to publish a request, which kinds run, what the client refuses — is in
[docs/decisions.md](docs/decisions.md).

## Configuration

One file, written by you — a package never puts anything in your home:

```
$XDG_CONFIG_HOME/ntfy-desktop/config.toml      # normally ~/.config/ntfy-desktop/config.toml
```

```toml
server    = "https://ntfy.example.org"   # required, http or https, no query, no fragment
topic     = "alerts"                     # required: 1 to 64 of [A-Za-z0-9_-] each, comma separated
user      = "andrea"                     # optional, together with password
password  = "…"                          # optional, together with user
log-level = "info"                       # optional: debug, info, warn or error
```

`topic` takes one name, or several separated by commas (`topic = "casa, pruebas"`): the daemon
subscribes to all of them over one connection and each message keeps its own topic in the
notification and the history. `publish` sends to one at a time, so with several configured the
destination is passed as an argument or with `--topic`.

Leave `user` and `password` out to subscribe to a public topic; set both to use HTTP Basic on a
protected one. Half a pair is a configuration error, not an anonymous subscription. Any other key is
an error too, so a typo is never silently ignored.

Full reference, with what is refused and why: [docs/configuration.md](docs/configuration.md).

## Run with the session

`install.sh` installs and enables the unit; done by hand it is:

```sh
install -Dm644 packaging/ntfy-desktop.service ~/.config/systemd/user/ntfy-desktop.service
systemctl --user daemon-reload
systemctl --user enable --now ntfy-desktop.service
journalctl --user -u ntfy-desktop -f
```

The unit starts and stops with `graphical-session.target`, restarts the client if it aborts, and
leaves a journal behind. It runs with `ProtectHome=read-only`, so it writes only where the client
needs to: `~/.config/ntfy-desktop` (configuration), `~/.local/share/ntfy-desktop` and
`~/.cache/ntfy-desktop` (the history).

When something does not arrive, [docs/troubleshooting.md](docs/troubleshooting.md) is the page to
open.

## Documentation

| Page | What it covers |
|---|---|
| [docs/usage.md](docs/usage.md) | commands, exit statuses, service, environment |
| [docs/configuration.md](docs/configuration.md) | the configuration file and its validation |
| [docs/decisions.md](docs/decisions.md) | publishing and answering decision requests |
| [docs/architecture.md](docs/architecture.md) | packages, dependencies, queue, notifications, tests |
| [docs/troubleshooting.md](docs/troubleshooting.md) | what to look at when nothing arrives |
| [docs/release.md](docs/release.md) | how a version is tagged and delivered |
| [plan/alcance.md](plan/alcance.md) | scope, milestones and decisions (Spanish) |

## Project layout

```
cmd/ntfy-desktop/     entry point and wiring
internal/action/      running the chosen action: open a link, call a URL
internal/config/      configuration loading and validation
internal/dialog/      asking a decision: kdialog and zenity adapters
internal/history/     the local store of received messages
internal/i18n/        the catalog and the language
internal/notify/      desktop notifications over the freedesktop D-Bus service
internal/ntfy/        ntfy protocol: JSON stream, publishing, actions, reconnection
internal/platform/    session user, locale and desktop detection
internal/queue/       the line of requests: one dialog at a time
internal/version/     version injected at build time
locales/              the catalogs: .pot template and one .po per language
docs/                 the user documentation
assets/               icon (SVG + PNG) and banner
packaging/            .desktop file and systemd user unit
plan/                 scope, milestones and design decisions
tools/                Go toolchain bootstrap, repo metadata and git hooks
```

## Roadmap

1. Read: connect and subscribe. — done
2. Notify: one notification per message. — done
3. Decide: actions, dialog, queue. — done
4. Resilience: reconnect, start with the session. — done
5. Publish. — done
6. History: store and `history` command. — done
7. History wiring: the daemon records what arrives and marks what is answered. — open
8. Portability: verify the GNOME adapter on a real session. — open
9. Packaging: `.deb` and a release pipeline with signed binaries. — open

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Contributions are accepted under the Developer
Certificate of Origin (`git commit -s`).

## Security

See [`SECURITY.md`](SECURITY.md) for how to report a vulnerability privately.

## License

GPL-3.0-or-later. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

ntfy is a separate project by Philipp C. Heckel. This client is not affiliated with it.
