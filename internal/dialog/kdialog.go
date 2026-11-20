// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"os/exec"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// KDialog asks with kdialog --menu on Plasma.
//
// The menu is what gives the entries their own labels: kdialog's own buttons
// (OK, Cancel) are localized by KDE, in KDE's language, and there is no flag to
// rename them. So the list carries the actions plus a "Dismiss" entry from our
// catalog, and the dialog text is the message itself.
type KDialog struct {
	path string
	tr   Translator
}

// NewKDialog builds the adapter for the kdialog binary at path.
func NewKDialog(path string, tr Translator) *KDialog {
	return &KDialog{path: path, tr: tr}
}

// Ask implements Decider. kdialog prints the tag of the chosen row on stdout
// and exits 1 when its Cancel button was used.
func (k *KDialog) Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error) {
	out, err := exec.CommandContext(ctx, k.path, k.args(ctx, m, actions)...).Output()
	if err != nil {
		return "", askError(ctx, err)
	}
	return resolve(strings.TrimSpace(string(out)), actions)
}

// args is the command line. It is a method of its own so the test can compare
// it without starting a process.
//
// kdialog parses --menu text tag item [tag item] ...: the text goes with the
// flag, the pairs are positional. --default preselects the first row, which
// also keeps kdialog from reporting an empty selection.
func (k *KDialog) args(ctx context.Context, m ntfy.Message, actions []ntfy.Action) []string {
	cs := choices(k.tr, actions)
	args := []string{
		"--title", title(ctx, k.tr),
		"--menu", promptText(k.tr, m, "<br/>"),
		"--default", cs[0].tag,
	}
	for _, c := range cs {
		args = append(args, c.tag, c.label)
	}
	return args
}

// KDialog is a Decider; the check fails at compile time if the method drifts.
var _ Decider = (*KDialog)(nil)
