# Contributing to ntfy-desktop

Thanks for taking the time to contribute. This document explains how the project is built, what
a good change looks like, and the few rules that keep it maintainable.

## Before you start

- For anything larger than a typo, **open an issue first**. It is cheaper to agree on the
  approach than to rewrite a pull request.
- This is a GPL-3.0-or-later project. By contributing you agree to license your work under the
  same terms.

## Developer Certificate of Origin

Every commit must be signed off:

```sh
git commit -s -m "your message"
```

That adds a trailer to the commit:

```
Signed-off-by: Your Name <you@example.com>
```

It certifies that you wrote the change, or have the right to submit it. There is no CLA and no
paperwork. A pull request whose commits are not signed off cannot be merged.

## Set up

```sh
git clone https://github.com/maurorosero/ntfy-desktop
cd ntfy-desktop
make bootstrap          # toolchain inside the repository, no system install
pre-commit install      # optional but recommended
```

## Build and test

```sh
make check     # gofmt + vet + tests + build
make test      # tests only
make vuln      # govulncheck
```

A change is ready when `make check` passes locally. The same checks run on every pull request.

## What we are looking for

- **Small, focused pull requests.** One topic per PR. A change that does three things is three
  pull requests.
- **Tests for logic.** Pure functions — parsing, classification, formatting — must have table
  tests. Anything that touches a process or the network should be isolable behind an interface.
- **No new dependencies without justification.** The project ships a single cgo-free binary and
  wants to keep it that way. Explain in the issue why the standard library is not enough.
- **No silent failures.** Errors are returned, wrapped with context, and logged once at the
  boundary. A failure the user cannot see is a bug of its own.

## What will not be accepted

These are design decisions, not preferences:

- **Linking Qt or GTK.** The client delegates dialogs to the desktop's own tool. A toolkit
  dependency would make the binary look foreign on one desktop and bind it to a library for
  years.
- **cgo.** It breaks the single-static-binary promise and complicates cross-compilation.
- **A dependency that is not GPL-compatible.** Check the license before proposing it.
- **Telemetry, analytics or phone-home of any kind.**
- **Writing into the user's home from a package.** Configuration belongs to the user.

## Code style

- `gofmt` and `go vet` clean, always.
- Standard library first. `net/http`, `encoding/json`, `os/exec` cover most of what this project
  needs.
- Every `.go` file starts with `// SPDX-License-Identifier: GPL-3.0-or-later`.
- Identifiers and comments in English. User-facing strings are English in the source and are
  translated through the catalogs.
- Keep the platform layer thin: detection and process launching live in `internal/platform`,
  and the core never calls `kdialog` directly.

## Commit messages

Imperative mood, short first line, body explaining the why:

```
Add topic subscription with catch-up

Reconnects with exponential backoff and resumes from the last
message id, so messages sent while the machine was off are not lost.
```

Prefixes are optional but appreciated: `docs:`, `ci:`, `deps:`, `packaging:`.

## Translations

Strings live in `.po` files under `locales/`. To contribute a language:

1. Copy `locales/ntfy-desktop.pot` to `locales/<lang>.po`.
2. Translate the entries. Keep `msgid` untouched.
3. Test with `LC_ALL=<lang> ./bin/ntfy-desktop`.
4. Open a pull request with the signed-off `.po` file.

English is the source language; Spanish is the first translation. Both must stay complete —
a release with a missing string in either is not released.

## Review

`main` and `devs` are protected: pull requests only, required checks, linear history, no
force-push, and administrators are subject to the same rules. Required approvals are zero
because the project has a single maintainer; **the checks are what gate the merge**.

## Branches: `topic → devs → main`

Two permanent branches and nothing else:

```
main     what gets released. Receives merges from devs only; tags are cut here.
devs     integration. All work lands here first.
topic    feat/something, fix/something, docs/something: hours or days, deleted on merge.
```

The flow, in order:

```sh
git switch devs && git pull
git switch -c fix/something
# ... work ...
git commit -s
git push -u origin fix/something
gh pr create --base devs              # 1) topic -> devs
# once checks pass: merge
gh pr create --base main --head devs  # 2) devs -> main, when releasing
```

Rules that keep this from stepping on itself:

- **Never commit on `main` or `devs`.** Both reject direct pushes, the owner included.
- **`devs` stays ahead of `main`, never the other way round.** If something fixed on `main` is
  needed in `devs`, merge `main` into `devs`; never rewrite.
- **One topic, one branch, one PR.** Short branches: if it takes weeks, split it.
- **Tags are cut from `main`**, never from `devs`.
- Merge commits created by GitHub are not signed; the DCO check ignores them (`--no-merges`)
  and reviews only the PR's own commits.
- Bot commits (Dependabot, Renovate, `[bot]`) are skipped by the DCO check: they cannot sign.

## Reporting bugs

Use the issue template. Include:

- the output of `./bin/ntfy-desktop` (it prints user, locale, desktop and dialog tool),
- your distribution and desktop version,
- what you expected and what happened.

Never paste tokens, passwords or full message contents from a private topic.
