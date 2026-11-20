`install.sh` now installs and enables the user unit along with the binary, the menu entry and the
icons, and writes it with the real path of the installed binary instead of assuming `/usr/bin`. The
unit goes to `<prefix>/share/systemd/user/`, which systemd --user already searches, so a prefix
other than `~/.local` works too.

`--bin-only` installs the binary and nothing else, for whoever wants to wire it by hand.

The unit also stopped blocking the history: it runs with `ProtectHome=read-only` and listed only
the configuration and data directories, so `~/.cache/ntfy-desktop` was out of reach.

Docs: README.md.
