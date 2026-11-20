# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Nothing yet.

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

[Unreleased]: https://github.com/maurorosero/ntfy-desktop/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/maurorosero/ntfy-desktop/releases/tag/v0.0.1
