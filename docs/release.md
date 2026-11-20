# Releasing

A release is a tag on `main` and the artifacts the pipeline builds from it. Nothing is compiled by
hand: the workflow does it from the tag, so the version inside the binary cannot drift from the
version in the tag.

## The order

```sh
# 1. the work is on devs, with the changelog already written for the version
tools/changelog.sh prepare v0.2.0        # builds the section and consumes the fragments
git commit -s -am "Changelog 0.2.0"
git push origin devs

# 2. the only pull request that main accepts
gh pr create --base main --head devs --title "0.2.0"
gh pr merge --merge                      # merge commit, not squash, not rebase

# 3. the tag goes on that merge commit, on main
git fetch origin main
git switch -c release/0.2.0 origin/main
git tag -a v0.2.0 -m "0.2.0"
git push origin v0.2.0
```

The push of the tag is what starts `.github/workflows/release.yml`.

## What the workflow does

1. **Guarda del tag** — before building anything:
   - the tagged commit must be an ancestor of `main`;
   - it must be a **merge commit** (two parents), because the tag goes on the merge of the release
     pull request, not on a commit pushed straight to `main`;
   - `CHANGELOG.md` must have a section for that version.
   If any of the three fails, the job stops and no binary is produced.
2. **Notas** — `tools/changelog.sh notes vX.Y.Z` writes the release body from the changelog section,
   so the release says what the changelog says.
3. **Entrega** — the test suite and the repository hooks run again on the tagged tree, and then
   `goreleaser` builds and publishes, for `amd64` and `arm64`:
   - `ntfy-desktop_X.Y.Z_linux_amd64.deb` and `_linux_arm64.deb`
   - `ntfy-desktop_X.Y.Z_linux_amd64.rpm` and `_linux_arm64.rpm`
   - `ntfy-desktop_X.Y.Z_linux_amd64.tar.gz` and `_linux_arm64.tar.gz`, for whoever does not use
     their distribution's package manager
   - `checksums.txt`

   The version inside each binary comes from the tag (`{{ .Version }}` in the `ldflags`), so
   `ntfy-desktop --version` on a released binary prints that version and the commit it was built
   from.

## What a package installs

| Path | What |
|---|---|
| `/usr/bin/ntfy-desktop` | the binary |
| `/usr/share/applications/ntfy-desktop.desktop` | the menu entry |
| `/usr/share/icons/hicolor/…/apps/ntfy-desktop.*` | the icon, in SVG and 64/128/256/512 |
| `/usr/lib/systemd/user/ntfy-desktop.service` | the user unit, with `ExecStart=/usr/bin/ntfy-desktop` |
| `/usr/share/doc/ntfy-desktop/` | `LICENSE`, `NOTICE`, `README.md` and the user docs |

`kdialog` and `zenity` are **recommended**, not required: without either, notifications still work and
decisions are logged instead of asked.

The package starts nothing on its own. Enabling the service is a per-user decision:

```sh
systemctl --user daemon-reload
systemctl --user enable --now ntfy-desktop.service
```

## Why the tag is not created by the pipeline

A published tag and a published release are not as easy to take back as they are to create. The step
that ships binaries stays explicit: a person merges and tags, and the pipeline does the rest the same
way every time.

## Checking a release

```sh
gh release view v0.2.0
gh release download v0.2.0 --pattern 'checksums.txt' --dir /tmp/rel
sha256sum -c /tmp/rel/checksums.txt      # from the directory with the packages

dpkg-deb -I /tmp/rel/ntfy-desktop_0.2.0_linux_amd64.deb       # metadata
dpkg-deb -c /tmp/rel/ntfy-desktop_0.2.0_linux_amd64.deb       # what it installs
# or
sudo dpkg -i /tmp/rel/ntfy-desktop_0.2.0_linux_amd64.deb
ntfy-desktop --version                   # 0.2.0 <commit>
```

## If the workflow fails

- **"el tag no está en main"** — the tag was pushed from a local branch that is not merged. Delete it
  (`git push origin :v0.2.0`), merge the pull request, tag the merge commit.
- **"no es un merge commit"** — the tag landed on a commit pushed straight to `main`. The release
  pull request has to be merged with a merge commit; the tag goes on that one.
- **"CHANGELOG.md no tiene una sección para X"** — the version was not prepared. Run
  `tools/changelog.sh prepare vX.Y.Z`, commit it, and open the pull request again.
- Anything after that is the build itself: read the `entrega` job log.
