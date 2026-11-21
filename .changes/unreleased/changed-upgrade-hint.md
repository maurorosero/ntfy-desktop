The package now says what it cannot do: after installing or upgrading, the client running in the
graphical session keeps the previous binary, because a package cannot restart another person's user
service. `packaging/scripts/postinstall.sh` prints the two commands that fix it.

Docs: README.md, docs/usage.md.
