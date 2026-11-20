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

## Branches: work on `devs`, release through `main`

```
devs     working branch. Commits and pushes go here directly. Integration.
main     what gets released. Only receives merges from devs, always through a pull request.
topic    feat/…, fix/…, chore/…: for changes that should not sit half-done on devs.
```

### Day to day

```sh
git switch devs
git pull --rebase
# ... work ...
make check                 # before every push: fmt, vet, tests, build
git commit -s -m "..."
git push origin devs
```

For anything that takes several steps, use a topic branch and merge it into `devs` once green:

```sh
git switch -c feat/something origin/devs
# ... work ...
git switch devs && git merge --no-ff feat/something && git push origin devs
git branch -d feat/something
```

### Releasing: `devs → main`, always by pull request

```sh
gh pr create --base main --head devs --title "..." --body "..."
gh pr checks <n>           # the four checks must be green
gh pr merge <n> --merge        # a merge commit marks the release
git switch main && git pull && git tag -a v0.x.y -m "..." && git push origin v0.x.y
```

Releases land with a **merge commit**, not a rebase. Each release is then an explicit, immutable
point on `main`, `devs` is always its ancestor, and no post-release sync is needed.
`git log --first-parent main` shows the release line.

`main` is protected: no direct pushes, not even for the owner, linear history, no force-push.

### Rules

- **Never push to `main`.** Anything that has to land there goes through a PR from `devs`.
- **`devs` is always an ancestor of `main`**: releases land with a merge commit, so there is no
  divergence and no post-release sync. Never rewrite `devs`.
- **Run `make check` before pushing to `devs`.** CI runs on the push but does not block; a red
  `devs` is the first thing to fix.
- **Merge commits and bot commits are not signed.** The DCO check ignores them (`--no-merges`,
  `[bot]`).
- **Tags are cut from `main`.**

## Changelog fragments

Every user-visible change leaves its entry when it is made, in `.changes/unreleased/`:

```
.changes/unreleased/added-<something>.md      new capability
.changes/unreleased/changed-<something>.md    behaviour that changed
.changes/unreleased/fixed-<something>.md      bug fixed
.changes/unreleased/security-<something>.md   security issue
```

The content is the entry itself, in English, one or two lines. Internal changes — refactors with
no visible effect, `ci:`, docs, dependency bumps that change nothing — need no fragment.

At release time:

```sh
tools/changelog.sh prepare v0.1.0   # builds the section, removes the used fragments
```

CI enforces it: a pull request to `devs` reports pending fragments; a pull request to `main`
**fails** if `CHANGELOG.md` did not change against `main`.

## Preparing a release

The mechanical part is the repository's job; the editorial part is an agent's task.

```sh
tools/release-prep.sh v0.2.0    # dossier: fragments, commits, files, docs to review
```

With that dossier the agent does three things, in order:

1. **Write the changelog** from the user's point of view: what the program can do now, or what
   stopped breaking. No internal jargon, no commit hashes, nothing that the fragments and the
   diff do not support.
2. **Update the documentation** that actually changed — scope, process, dependencies. If nothing
   changed, say so and leave it.
3. **Update the README** only if what the program does, how it is installed or how it is
   configured changed. The README describes the real state: a feature that does not exist yet is
   not announced.

Then `tools/changelog.sh prepare v0.2.0`, `make check`, signed commit.

## Reporting bugs

Use the issue template. Include:

- the output of `./bin/ntfy-desktop` (it prints user, locale, desktop and dialog tool),
- your distribution and desktop version,
- what you expected and what happened.

Never paste tokens, passwords or full message contents from a private topic.
