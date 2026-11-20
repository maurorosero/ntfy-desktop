Publish a message to a topic with `publish`: the body is read from stdin, so it is never taken for a
flag, and `--title`, `--priority`, `--tags`, `--click` and `--markdown` set what it carries. It
authenticates like the daemon, with the configured pair or anonymously, prints the id the server
assigned and reports a refused publish (HTTP 403) with the reason to look at.

Docs: README.md, plan/alcance.md.
