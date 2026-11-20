// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"os/exec"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// Zenity asks with zenity --list --radiolist on GNOME.
//
// Unlike kdialog, zenity can rename its two buttons (--ok-label, --cancel-label),
// so both carry our catalog's text. The tag goes back on stdout: it lives in a
// hidden column, and the visible one is the label the person reads.
type Zenity struct {
	path string
	tr   Translator
}

// NewZenity builds the adapter for the zenity binary at path. GNOME is the
// desktop the project has not been able to test yet: the command line follows
// the manual, and the first run on a GNOME session is still pending.
func NewZenity(path string, tr Translator) *Zenity {
	return &Zenity{path: path, tr: tr}
}

// Ask implements Decider. zenity exits 1 when its Cancel button was used, and 5
// when its own timeout fired, which does not happen here because --timeout is
// never passed: the deadline is the caller's.
func (z *Zenity) Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error) {
	out, err := exec.CommandContext(ctx, z.path, z.args(ctx, m, actions)...).Output()
	if err != nil {
		return "", askError(ctx, err)
	}
	return resolve(strings.TrimSpace(string(out)), actions)
}

// args is the command line, kept apart from Ask so the test can compare it
// without starting a process.
//
// A radio list numbers its columns as they are declared: the first one holds
// the selection, the second the tag (hidden, printed), the third the label.
// Rows are triples in that same order.
func (z *Zenity) args(ctx context.Context, m ntfy.Message, actions []ntfy.Action) []string {
	cs := choices(z.tr, actions)
	args := []string{
		"--list", "--radiolist",
		"--title", title(ctx, z.tr),
		"--text", promptText(z.tr, m, "\n"),
		"--column", "", "--column", "", "--column", z.tr.T("Action"),
		"--hide-header", "--hide-column=2", "--print-column=2",
		"--ok-label", z.tr.T("Select"),
		"--cancel-label", z.tr.T("Dismiss"),
	}
	for i, c := range cs {
		selected := "FALSE"
		if i == 0 {
			selected = "TRUE"
		}
		args = append(args, selected, c.tag, c.label)
	}
	return args
}

// Zenity is a Decider; the check fails at compile time if the method drifts.
var _ Decider = (*Zenity)(nil)
