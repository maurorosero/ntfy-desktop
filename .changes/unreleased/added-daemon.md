Run as a daemon: `--daemon` subscribes, shows every message as a notification and asks for the
decisions its actions carry, until it is told to stop. `SIGTERM` and `SIGINT` end it in well under a
second, killing any dialog it opened, and the log goes to stderr at a level set by `--log-level` or
the `log-level` key.
The user unit in `packaging/` starts it with the graphical session and restarts it if it aborts.

Docs: README.md, plan/alcance.md.
