// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// ErrPromptCancelled says the person stopped the interview: they closed a
// dialog or answered the question that ends it. Nothing was written, and the
// wizard treats it as a cancellation, not as a failure.
var ErrPromptCancelled = errors.New("the interview was cancelled")

// Prompter is how the configuration wizard talks to the person: one operation
// per step of the interview, so the text front end and a desktop front end
// drive the same steps and only the way of asking changes.
//
// The two desktop implementations run kdialog and zenity as child processes,
// which is why every operation that opens a window takes the context: it is
// what kills the dialog when the program is told to stop. The deadline of a
// dialog is the person's, so the adapters do not impose one of their own.
type Prompter interface {
	// Step announces the screen that comes next, so a dialog can title itself
	// "2 of 5 · Credentials". The text front end does not need it.
	Step(n, total int, name string)
	// Line asks for a line of text, offering current as the value Enter keeps.
	Line(ctx context.Context, question, current string) (string, error)
	// Secret asks for a value that is not echoed and never travels as an
	// argument of the dialog.
	Secret(ctx context.Context, question string) (string, error)
	// YesNo asks a closed question, where def is what Enter answers.
	YesNo(ctx context.Context, question string, def bool) (bool, error)
	// Topics asks for the list of topics, starting from current.
	Topics(ctx context.Context, current []string) ([]string, error)
	// Report shows the outcome of the checks over one candidate.
	Report(ctx context.Context, report ntfy.Report, server string) error
	// Confirm shows body as the thing that is about to happen and asks
	// question. False and ErrPromptCancelled both mean "write nothing".
	Confirm(ctx context.Context, question, body string) (bool, error)
	// Note is a line for the console. A desktop front end does not stop for
	// it: the dialogs already carried what the person had to answer.
	Note(text string)
	// Warn reports a problem the person has to read before going on.
	Warn(ctx context.Context, text string) error
	// Done shows the closing message of the interview.
	Done(ctx context.Context, text string) error
}

// ReportLine is one row of the verification screen: what was checked, what the
// check concluded and, when there is one, the reason it gives.
type ReportLine struct {
	Label  string
	State  ntfy.State
	Detail string
}

// String renders the line for a front end that shows plain text.
func (l ReportLine) String(tr Translator) string {
	text := fmt.Sprintf("%s: %s", l.Label, stateWord(l.State, tr))
	if l.Detail != "" {
		text += " — " + l.Detail
	}
	return text
}

// stateWord names a state with a msgid of the catalogues, the same words the
// --check listing uses.
func stateWord(state ntfy.State, tr Translator) string {
	switch state {
	case ntfy.StateOK:
		return tr.T("ok")
	case ntfy.StateUnverifiable:
		return tr.T("unverified")
	case ntfy.StateFailed:
		return tr.T("failed")
	}
	return tr.T("unknown")
}

// ReportLines turns a verification into the rows of its screen. The server URL
// is what the health line names when it went well; every other detail comes
// from the result itself.
func ReportLines(report ntfy.Report, server string, tr Translator) []ReportLine {
	lines := make([]ReportLine, 0, 2+len(report.Read))
	lines = append(lines, ReportLine{
		Label:  tr.T("server"),
		State:  report.Health.State,
		Detail: healthDetail(report.Health, server, tr),
	}, ReportLine{
		Label:  tr.T("credentials"),
		State:  report.Account.State,
		Detail: accountDetail(report.Account, tr),
	})
	for _, res := range report.Read {
		lines = append(lines, ReportLine{
			Label:  tr.T("topic %s", res.Topic),
			State:  res.State,
			Detail: reason(res),
		})
	}
	return lines
}

// ReportFooter is the line that explains what an unverified check means, or
// "" when every check concluded.
func ReportFooter(report ntfy.Report, tr Translator) string {
	if report.State() != ntfy.StateUnverifiable {
		return ""
	}
	return tr.T("a check that could not be verified does not block: the configuration can be saved, and the summary warns about it")
}

// healthDetail says what the health check saw.
func healthDetail(res ntfy.Result, server string, tr Translator) string {
	if res.State != ntfy.StateOK {
		return reason(res)
	}
	return tr.T("%s answers as an ntfy server", server)
}

// accountDetail names the credentials the server accepted, or says that it
// asked for none.
func accountDetail(res ntfy.Result, tr Translator) string {
	if res.State != ntfy.StateOK {
		return reason(res)
	}
	if res.Account.Role == ntfy.RoleAnonymous {
		return tr.T("the server asks for no credentials: it answered as %s", res.Account.Role)
	}
	return tr.T("%s, role %s", res.Account.Username, res.Account.Role)
}

// reason is the line a check gives for a state that is not ok. The text is
// technical and stays as the probe wrote it, like the error of publish.
func reason(res ntfy.Result) string {
	if res.Err == nil {
		return ""
	}
	return res.Err.Error()
}

// heading is the title state the two desktop front ends share: the step the
// wizard announced and the count it belongs to.
type heading struct {
	tr    Translator
	pos   int
	total int
	name  string
}

// set records the step the wizard is about to ask.
func (h *heading) set(n, total int, name string) {
	h.pos, h.total, h.name = n, total, name
}

// title is the window title: the step and its count, the way the brief draws
// the dialogs.
func (h *heading) title() string {
	switch {
	case h.name == "":
		return h.tr.T("Configure ntfy-desktop")
	case h.total > 0 && h.pos > 0:
		return h.tr.T("Configure ntfy-desktop — %d of %d · %s", h.pos, h.total, h.name)
	default:
		return h.tr.T("Configure ntfy-desktop — %s", h.name)
	}
}

// esc escapes the three characters both renderers read as markup: Qt's rich
// text in kdialog and Pango's in zenity. Values the person typed and errors
// from the network are text, never formatting.
func esc(text string) string { return markup.Replace(text) }

// runTool runs one dialog and returns its standard output. A non-zero exit is
// left as an *exec.ExitError for the caller to read: the same code means
// different things in each tool and each kind of dialog, so the translation
// belongs to the adapter, not here.
func runTool(ctx context.Context, path string, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// What the tool said on stderr is the only explanation a broken
			// dialog gives; it travels with the exit code.
			if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
				return "", fmt.Errorf("%s: %w", detail, err)
			}
		}
		return "", fmt.Errorf("running %s: %w", filepath.Base(path), err)
	}
	return string(out), nil
}

// exitStatus reads the code of a tool that ended with one.
func exitStatus(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// splitLines reads the rows a list dialog printed, without the empty ones a
// trailing separator leaves behind.
func splitLines(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}
