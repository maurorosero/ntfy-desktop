zenity exits 1 for its extra button, the same code as its own cancel button, so a rejection on
GNOME was read as "no answer" and the request stayed pending. The client now sets `ZENITY_EXTRA`
when it starts the dialog, which zenity reads to give that button its own code.

Found while wiring the button dialog: it would have answered the wrong thing in silence.

Docs: README.md.
