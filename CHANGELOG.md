# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- Nothing yet.

## [0.2.0] - 2026-11-20

### Added

- Every release also publishes an apt and an rpm repository on GitHub Pages:
  `apt install ntfy-desktop` after adding the key and the source, and `dnf install`
  likewise. Both are signed with a key of their own, not with a personal one, and old
  versions stay installable.
- A tag on `main` delivers the release: `.github/workflows/release.yml` checks that the tag sits on
  the merge commit of the release pull request and that the version exists in this file, and then
  builds and publishes `.deb` and `.rpm` packages for `amd64` and `arm64` — binary in `/usr/bin`, menu
  entry, icons, user unit and docs — plus the plain `tar.gz` archives and `checksums.txt`. The version
  inside each binary comes from the tag.
- Run as a daemon: `--daemon` subscribes, shows every message as a notification and asks for the
  decisions its actions carry, until it is told to stop. `SIGTERM` and `SIGINT` end it in well under
  a second, killing any dialog it opened, and the log goes to stderr at a level set by `--log-level`
  or the `log-level` key.
- The user unit in `packaging/` starts it with the graphical session and restarts it if it aborts.
- Show a message's actions in a native desktop dialog — `kdialog` on Plasma, `zenity` on GNOME — with
  the sender's own labels, and run the chosen action: open its link or call its URL. One dialog at a
  time, with the pending count in the title; closing it leaves the request in the queue, and a
  request nobody answers before its deadline is answered as expired.
- Show each message as a native desktop notification: the message title and body, the urgency that
  matches its priority, a timeout of 30 to 60 seconds, and the message link opening in the desktop's
  browser when the notification is clicked.
- `ntfy-desktop history` lists the messages kept in a local history: date, topic, title and the first
  line of each. `--unread` narrows the listing to the ones that have not been read yet, and the store
  keeps the last 500 messages under the user cache directory, dropping the oldest as new ones arrive.
- Load the configuration file and subscribe to an ntfy topic's JSON stream, with optional HTTP Basic
  authentication and automatic reconnection.
- Publish a message to a topic with `publish`: the body is read from stdin, so it is never taken for a
  flag, and `--title`, `--priority`, `--tags`, `--click` and `--markdown` set what it carries. It
  authenticates like the daemon, prints the id the server assigned and reports a refused publish
  (HTTP 403) with the reason to look at.

### Changed

- A decision with two actions is asked with real buttons instead of a list: kdialog's `--yesnocancel`
  and zenity's `--question` both rename their buttons from the catalog, so answering is one click
  instead of picking a row and then pressing OK. One action gets a single button; three or more keep
  the list, which now carries the way out once and is sized to show every row. The dialog also wears
  the urgency of the message as its icon: warning at priority 4 and above, information at 2 and
  below, a question otherwise.
- The queue waits a quarter of a second before opening the first dialog of a batch, so the counter in
  the title says how many are waiting behind it: three messages that arrive together read
  "Decision (1 of 3)" instead of "Decision".
- `install.sh` installs and enables the user unit along with the binary, the menu entry and the icons,
  and writes it with the real path of the installed binary instead of assuming `/usr/bin`. The unit
  goes to `<prefix>/share/systemd/user/`, which systemd --user already searches, so a prefix other
  than `~/.local` works too. `--bin-only` installs the binary and nothing else.
- The unit no longer blocks the history: it runs with `ProtectHome=read-only`, and now lists
  `~/.cache/ntfy-desktop` among the paths it may write, where the store lives.
- Releases are prepared with a dossier and written by an agent; `main` exists only on the remote.

### Fixed

- The apt repository listed its packages as `apt/pool/...` while the repository URL already ends
  in `/apt`, so every download was a 404. The index is now built from inside `apt/`, and
  `apt-get download ntfy-desktop` fetches the package whose checksum the index announces.
- The first delivery attempt stopped before publishing anything: goreleaser only builds from a
  clean tree, and the release notes were written inside it. They now go outside the work tree,
  so a tag on `main` reaches the packages without a manual step.
- zenity exits 1 for its extra button, the same code as its own cancel button, so a rejection on
  GNOME was read as "no answer" and the request stayed pending. The client now sets `ZENITY_EXTRA`
  when it starts the dialog, which zenity reads to give that button its own code.

## [0.1.0] - 2026-11-20

### Added

- CHANGELOG entries are now written as fragments in `.changes/unreleased/` and assembled at release time by `tools/changelog.sh`.

### Changed

- `devs` is the working branch and accepts direct pushes; `main` only receives releases through a pull request with a merge commit.

## [0.0.1] - 2026-11-20

First public skeleton. The binary does not connect to an ntfy server yet.

### Added

- Project scaffolding: `cmd/`, `internal/`, `assets/`, `packaging/`, `plan/`, `tools/`.
- Session environment detection: user (`os/user`), locale (`LC_ALL`, `LC_MESSAGES`, `LANG`,
  with `C`/`POSIX` falling back to `en`) and desktop family from `XDG_CURRENT_DESKTOP`.
- Dialog tool detection: `kdialog` preferred on KDE, `zenity` on GNOME, with fallback.
- Go toolchain bootstrap script that downloads go1.27.4 into `.toolchain/` and verifies its
  official sha256 before extracting. No system installation, no root.
- `Makefile` that resolves the toolchain from the repository, so no Go install is required.
- Unit tests for desktop classification, locale normalization and neutral-locale fallback.
- Pre-commit hooks: gofmt, go vet, go test, govulncheck, gitleaks and SPDX header check.
- GitHub Actions workflow: formatting, vet, tests, govulncheck, SPDX check, cgo-free build for
  `linux/amd64` and `linux/arm64`, and secret scanning.
- Dependabot configuration for Go modules and GitHub Actions.
- GoReleaser configuration for `linux/amd64` and `linux/arm64` with checksums.
- Application icon as SVG plus PNG at 64, 128, 256 and 512 px, and a 1280×640 banner.
- `.desktop` entry (validated with `desktop-file-validate`) and a hardened systemd user unit.
- Internationalization: gettext catalogs (`locales/<lang>/LC_MESSAGES/ntfy-desktop.po`)
  embedded in the binary with `//go:embed`, source language English, Spanish translation,
  fallback chain `es_PA -> es -> en` and `C`/`POSIX` mapping to `en`.
- `about.me` and `description.me` as the single source for the repository's About and
  Description; `tools/repo-meta.sh` syncs both (and the topics) to GitHub with `gh`.
- `install.sh`: installs without root into `~/.local` (or `--prefix`), with `--dry-run` and
  `--uninstall`; warns when the desktop's dialog tool is missing.
- CI now runs `staticcheck` and a DCO check on pull requests; `tools/hooks/dco.sh` verifies
  the `Signed-off-by` trailer against each commit author.
- License: GPL-3.0-or-later, with `LICENSE` and `NOTICE`.
- `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1) and `SECURITY.md`.

### Known limitations

- No daemon mode yet: the systemd unit refers to `--daemon`, which arrives with milestone 1.
- Spanish is the only translation so far; English is the source language.
- GNOME support is scaffolded but unverified; there is no GNOME machine in the test setup.

[0.1.0]: https://github.com/maurorosero/ntfy-desktop/releases/tag/v0.1.0
[0.2.0]: https://github.com/maurorosero/ntfy-desktop/releases/tag/v0.2.0
[Unreleased]: https://github.com/maurorosero/ntfy-desktop/compare/v0.2.0...HEAD
[0.0.1]: https://github.com/maurorosero/ntfy-desktop/releases/tag/v0.0.1
