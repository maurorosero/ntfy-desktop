# Security policy

## Reporting a vulnerability

**Do not open a public issue.** Report privately through GitHub:

1. Go to the repository's **Security** tab.
2. Choose **Report a vulnerability**.
3. Describe the problem, the version affected and, if you can, a minimal way to reproduce it.

If GitHub private reporting is unavailable to you, open a public issue that says only *"I need
to report a security issue privately"* and nothing else; a maintainer will provide a channel.

## What you can expect

- Acknowledgement within **7 days**.
- An assessment with a severity and a plan, or a clear explanation of why it is not a
  vulnerability, within **30 days**.
- Credit in the changelog when a fix ships, unless you prefer to stay anonymous.

This is a small project with a single maintainer; those are best-effort commitments, not a
contractual SLA.

## Supported versions

Only the latest release receives fixes. There are no long-term support branches.

## What is in scope

- The client binary and its build.
- The way credentials are read, stored and passed to the ntfy server.
- The way the client launches external tools (`kdialog`, `zenity`) — argument handling,
  quoting, environment.
- Anything that could make the client send data somewhere the user did not configure.

## What is out of scope

- The ntfy server itself: report that to the [ntfy project](https://github.com/binwiederhier/ntfy).
- Notification daemons, `kdialog`, `zenity` or systemd: report those upstream.
- Social engineering.
- Missing features (open a normal issue instead).

## Design decisions that matter for security

- Credentials are never compiled into the binary and never logged. They are read from the
  user's configuration file or the environment.
- The client runs as a systemd **user** unit with `NoNewPrivileges`, `ProtectSystem=strict`,
  `ProtectHome=read-only` and write access limited to `~/.config/ntfy-desktop` and
  `~/.local/share/ntfy-desktop`.
- No telemetry, no analytics, no remote logging.
