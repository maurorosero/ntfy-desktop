# ntfy-desktop

![ntfy-desktop](assets/banner.png)

An [ntfy](https://ntfy.sh) client for the Linux desktop.

ntfy has a web app, a mobile app and a CLI — but no desktop client for Linux. This project is
that client: it subscribes to a topic, shows what arrives as native notifications, lets you act
on messages from a desktop dialog, and publishes messages back.

**Status: skeleton (0.0.1).** The binary detects your environment and prints it. It does not
talk to a server yet. See the [roadmap](#roadmap).

## What it will do

- Subscribe to one or more ntfy topics over the JSON stream.
- Show each message as a native desktop notification, with priority and tags.
- Open a desktop dialog for messages that carry actions, and send the answer back.
- Publish messages to a topic.
- Keep a local history, so messages that arrived while you were away are not lost.
- Queue decisions: one dialog at a time, never two.

## Platforms

| Desktop | Status |
|---|---|
| KDE Plasma and derivatives (Kubuntu, KDE neon) | first target |
| GNOME and derivatives (Ubuntu, Fedora Workstation) | scaffolded, not implemented |
| Others (XFCE, Cinnamon, Sway) | out of scope for now |

The client does not link Qt or GTK. It delegates dialogs to the desktop's own tool — `kdialog`
on Plasma, `zenity` on GNOME — and detects which one to use at startup. One build, native look
where it runs.

## Requirements

- Linux, with a graphical session.
- Nothing else: the Go toolchain lives inside the repository and the binary is built without
  cgo, so there are no shared library dependencies.

On KDE, dialogs need `kdialog`, which ships with `kde-cli-tools` — a dependency of
`plasma-desktop`, so it is already there on any Plasma install.

## Build from source

```sh
git clone https://github.com/maurorosero/ntfy-desktop
cd ntfy-desktop

make bootstrap     # downloads go1.27.4 into .toolchain/ and verifies its sha256
make check         # gofmt + vet + tests + build
```

No Go installation is needed: `make` uses the toolchain inside the repository.

## Run

```sh
./bin/ntfy-desktop            # prints the detected environment
./bin/ntfy-desktop -version   # prints the version
```

Example output:

```
ntfy-desktop 0.0.1
  user       mrosero
  locale     es_PA
  desktop    kde
  dialogs    /usr/bin/kdialog
```

## Configuration

One file, written by you — a package never puts anything in your home:

```
$XDG_CONFIG_HOME/ntfy-desktop/config.toml      # normally ~/.config/ntfy-desktop/config.toml
```

```toml
server   = "https://ntfy.example.org"   # required, http or https, no query, no fragment
topic    = "alerts"                     # required: 1 to 64 of [A-Za-z0-9_-]
user     = "andrea"                     # optional, together with password
password = "…"                          # optional, together with user
```

Leave `user` and `password` out to subscribe to a public topic; set both to use HTTP Basic on a
protected one. Half a pair is a configuration error, not an anonymous subscription: the loader
says which key is missing instead of quietly connecting without credentials.

The loader and its validation live in `internal/config`; comments, blank lines and one
`key = "value"` per line are supported, with `\"`, `\\`, `\n`, `\t` and `\r` escapes. The binary
does not read the file yet: subscribing is the first milestone, so for now the file only matters
to the tests.

## Project layout

```
cmd/ntfy-desktop/     entry point
internal/config/      configuration loading and validation
internal/ntfy/        ntfy protocol: JSON stream, authentication, reconnection
internal/platform/    session user, locale and desktop detection
internal/version/     version injected at build time
assets/               icon (SVG + PNG) and banner
packaging/            .desktop file and systemd user unit
plan/                 scope, milestones and design decisions
tools/                Go toolchain bootstrap and git hooks
```

## Roadmap

Full scope, milestones and acceptance criteria live in [`plan/alcance.md`](plan/alcance.md)
(written in Spanish). In short:

1. Read: connect and subscribe.
2. Notify: one notification per message.
3. Decide: actions, dialog, queue.
4. Resilience: reconnect, catch up, start with the session.
5. History.
6. Publishing.
7. Portability: verify the GNOME adapter on a real machine.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Contributions are accepted under the Developer
Certificate of Origin (`git commit -s`).

## Security

See [`SECURITY.md`](SECURITY.md) for how to report a vulnerability privately.

## License

GPL-3.0-or-later. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

ntfy is a separate project by Philipp C. Heckel. This client is not affiliated with it.
