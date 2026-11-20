# Decision requests

A message can ask something instead of only telling. When it carries **actions**, the client opens a
dialog and runs the one the person chooses. This page is the contract: what to publish, what the
client does with it, and what it deliberately does not do.

## Publishing a decision request

Actions travel in the ntfy `Actions` header. Each one is a kind, a label and a target, separated by
`;`:

```
Actions: http, Aprobar, https://example.org/approve, method=POST; http, Rechazar, https://example.org/reject, method=POST
```

```sh
curl -H "Title: ¿Restauro el backup?" \
     -H "Priority: 5" \
     -H 'Actions: http, Aprobar, https://example.org/approve, method=POST; http, Rechazar, https://example.org/reject, method=POST' \
     -d "El backup de anoche falló. ¿Lo reintento ahora?" \
     https://ntfy.example.org/casa
```

The dialog shows the title in bold, the body under it, and one button per action labelled with the
label you wrote — translated by your catalog when the label is one of the client's own, and shown as
it arrives otherwise.

## Kinds the client runs

| Kind | What the client does |
|---|---|
| `view` | opens the URL in the session's browser |
| `http` | calls the URL with the method, headers and body the action carries |

`broadcast` and `copy` are ntfy's other two kinds; they act on other devices or on the clipboard, and
this client does not run them. A message whose actions are all of those kinds asks nothing, and the
log says so at debug level instead of opening a dialog with one button that does nothing.

For `http`, the method defaults to `POST` (ntfy's own default) and may be `GET`, `PUT`, `PATCH` or
`DELETE`. `headers` and `body` travel as the action declares them.

## One at a time

Requests are answered **one at a time**. A second dialog on top of the first hides it, and the person
ends up answering the wrong thing, so the client keeps a line:

- one dialog is open at a time; the others wait, with their place in the title: *Decision (1 of 3)*;
- closing a dialog without answering does **not** throw the request away: it goes to the end of the
  line, and is not reopened immediately, so a person who is busy is not trapped in a loop;
- a request nobody answers within **10 minutes** is answered as *expired* and logged;
- when several messages arrive together, the first dialog waits a quarter of a second so the batch
  is counted whole in its title.

## How the answer gets back

The answer **is** the action: choosing `Aprobar` calls `https://example.org/approve`. There is no
reply message and no shared state — the endpoint you wrote receives the call, with the method and
body you asked for. That keeps the client out of your protocol: whatever can receive an HTTP request
can be the other half of a decision, from a cron job to Home Assistant.

The log records what happened to every request, so the journal is the place to check:

```sh
journalctl --user -u ntfy-desktop | grep "action run"
# ... msg="action run" message=o79Eqt1OAPsq action=Aprobar
```

A call that fails says why, and the message keeps its id, so it can be retried by hand.

## Dialog buttons, per desktop

| Actions | KDE (`kdialog`) | GNOME (`zenity`) | Others |
|---|---|---|---|
| 1 | one button | one button | one button |
| 2 | three buttons: the two actions and *Dismiss* | same | same |
| 3+ | a list with the actions and *Dismiss* | same | same |

The labels come from the catalog, not from the desktop, so the buttons say what the message asks in
the person's language. The dialog also wears the urgency of the message as its icon: a warning at
priority 4 and above, information at 2 and below, a question in between.

## What it will not do

- It does not open a URL that is not `http` or `https` — a `file://` or a custom scheme is refused,
  and the refusal is logged.
- It never hands a URL to a shell: the opener receives it as a single argument.
- It does not run `broadcast` or `copy`.
- It does not answer a request it cannot show: with no dialog tool installed, decisions are logged
  instead of being silently accepted.

## A warning about who can publish

Anyone who can publish to the topic can make your desktop open a link or call a URL. Treat the topic
as the trust boundary:

- use a protected topic with `user` and `password` (see [configuration](configuration.md));
- point the calls at something that checks them again — a token in a header, a signature in the
  body, an endpoint that expects the message id;
- do not put an unauthenticated "approve" endpoint on a network you do not control.
