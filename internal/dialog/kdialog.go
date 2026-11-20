// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// KDialog asks with kdialog on Plasma.
//
// One action gets one button and two actions get three: kdialog renames the
// buttons of --msgbox and --yesnocancel with --ok-label, --yes-label,
// --no-label and --cancel-label, so the labels come from our catalog, in the
// person's language, and answering is a single click. Three actions or more get
// the menu, because kdialog has no more buttons than that.
type KDialog struct {
	path string
	tr   Translator
}

// NewKDialog builds the adapter for the kdialog binary at path.
func NewKDialog(path string, tr Translator) *KDialog {
	return &KDialog{path: path, tr: tr}
}

// Ask implements Decider. The answer comes back the way the dialog asked for
// it: --msgbox exits 0 on its only button, --yesnocancel exits 0 for yes, 1 for
// no and 2 for cancel, and --menu prints the tag of the chosen row and exits 1
// on cancel.
func (k *KDialog) Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error) {
	out, err := exec.CommandContext(ctx, k.path, k.args(ctx, m, actions)...).Output()

	switch len(actions) {
	case 1:
		if err != nil {
			return "", askError(ctx, err)
		}
		return action.Label(actions[0]), nil
	case 2:
		return answerOfButtons(ctx, err, actions)
	default:
		if err != nil {
			return "", askError(ctx, err)
		}
		return resolve(strings.TrimSpace(string(out)), actions)
	}
}

// answerOfButtons reads a --yesnocancel answer: 0 is the first action, 1 the
// second, and 2 means the dialog was closed without answering.
func answerOfButtons(ctx context.Context, err error, actions []ntfy.Action) (string, error) {
	if err == nil {
		return action.Label(actions[0]), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return action.Label(actions[1]), nil
		case 2:
			return "", ErrCancelled
		}
	}
	return "", askError(ctx, err)
}

// args is the command line. It is a method of its own so the test can compare
// it without starting a process.
func (k *KDialog) args(ctx context.Context, m ntfy.Message, actions []ntfy.Action) []string {
	base := []string{"--title", title(ctx, k.tr), "--icon", iconFor(m.Priority)}
	text := promptText(k.tr, m, "<br/>")

	switch len(actions) {
	case 1:
		return append(base, "--msgbox", text, "--ok-label", action.Label(actions[0]))
	case 2:
		return append(base,
			"--yesnocancel", text,
			"--yes-label", action.Label(actions[0]),
			"--no-label", action.Label(actions[1]),
			"--cancel-label", k.tr.T("Dismiss"),
		)
	}

	// kdialog parses --menu text tag item [tag item] ...: the text goes with the
	// flag, the pairs are positional. --default preselects the first row, which
	// also keeps kdialog from reporting an empty selection. The list carries the
	// way out as a row, because --menu has no cancel button of its own.
	cs := choices(k.tr, actions, true)
	args := append(base, "--menu", text, "--default", cs[0].tag)
	for _, c := range cs {
		args = append(args, c.tag, c.label)
	}
	return args
}

// KDialog is a Decider; the check fails at compile time if the method drifts.
var _ Decider = (*KDialog)(nil)
