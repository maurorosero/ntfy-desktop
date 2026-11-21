# Configuration

One file, written by you. A package never puts anything in your home:

```
$XDG_CONFIG_HOME/ntfy-desktop/config.toml        # normally ~/.config/ntfy-desktop/config.toml
```

```toml
server    = "https://ntfy.example.org"   # required: http or https, no query, no fragment
topic     = "alerts"                     # required: 1 to 64 of [A-Za-z0-9_-] each, comma separated
user      = "andrea"                     # optional, together with password
password  = "…"                          # optional, together with user
log-level = "info"                       # optional: debug, info, warn or error
```

## Keys

### `server` — required

The base URL of the ntfy server. `http` or `https`, with no query and no fragment, because the
client appends the topic and its own parameters. A trailing slash is accepted.

```toml
server = "https://ntfy.sh"
server = "http://10.0.0.5:2586"
```

### `topic` — required

The topic, or the topics, to subscribe to: each one is 1 to 64 characters from `[A-Za-z0-9_-]`,
and several are written separated by commas, in the order they are read. Every name is validated
because each one becomes the name of a file of the history.

```toml
topic = "casa"
topic = "casa, pruebas-multi"
topic = "casa, pruebas-multi, avisos"
```

The list is one subscription: the client opens a single stream to `/casa,pruebas-multi/json` and
the server delivers the messages of all of them, each one carrying its own `topic`. Notifications
and the history are per topic, as they were with one.

An empty name is an error that names the segment — `topic = "casa,,x"` says `""`, and
`topic = "casa,pruebas multi"` says `"pruebas multi"` — rather than a subscription that quietly
lost a topic. `publish` needs one destination: with several configured, pass it as an argument or
with `--topic`.

### `user` and `password` — optional, as a pair

Set both to use HTTP Basic on a protected topic. Leave both out to subscribe anonymously.

```toml
user     = "andrea"
password = "…"
```

Half a pair is a configuration error, not an anonymous subscription: the loader says which key is
missing instead of quietly connecting without credentials. That asymmetry is deliberate — a missing
password would otherwise turn into a silent downgrade.

The file holds a password in the clear, so give it the permissions to match:

```sh
chmod 600 ~/.config/ntfy-desktop/config.toml
```

### `log-level` — optional

What the daemon logs at: `debug`, `info`, `warn` or `error`. `info` by default. `--log-level` on the
command line wins over the file, which is what you want for a one-off debug run without editing it.

## The format

The loader is small on purpose and accepts exactly this:

- one `key = "value"` per line;
- `#` starts a comment; blank lines are ignored;
- `\"`, `\\`, `\n`, `\t` and `\r` inside a value;
- **any other key is an error**, so a typo is never silently ignored.

It is not a general TOML parser: there are no tables, arrays or multi-line strings. The file has five
scalars and no reason to grow a parser.

## From the terminal

`ntfy-desktop config` reads, verifies and edits this file, so it never has to be written by hand:

```sh
ntfy-desktop config                       # the wizard: server, credentials, topics, summary
ntfy-desktop config --gui                 # the same wizard, one desktop dialog per step
ntfy-desktop config --show                # the effective configuration and the path of the file
ntfy-desktop config --check               # verify the file, the server, the credentials, every topic
ntfy-desktop config --set server=https://ntfy.sh
ntfy-desktop config topic --add avisos
ntfy-desktop config topic --remove avisos
```

The wizard asks one question at a time and writes only after showing the file it is about to write —
with the password as `••••••••` — and asking for a confirmation. Cancelling at any question (an
empty answer is kept as the default; `Ctrl-D` stops) leaves the file exactly as it was. A change made
with `--set`, `--add` or `--remove` is validated with the same loader and written atomically; an
invalid file is never the base of an edit, and a change that would leave the file invalid is
refused.

`--gui` runs the same interview with the dialogs of the desktop instead of the terminal: server,
credentials, topics, the verification of every control and the summary, one dialog per step. On
Plasma the dialogs are `kdialog`; `zenity` draws the same steps elsewhere. Without either of them,
or without a session to draw them in, `--gui` says so in one line and asks in the terminal — it does
not fail. A cancelled dialog writes nothing, and the password goes from the field to the program
without ever passing through the argument list.

The password is never an argument, where `ps` and the shell history would see it: the wizard asks for
it without echoing it (`stty`), and `--password-stdin` reads it from the standard input for a script.
`--set password=…` is refused on purpose.

`--check` writes nothing and exits with a code that names what failed:

| Code | What it means |
|---|---|
| 0 | everything verified |
| 1 | nothing was written: the wizard was cancelled |
| 2 | the file cannot be read, or it is not valid |
| 3 | the server does not answer, or it is not an ntfy server |
| 4 | the server rejected the credentials (401) |
| 5 | the credentials have no permission for a topic (403) |

The wizard and the commands that edit one key use the same numbers for the same failures, with one
deliberate difference: a server that does not answer does not stop them. They save the file with a
warning, because a configuration can be written before the server is reachable (decision of
`plan/configuracion.md` §8). A refusal — not ntfy, 401, 403 — blocks and leaves the file untouched.

`--check` makes one request to `/v1/health`, one to `/v1/account` and opens one stream per topic,
cutting it on its first event: it reads no message and publishes nothing. A server without
`/v1/account` — an older instance — is reported as unverified and does not turn the answer into a
failure.

`--show` prints the password as `••••••••`, never the value: the command exists to look at the file
without opening it.

## Validation, and what it refuses

| What | Why it is refused |
|---|---|
| unknown key | a typo would silently change nothing |
| missing `server` or `topic` | there is nothing to subscribe to |
| `server` that is not `http`/`https` | the client would not know how to talk to it |
| `server` with a query or fragment | the client builds the path itself |
| half a `user`/`password` pair | a silent downgrade to anonymous |
| `log-level` outside the four | the logger would have to guess |

Every error names the key and the reason, so the journal line is enough to fix it:

```
level=ERROR msg="the configuration is not usable" path=/home/mrosero/.config/ntfy-desktop/config.toml
  key=password reason="requires a password"
```

## Where the code lives

`internal/config` — loading, validation, the typed errors and the atomic write. Its tests are
table-driven and cover each refusal above, plus the escapes and the comments.

`internal/setup` — the operations that read the file, change one key or the list of topics and write
the result, without a user interface or the network behind them.

`cmd/ntfy-desktop/config.go` — the `config` subcommand: the wizard, `--show`, `--check` and the
commands that change one key, with the exit codes above. Its questions go through a `Prompter`, so
the wizard is tested with fixed answers instead of a terminal; `internal/ntfy` provides the three
checks it runs.

`internal/dialog` — the `Prompter` implementations: `KDialogPrompt` and `ZenityPrompt` run the tool
as a child process, one dialog per step, and translate its exit codes (`0` yes, `1` no or cancel,
`2` cancel; zenity needs `ZENITY_EXTRA` to tell a rejection from a cancel) into an answer, a
cancellation or an error. They draw no widget of their own: no Qt and no GTK are linked.
