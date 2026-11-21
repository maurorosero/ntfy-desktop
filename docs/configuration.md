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
