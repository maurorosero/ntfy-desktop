// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// ZenityPrompt asks the configuration with zenity, the fallback of a session
// that is not Plasma.
//
// zenity has a trap of its own: it exits 1 for its cancel button, for a closed
// window and, in 3.x, also for the extra button. The question of this adapter
// therefore puts "no" on the extra button and runs the dialog with
// ZENITY_EXTRA=2, so 1 is always a cancel and 2 is always a rejection. The
// translation happens here; the wizard never sees a number.
type ZenityPrompt struct {
	path string
	tr   Translator
	h    heading
}

// NewZenityPrompt builds the adapter for the zenity binary at path.
func NewZenityPrompt(path string, tr Translator) *ZenityPrompt {
	return &ZenityPrompt{path: path, tr: tr, h: heading{tr: tr}}
}

// Step implements Prompter.
func (z *ZenityPrompt) Step(n, total int, name string) { z.h.set(n, total, name) }

// Line implements Prompter with --entry.
func (z *ZenityPrompt) Line(ctx context.Context, question, current string) (string, error) {
	args := []string{"--title", z.h.title(), "--entry", "--text", esc(question)}
	if current != "" {
		args = append(args, "--entry-text", current)
	}
	out, err := z.run(ctx, args)
	if err != nil {
		return "", z.cancelled(ctx, err)
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// Secret implements Prompter with --password: the field is masked and the value
// arrives on standard output, never on the command line.
func (z *ZenityPrompt) Secret(ctx context.Context, question string) (string, error) {
	args := []string{"--title", z.h.title(), "--password", "--text", esc(question)}
	out, err := z.run(ctx, args)
	if err != nil {
		return "", z.cancelled(ctx, err)
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// YesNo implements Prompter with a question that has three buttons: the answer
// of def on the default one, the other one on the extra button (exit 2 with
// ZENITY_EXTRA) and cancel on the one that exits 1. zenity has no way to make
// the extra button the default, so the labels swap instead: Enter always
// answers what the caller proposed, and the button says which answer it is.
func (z *ZenityPrompt) YesNo(ctx context.Context, question string, def bool) (bool, error) {
	accepted, rejected := z.tr.T("Yes"), z.tr.T("No")
	if !def {
		accepted, rejected = rejected, accepted
	}
	args := []string{"--title", z.h.title(), "--question", "--text", esc(question),
		"--ok-label", accepted, "--extra-button", rejected, "--cancel-label", z.tr.T("Cancel")}
	if _, err := z.run(ctx, args); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if code, ok := exitStatus(err); ok {
			switch code {
			case 2:
				return !def, nil
			case 1:
				return false, ErrPromptCancelled
			}
		}
		return false, err
	}
	return def, nil
}

// Topics implements Prompter with a check list of the configured names and an
// entry for the new one. With --separate-output the names come one per line,
// which is what splitLines expects.
func (z *ZenityPrompt) Topics(ctx context.Context, current []string) ([]string, error) {
	chosen := current
	if len(current) > 0 {
		args := []string{"--title", z.h.title(), "--list", "--checklist",
			"--text", esc(z.tr.T("topics to subscribe to")),
			"--column", "", "--column", z.tr.T("Topic"),
			"--print-column=2", "--separate-output",
			"--ok-label", z.tr.T("Continue"), "--cancel-label", z.tr.T("Cancel")}
		for _, name := range current {
			args = append(args, "TRUE", name)
		}
		out, err := z.run(ctx, args)
		if err != nil {
			return nil, z.cancelled(ctx, err)
		}
		chosen = splitLines(out)
	}

	for {
		name, err := z.Line(ctx, z.tr.T("add a topic (leave it empty to finish)"), "")
		if err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return chosen, nil
		}
		if err := checkNewTopic(name, chosen, z.tr); err != nil {
			if err := z.Warn(ctx, err.Error()); err != nil {
				return nil, err
			}
			continue
		}
		chosen = append(chosen, name)
	}
}

// Report implements Prompter with an info, warning or error window, chosen by
// the worst state of the report.
func (z *ZenityPrompt) Report(ctx context.Context, report ntfy.Report, server string) error {
	args := []string{"--title", z.h.title(), reportKind(report), "--text", richReport(report, server, z.tr, "zenity")}
	if _, err := z.run(ctx, args); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// Closing a read-only window is acknowledging it.
		if code, ok := exitStatus(err); ok && code == 1 {
			return nil
		}
		return err
	}
	return nil
}

// Confirm implements Prompter with --text-info for the body followed by a
// question: the summary is read first, and only then the file is written.
func (z *ZenityPrompt) Confirm(ctx context.Context, question, body string) (bool, error) {
	if err := z.textInfo(ctx, body); err != nil {
		return false, err
	}
	args := []string{"--title", z.h.title(), "--question", "--text", esc(question),
		"--ok-label", z.tr.T("Write"), "--cancel-label", z.tr.T("Cancel")}
	if _, err := z.run(ctx, args); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if code, ok := exitStatus(err); ok && code == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Note implements Prompter. A desktop front end already showed everything the
// person had to answer, and a window after a cancel would be noise.
func (z *ZenityPrompt) Note(string) {}

// Warn implements Prompter with --error.
func (z *ZenityPrompt) Warn(ctx context.Context, text string) error {
	args := []string{"--title", z.h.title(), "--error", "--text", esc(text)}
	if _, err := z.run(ctx, args); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if code, ok := exitStatus(err); ok && code == 1 {
			return nil
		}
		return err
	}
	return nil
}

// Done implements Prompter with --info.
func (z *ZenityPrompt) Done(ctx context.Context, text string) error {
	args := []string{"--title", z.h.title(), "--info", "--text", esc(text)}
	if _, err := z.run(ctx, args); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if code, ok := exitStatus(err); ok && code == 1 {
			return nil
		}
		return err
	}
	return nil
}

// textInfo shows body in a temporary file, which is what --text-info reads.
func (z *ZenityPrompt) textInfo(ctx context.Context, body string) error {
	return withTempText(body, func(path string) error {
		args := []string{"--title", z.h.title(), "--text-info", "--filename", path,
			"--width", "640", "--height", "480"}
		if _, err := z.run(ctx, args); err != nil {
			return z.cancelled(ctx, err)
		}
		return nil
	})
}

// cancelled reads the single code of a zenity cancel: 1 for a closed window or
// for the button that abandons the step.
func (z *ZenityPrompt) cancelled(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if code, ok := exitStatus(err); ok && code == 1 {
		return ErrPromptCancelled
	}
	return err
}

// run is one zenity window: the shared environment carries ZENITY_EXTRA, so
// the extra button of a question cannot be mistaken for a cancel.
func (z *ZenityPrompt) run(ctx context.Context, args []string) (string, error) {
	out, err := runTool(ctx, z.path, args, zenityEnv())
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return out, err
}

// ZenityPrompt is a Prompter; the check fails at compile time if it drifts.
var _ Prompter = (*ZenityPrompt)(nil)

// reportKind is the window kind of the verification, taken from the worst
// state of the report.
func reportKind(report ntfy.Report) string {
	switch report.State() {
	case ntfy.StateUnverifiable:
		return "--warning"
	case ntfy.StateFailed:
		return "--error"
	}
	return "--info"
}
