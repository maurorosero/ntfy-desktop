// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// The three colours of the verification screen: ok is green, unverified amber
// and failed red, on both renderers — Qt's rich text in kdialog and Pango's in
// zenity understand the same values.
const (
	colorOK      = "#2e7d32"
	colorUnverif = "#b26a00"
	colorFailed  = "#c62828"
)

// KDialogPrompt asks the configuration with kdialog on Plasma.
//
// One step of the wizard is one dialog. The exit codes are kdialog's own and
// they are read here, never above: 0 is yes or accept, 1 is no or a closed
// field, 2 is cancel. A caller of the wizard receives a value, a false or
// ErrPromptCancelled, never a number.
type KDialogPrompt struct {
	path string
	tr   Translator
	h    heading
}

// NewKDialogPrompt builds the adapter for the kdialog binary at path.
func NewKDialogPrompt(path string, tr Translator) *KDialogPrompt {
	return &KDialogPrompt{path: path, tr: tr, h: heading{tr: tr}}
}

// Step implements Prompter.
func (k *KDialogPrompt) Step(n, total int, name string) { k.h.set(n, total, name) }

// Line implements Prompter with --inputbox, which keeps current as the value
// the person starts from.
//
// kdialog 25.12 ignores --ok-label and --cancel-label here and in Secret and
// Topics: it draws the text of its own buttons, translated by Qt. Measured with
// `--ok-label "ZZZ" --cancel-label "YYY"`, which came out as the toolkit's own
// pair in Spanish ("Aceptar"/"Cancelar"), not ZZZ/YYY; --button, the usual way
// to name buttons, answers "unknown option". The labels are kept anyway because
// they cost nothing and a later kdialog may read them.
// --yesno is the one class that honours them, through --yes-label/--no-label.
func (k *KDialogPrompt) Line(ctx context.Context, question, current string) (string, error) {
	args := []string{"--title", k.h.title(), "--ok-label", k.tr.T("Continue"), "--cancel-label", k.tr.T("Cancel"),
		"--inputbox", esc(question)}
	if current != "" {
		args = append(args, current)
	}
	out, err := runTool(ctx, k.path, args, nil)
	if err != nil {
		return "", k.cancelled(ctx, err)
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// Secret implements Prompter with --password: the value goes from the field to
// this process, never through the argument list.
func (k *KDialogPrompt) Secret(ctx context.Context, question string) (string, error) {
	args := []string{"--title", k.h.title(), "--ok-label", k.tr.T("Continue"), "--cancel-label", k.tr.T("Cancel"),
		"--password", esc(question)}
	out, err := runTool(ctx, k.path, args, nil)
	if err != nil {
		return "", k.cancelled(ctx, err)
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// YesNo implements Prompter with --yesno: 0 is yes, 1 is no and 2 is the
// dialog closed without answering. --default puts the answer the person most
// likely wants on the button Enter presses.
func (k *KDialogPrompt) YesNo(ctx context.Context, question string, def bool) (bool, error) {
	press := "no"
	if def {
		press = "yes"
	}
	args := []string{"--title", k.h.title(), "--default", press, "--yesno", esc(question)}
	if _, err := runTool(ctx, k.path, args, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if code, ok := exitStatus(err); ok {
			switch code {
			case 1:
				return false, nil
			case 2:
				return false, ErrPromptCancelled
			}
		}
		return false, err
	}
	return true, nil
}

// Topics implements Prompter: the configured names come back as a --checklist
// and the field below it adds one at a time. An added name is checked on the
// spot, so a typo is refused where it is typed.
func (k *KDialogPrompt) Topics(ctx context.Context, current []string) ([]string, error) {
	chosen := current
	if len(current) > 0 {
		args := []string{"--title", k.h.title(), "--separate-output",
			"--ok-label", k.tr.T("Continue"), "--cancel-label", k.tr.T("Cancel"),
			"--checklist", k.tr.T("topics to subscribe to")}
		for _, name := range current {
			args = append(args, name, name, "on")
		}
		out, err := runTool(ctx, k.path, args, nil)
		if err != nil {
			return nil, k.cancelled(ctx, err)
		}
		chosen = splitLines(out)
	}

	for {
		name, err := k.Line(ctx, k.tr.T("add a topic (leave it empty to finish)"), "")
		if err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return chosen, nil
		}
		if err := checkNewTopic(name, chosen, k.tr); err != nil {
			if err := k.Warn(ctx, err.Error()); err != nil {
				return nil, err
			}
			continue
		}
		chosen = append(chosen, name)
	}
}

// Report implements Prompter with a --msgbox that carries one line per check
// and the state of each one. It is read-only: the wizard decides what a state
// blocks, not this window.
func (k *KDialogPrompt) Report(ctx context.Context, report ntfy.Report, server string) error {
	args := []string{"--title", k.h.title(), "--icon", reportIcon(report),
		"--ok-label", k.tr.T("Continue"), "--msgbox", richReport(report, server, k.tr, "kdialog")}
	if _, err := runTool(ctx, k.path, args, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// Closing a read-only window with Escape is acknowledging it.
		if code, ok := exitStatus(err); ok && (code == 1 || code == 2) {
			return nil
		}
		return err
	}
	return nil
}

// Confirm implements Prompter with --textbox for the body and --yesno for the
// question: the summary is read first, and only then the file is written.
func (k *KDialogPrompt) Confirm(ctx context.Context, question, body string) (bool, error) {
	if err := k.textBox(ctx, body); err != nil {
		return false, err
	}
	args := []string{"--title", k.h.title(),
		"--yes-label", k.tr.T("Write"), "--no-label", k.tr.T("Cancel"),
		"--yesno", esc(question)}
	if _, err := runTool(ctx, k.path, args, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if code, ok := exitStatus(err); ok {
			switch code {
			case 1:
				return false, nil
			case 2:
				return false, ErrPromptCancelled
			}
		}
		return false, err
	}
	return true, nil
}

// Note implements Prompter. A desktop front end already showed everything the
// person had to answer, and a window after a cancel would be noise.
func (k *KDialogPrompt) Note(string) {}

// Warn implements Prompter with --error: the dialog stays until it is read.
func (k *KDialogPrompt) Warn(ctx context.Context, text string) error {
	args := []string{"--title", k.h.title(), "--error", esc(text)}
	if _, err := runTool(ctx, k.path, args, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if code, ok := exitStatus(err); ok && (code == 1 || code == 2) {
			return nil
		}
		return err
	}
	return nil
}

// Done implements Prompter with --msgbox: the closing line of the interview.
func (k *KDialogPrompt) Done(ctx context.Context, text string) error {
	args := []string{"--title", k.h.title(), "--icon", "dialog-information", "--msgbox", esc(text)}
	if _, err := runTool(ctx, k.path, args, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if code, ok := exitStatus(err); ok && (code == 1 || code == 2) {
			return nil
		}
		return err
	}
	return nil
}

// textBox shows body in a temporary file: --textbox reads a path, and the body
// is already masked, so nothing secret is left in the file.
func (k *KDialogPrompt) textBox(ctx context.Context, body string) error {
	return withTempText(body, func(path string) error {
		args := []string{"--title", k.h.title(), "--textbox", path}
		if _, err := runTool(ctx, k.path, args, nil); err != nil {
			return k.cancelled(ctx, err)
		}
		return nil
	})
}

// cancelled reads the codes that mean the person stopped the interview in the
// dialogs that take a value: 1 is a closed field and 2 is a cancel.
func (k *KDialogPrompt) cancelled(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if code, ok := exitStatus(err); ok && (code == 1 || code == 2) {
		return ErrPromptCancelled
	}
	return err
}

// KDialogPrompt is a Prompter; the check fails at compile time if it drifts.
var _ Prompter = (*KDialogPrompt)(nil)

// checkNewTopic applies the rule of the configuration to a name typed in the
// dialog, so the answer is refused where it is typed.
func checkNewTopic(name string, chosen []string, tr Translator) error {
	if err := config.CheckTopic(name); err != nil {
		return fmt.Errorf("%q: %s", name, tr.T("must be 1 to 64 characters of letters, digits, dashes or underscores"))
	}
	for _, topic := range chosen {
		if topic == name {
			return errors.New(tr.T("%q is already in the topic list", name))
		}
	}
	return nil
}

// reportIcon is the icon of the verification window, taken from the worst
// state of the report.
func reportIcon(report ntfy.Report) string {
	switch report.State() {
	case ntfy.StateUnverifiable:
		return "dialog-warning"
	case ntfy.StateFailed:
		return "dialog-error"
	}
	return "dialog-information"
}

// richReport composes the verification screen. kdialog reads Qt rich text and
// zenity Pango, so the two differ only in the tag that colours a state.
func richReport(report ntfy.Report, server string, tr Translator, renderer string) string {
	lines := ReportLines(report, server, tr)
	rows := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		row := fmt.Sprintf("%s: %s", esc(line.Label), colorState(line.State, tr, renderer))
		if line.Detail != "" {
			row += " — " + esc(line.Detail)
		}
		rows = append(rows, row)
	}
	if footer := ReportFooter(report, tr); footer != "" {
		rows = append(rows, "", colorText(footer, colorUnverif, renderer))
	}
	return strings.Join(rows, "<br/>")
}

// colorState renders a state word in its colour.
func colorState(state ntfy.State, tr Translator, renderer string) string {
	switch state {
	case ntfy.StateOK:
		return colorText(tr.T("ok"), colorOK, renderer)
	case ntfy.StateUnverifiable:
		return colorText(tr.T("unverified"), colorUnverif, renderer)
	case ntfy.StateFailed:
		return colorText(tr.T("failed"), colorFailed, renderer)
	}
	return esc(tr.T("unknown"))
}

// colorText wraps text in the tag of the renderer, escaping the text first:
// a URL with an ampersand must not break the tag.
func colorText(text, color, renderer string) string {
	if renderer == "zenity" {
		return fmt.Sprintf("<span foreground=%q>%s</span>", color, esc(text))
	}
	return fmt.Sprintf("<font color=%q>%s</font>", color, esc(text))
}

// withTempText hands fn the path of a temporary file that holds text. The file
// is created with the same permissions the configuration gets, and it is
// removed when fn returns.
func withTempText(text string, fn func(path string) error) error {
	f, err := os.CreateTemp("", "ntfy-desktop-summary-*.txt")
	if err != nil {
		return fmt.Errorf("creating the temporary file for the summary: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)

	_, writeErr := f.WriteString(text)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("writing the temporary file for the summary: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing the temporary file for the summary: %w", closeErr)
	}
	return fn(name)
}
