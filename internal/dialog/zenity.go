// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// Zenity asks with zenity on GNOME.
//
// zenity renames its buttons with --ok-label, --cancel-label and, since 4.0,
// --extra-button. So one action gets one button and two actions get three, all
// with labels from our catalog; three actions or more fall back to the radio
// list, where the tag travels in a hidden column and comes back on stdout.
type Zenity struct {
	path string
	tr   Translator
}

// NewZenity builds the adapter for the zenity binary at path.
func NewZenity(path string, tr Translator) *Zenity {
	return &Zenity{path: path, tr: tr}
}

// Ask implements Decider. zenity exits 0 for its accept button, 1 for cancel
// and, with ZENITY_EXTRA set, 2 for the extra button; the radio list prints the
// tag of the chosen row. Exit code 5 is zenity's own timeout, which never fires
// here: the deadline is the caller's.
func (z *Zenity) Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error) {
	cmd := exec.CommandContext(ctx, z.path, z.args(ctx, m, actions)...)
	cmd.Env = z.env()
	out, err := cmd.Output()

	switch len(actions) {
	case 1:
		if err != nil {
			return "", askError(ctx, err)
		}
		return action.Label(actions[0]), nil
	case 2:
		if err == nil {
			return action.Label(actions[0]), nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			switch exitErr.ExitCode() {
			case 1:
				return "", ErrCancelled
			case 2:
				return action.Label(actions[1]), nil
			}
		}
		return "", askError(ctx, err)
	default:
		if err != nil {
			return "", askError(ctx, err)
		}
		return resolve(strings.TrimSpace(string(out)), actions)
	}
}

// extraExitCode is the code the third button of --question is made to return.
//
// zenity exits 1 for its extra button by default, the same code as its cancel
// button, so the two are indistinguishable and a rejection would be read as "no
// answer". Zenity reads ZENITY_EXTRA to change it; the default is kept only for
// compatibility with 3.x.
const extraExitCode = "2"

// env is the environment the dialog runs with: the caller's, minus any
// ZENITY_EXTRA the session may carry, plus ours, so the code cannot be taken
// by surprise.
func (z *Zenity) env() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "ZENITY_EXTRA=") || strings.HasPrefix(kv, "DIALOG_EXTRA=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "ZENITY_EXTRA="+extraExitCode)
}

// args is the command line, kept apart from Ask so the test can compare it
// without starting a process.
//
// In the radio list the columns are numbered as they are declared: the first
// one holds the selection, the second the tag (hidden, printed), the third the
// label. Rows are triples in that same order.
func (z *Zenity) args(ctx context.Context, m ntfy.Message, actions []ntfy.Action) []string {
	base := []string{"--title", title(ctx, z.tr)}
	text := promptText(z.tr, m, "\n")

	switch len(actions) {
	case 1:
		return append(base, "--info", "--text", text, "--ok-label", action.Label(actions[0]))
	case 2:
		return append(base,
			"--question", "--text", text,
			"--ok-label", action.Label(actions[0]),
			"--extra-button", action.Label(actions[1]),
			"--cancel-label", z.tr.T("Dismiss"),
		)
	}

	// The height grows with the rows: a fixed one clips the last of them behind
	// the buttons.
	cs := choices(z.tr, actions, true)
	args := append(base,
		"--list", "--radiolist",
		"--text", text,
		"--column", "", "--column", "", "--column", z.tr.T("Action"),
		"--hide-header", "--hide-column=2", "--print-column=2",
		"--width", "520", "--height", strconv.Itoa(150+40*len(cs)),
		"--ok-label", z.tr.T("Select"),
		"--cancel-label", z.tr.T("Dismiss"),
	)
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
