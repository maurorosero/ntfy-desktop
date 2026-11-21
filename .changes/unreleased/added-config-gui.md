The configuration can also be done from the desktop: `ntfy-desktop config --gui` asks the same five
steps — server, credentials, topics, verification and summary — with one native dialog per step,
using kdialog on Plasma and zenity elsewhere. Cancelling a dialog writes nothing, a server that does
not answer can still be saved after the summary warns about it, and the password has a dialog of its
own: it never appears in the summary, in the log or in the argument list. Without either dialog tool
the wizard says so and continues in the terminal.
