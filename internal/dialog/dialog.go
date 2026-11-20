// SPDX-License-Identifier: GPL-3.0-or-later

// Package dialog asks the desktop which action the person wants for a message.
//
// It declares the Decider contract and its two implementations, kdialog on KDE
// and zenity on GNOME. Both are run as child processes, so the client draws no
// widgets of its own and needs neither Qt nor GTK; the desktop localizes its
// own buttons and this client brings the labels of its own entries translated
// by the catalog.
package dialog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/maurorosero/ntfy-desktop/internal/action"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// ErrCancelled says the user closed the dialog without choosing. The request
// is not answered and must not be thrown away: the caller decides what to do
// with it.
var ErrCancelled = errors.New("dialog closed without an answer")

// Decider shows the actions of a message and returns the label of the chosen
// one. It returns ErrCancelled when the user closes the dialog.
type Decider interface {
	Ask(ctx context.Context, m ntfy.Message, actions []ntfy.Action) (string, error)
}

// Translator is the part of the catalog a dialog uses. It is declared here,
// where it is used, so the adapters do not depend on the i18n package.
type Translator interface {
	T(msgid string, vars ...any) string
}

// New returns the decider for the tool found at start-up, which is the path
// platform.Detect left in Env.DialogTool. An empty or unknown tool is an error:
// a decision without a dialog would have nowhere to be answered.
func New(tool string, tr Translator) (Decider, error) {
	switch filepath.Base(tool) {
	case "kdialog":
		return NewKDialog(tool, tr), nil
	case "zenity":
		return NewZenity(tool, tr), nil
	default:
		return nil, fmt.Errorf("unsupported dialog tool %q", tool)
	}
}

// pending is the place of a request among the ones waiting for an answer.
type pending struct {
	pos   int // from 1
	total int // how many requests this batch has
}

type pendingKey struct{}

// WithPending marks ctx with the place of this request among the pending ones,
// so the title can say "1 of 2" and the person knows others are waiting behind.
// Without it the title is just "Decision".
func WithPending(ctx context.Context, pos, total int) context.Context {
	return context.WithValue(ctx, pendingKey{}, pending{pos: pos, total: total})
}

// Pending reads back what WithPending wrote. It reports zeros when the context
// carries nothing, which is the case of a lone request.
func Pending(ctx context.Context) (pos, total int) {
	p, ok := ctx.Value(pendingKey{}).(pending)
	if !ok {
		return 0, 0
	}
	return p.pos, p.total
}

// title is the window title: the count is what tells the person that answering
// one request does not end the queue.
func title(ctx context.Context, tr Translator) string {
	pos, total := Pending(ctx)
	if total > 1 && pos > 0 {
		return tr.T("Decision (%d of %d)", pos, total)
	}
	return tr.T("Decision")
}

// choice is one row of the dialog: the tag the tool prints back and the label
// the person reads. The tag is a number, so two actions with the same label
// still resolve to one of them.
type choice struct {
	tag   string
	label string
}

// dismissTag is the entry that means "not now". Action tags start at 1, so no
// action can collide with it.
const dismissTag = "0"

// choices numbers the actions and closes the list with the way out. The list
// always carries our own "Dismiss" entry: KDE's and GTK's buttons are
// localized by the desktop, in the desktop's language, not in ours.
func choices(tr Translator, actions []ntfy.Action) []choice {
	out := make([]choice, 0, len(actions)+1)
	for i, a := range actions {
		out = append(out, choice{tag: strconv.Itoa(i + 1), label: action.Label(a)})
	}
	return append(out, choice{tag: dismissTag, label: tr.T("Dismiss")})
}

// resolve turns the tag the tool printed into the label of the action.
func resolve(tag string, actions []ntfy.Action) (string, error) {
	if tag == dismissTag {
		return "", ErrCancelled
	}
	i, err := strconv.Atoi(tag)
	if err != nil || i < 1 || i > len(actions) {
		return "", fmt.Errorf("dialog answered %q, which is not one of its choices", tag)
	}
	return action.Label(actions[i-1]), nil
}

// askError tells the three ways a dialog can end: the user closed it, the
// caller gave up on it, or the tool itself failed.
func askError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// Both tools use exit code 1 for their cancel button.
		return ErrCancelled
	}
	return fmt.Errorf("running the dialog: %w", err)
}
