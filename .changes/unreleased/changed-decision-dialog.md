A decision with two actions is now asked with real buttons instead of a list: kdialog's
`--yesnocancel` and zenity's `--question` both rename their buttons from the catalog, so
answering is one click instead of picking a row and then pressing OK. One action gets a single
button; three or more keep the list, which now carries the way out once and is sized to show
every row.

The dialog also wears the urgency of the message as its icon: warning at priority 4 and above,
information at 2 and below, a question otherwise.

Docs: README.md, plan/alcance.md.
