// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// textPrompter asks through the terminal, one question at a time: a line, a
// secret, a yes or no, and a note that needs no answer. It is the text front
// end of the wizard; the desktop one (issue #42) asks through dialogs with the
// same interface.
type textPrompter struct {
	tr       *i18n.Translator
	in       *bufio.Reader
	out      io.Writer
	warn     io.Writer
	terminal *os.File // the input, when it is a terminal whose echo can be turned off
}

// newTextPrompter builds the prompter of the terminal. When the input is not a
// terminal —a pipe, a file— there is no echo to hide and the answers are read
// as they come, which is what lets a script drive the wizard.
func newTextPrompter(tr *i18n.Translator, in io.Reader, out, warn io.Writer) *textPrompter {
	p := &textPrompter{tr: tr, in: bufio.NewReader(in), out: out, warn: warn}
	if f, ok := in.(*os.File); ok && isTerminal(f) {
		p.terminal = f
	}
	return p
}

// Line asks for a line and offers current as the value Enter keeps.
func (p *textPrompter) Line(_ context.Context, question, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(p.out, "%s [%s] ", question, current)
	} else {
		fmt.Fprintf(p.out, "%s ", question)
	}
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	if line == "" && current != "" {
		return current, nil
	}
	return line, nil
}

// Secret asks for a value that is not echoed. When the echo cannot be turned
// off the value is read anyway, with the warning that it will be visible: a
// missing stty must not stop the configuration.
func (p *textPrompter) Secret(_ context.Context, question string) (string, error) {
	fmt.Fprintf(p.out, "%s ", question)
	if p.terminal == nil {
		return p.readLine()
	}
	if err := echo(p.terminal, false); err != nil {
		fmt.Fprintln(p.warn, p.tr.T("warning: the terminal echo could not be turned off, so the password will be visible: %v", err))
		return p.readLine()
	}
	// The echo comes back on every way out of here, including a read error.
	// An interrupt during the read is delivered to the context, but the read
	// itself ends with the line: the window without echo never outlives it.
	defer func() {
		if err := echo(p.terminal, true); err != nil {
			fmt.Fprintln(p.warn, p.tr.T("warning: the terminal echo could not be turned back on: %v", err))
		}
	}()
	line, err := p.readLine()
	fmt.Fprintln(p.out)
	return line, err
}

// YesNo asks a closed question. Enter keeps def, and anything that is not an
// answer asks again instead of guessing.
func (p *textPrompter) YesNo(_ context.Context, question string, def bool) (bool, error) {
	hint := p.tr.T("y/n")
	if def {
		hint = p.tr.T("Y/n")
	}
	for {
		fmt.Fprintf(p.out, "%s [%s] ", question, hint)
		line, err := p.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes", "s", "si", "sí":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.out, p.tr.T("answer yes or no"))
	}
}

// Note shows a line that needs no answer.
func (p *textPrompter) Note(text string) {
	fmt.Fprintln(p.out, text)
}

// Step does nothing: the terminal asks one question at a time and the question
// itself says where the interview is. The desktop front end uses it for the
// title of the dialog.
func (p *textPrompter) Step(int, int, string) {}

// Topics asks for the list as one comma separated line. Splitting and
// validating is the wizard's: a name is refused by name, and the question
// comes back with what the person wrote.
func (p *textPrompter) Topics(ctx context.Context, current []string) ([]string, error) {
	answer, err := p.Line(ctx, p.tr.T("topics, separated by commas:"), strings.Join(current, ", "))
	if err != nil {
		return nil, err
	}
	return strings.Split(answer, ","), nil
}

// Report prints the outcome of every check, one line each.
func (p *textPrompter) Report(_ context.Context, report ntfy.Report, server string) error {
	for _, line := range dialog.ReportLines(report, server, p.tr) {
		fmt.Fprintf(p.out, "  %s\n", line.String(p.tr))
	}
	if footer := dialog.ReportFooter(report, p.tr); footer != "" {
		fmt.Fprintln(p.out, footer)
	}
	return nil
}

// Confirm prints the body and asks the question: what a dialog shows in two
// windows is two paragraphs and a yes/no here.
func (p *textPrompter) Confirm(ctx context.Context, question, body string) (bool, error) {
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintln(p.out, line)
	}
	return p.YesNo(ctx, question, true)
}

// Warn prints a problem the person has to read.
func (p *textPrompter) Warn(_ context.Context, text string) error {
	fmt.Fprintln(p.out, text)
	return nil
}

// Done prints the closing message of the interview.
func (p *textPrompter) Done(_ context.Context, text string) error {
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintln(p.out, line)
	}
	return nil
}

// textPrompter is a Prompter; the check fails at compile time if it drifts.
var _ dialog.Prompter = (*textPrompter)(nil)

// readLine reads one answer. A stream that ends —Ctrl-D, or the last line of a
// script without its newline— is the person stopping the wizard, which is a
// cancellation and not an error.
func (p *textPrompter) readLine() (string, error) {
	line, err := p.in.ReadString('\n')
	switch {
	case err == nil:
		return strings.TrimRight(line, "\r\n"), nil
	case errors.Is(err, io.EOF) && line != "":
		return strings.TrimRight(line, "\r\n"), nil
	case errors.Is(err, io.EOF):
		return "", ErrCancelled
	default:
		return "", fmt.Errorf("%s: %w", p.tr.T("reading the answer"), err)
	}
}

// echo turns the echo of the terminal on or off. stty is part of coreutils and
// works on the file it reads, so the same terminal the answers come from is
// the one that stops showing them.
func echo(f *os.File, on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stty %s: %w: %s", arg, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// isTerminal reports whether f is a terminal that stty can drive. Asking stty
// answers both questions at once —is it a terminal, and is stty there— with one
// call, and it never guesses from the file mode: /dev/null is a character
// device too.
func isTerminal(f *os.File) bool {
	cmd := exec.Command("stty", "-a")
	cmd.Stdin = f
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
