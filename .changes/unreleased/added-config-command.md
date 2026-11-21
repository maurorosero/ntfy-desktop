The configuration can be written, checked and changed from the terminal: `ntfy-desktop config` runs
a wizard that writes only what you confirm, `--check` verifies file, server, credentials and every
topic and exits with 0, 2, 3, 4 or 5, and `--set` or `config topic --add/--remove` change one thing
at a time. The password never travels as an argument.
