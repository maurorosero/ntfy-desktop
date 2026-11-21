// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/setup"
)

// Prompter is how the wizard talks to the person. The contract lives next to
// the adapters that implement it (internal/dialog), the way Decider does, so
// the text front end and the desktop ones drive the same steps.
type Prompter = dialog.Prompter

// ErrCancelled reports that the person stopped the wizard. Nothing is written.
var ErrCancelled = dialog.ErrPromptCancelled

// steps is how many screens the interview has, for the "1 of 5" of a dialog
// title. Asking again inside a step does not add one.
const steps = 5

// Prober is the part of the verification the wizard runs while it asks: the
// three checks, one at a time, each with its own short timeout.
type Prober interface {
	Health(ctx context.Context) ntfy.Result
	Account(ctx context.Context) ntfy.Result
	Read(ctx context.Context, topic string) ntfy.Result
}

// Verifier builds the verification of a candidate configuration. The wizard
// asks for one per candidate, so the checks follow the answers: the server
// before the credentials, the credentials before a topic.
type Verifier func(config.Config) Prober

// blocked is a step that said no and ended the wizard. code is the exit code
// the command returns for that failure, so the wizard and --check agree.
type blocked struct {
	code int
	msg  string
}

func (b *blocked) Error() string { return b.msg }

// runWizard creates or edits the configuration by asking, and writes only what
// the person confirms. Every path that ends without that confirmation leaves
// the file exactly as it was.
func runWizard(ctx context.Context, path string, pr Prompter, verify Verifier, tr *i18n.Translator) int {
	w := &wizard{tr: tr, pr: pr, verify: verify}

	if err := w.interview(ctx, path); err != nil {
		if errors.Is(err, ErrCancelled) {
			// A cancellation is an answer of its own: it needs no window that
			// says so, and a desktop front end shows nothing.
			pr.Note(tr.T("nothing was written"))
			return exitNothing
		}
		code := exitNothing
		var stop *blocked
		if errors.As(err, &stop) {
			code = stop.code
		}
		if warnErr := pr.Warn(ctx, err.Error()); warnErr != nil {
			return exitNothing
		}
		pr.Note(tr.T("nothing was written"))
		return code
	}
	return exitOK
}

// wizard is the interview: what it asks, in which order, and the draft it
// builds. It holds no context and no open file: the steps receive what they
// need.
type wizard struct {
	tr     *i18n.Translator
	pr     Prompter
	verify Verifier

	// report collects what each check said, so the verification screen shows
	// the conclusions the steps reached instead of asking the server again.
	report ntfy.Report
}

// interview runs the steps in the order of the plan: server, credentials,
// topics, the check of every control, and the summary before the file is
// touched.
func (w *wizard) interview(ctx context.Context, path string) error {
	draft, base, err := w.open(path)
	if err != nil {
		return err
	}

	w.pr.Step(1, steps, w.tr.T("Server"))
	server, err := w.askServer(ctx, base.ServerURL)
	if err != nil {
		return err
	}
	if err := draft.Set(keyServer, server); err != nil {
		return w.editFailed(err)
	}

	w.pr.Step(2, steps, w.tr.T("Credentials"))
	user, password, err := w.askCredentials(ctx, server, base)
	if err != nil {
		return err
	}
	if err := w.setPair(draft, base, user, password); err != nil {
		return err
	}

	w.pr.Step(3, steps, w.tr.T("Topics"))
	topics, err := w.askTopics(ctx, base.Topics)
	if err != nil {
		return err
	}
	if err := draft.Set(keyTopic, strings.Join(topics, ", ")); err != nil {
		return w.editFailed(err)
	}

	cfg, err := draft.Config()
	if err != nil {
		return &blocked{code: exitFile, msg: w.tr.T("the configuration would not be valid: %s", describe(w.tr, err))}
	}

	w.pr.Step(4, steps, w.tr.T("Verification"))
	unverified, err := w.checkControls(ctx, *cfg)
	if err != nil {
		return err
	}

	w.pr.Step(5, steps, w.tr.T("Summary"))
	write, err := w.pr.Confirm(ctx, w.tr.T("write the configuration?"), w.summary(path, draft, unverified))
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return ErrCancelled
	}
	if !write {
		return ErrCancelled
	}

	result, err := draft.Write()
	if err != nil {
		return &blocked{code: exitNothing, msg: w.tr.T("the configuration could not be written: %s", describe(w.tr, err))}
	}
	return w.pr.Done(ctx, w.done(result))
}

// open loads the file the wizard edits or starts an empty draft. A file the
// loader does not accept is never the base of an edit: it is reported and the
// wizard stops, because overwriting it would throw away what the person wrote.
func (w *wizard) open(path string) (*setup.Draft, config.Config, error) {
	_, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return setup.New(path), config.Config{}, nil
	case err != nil:
		return nil, config.Config{}, &blocked{code: exitFile,
			msg: w.tr.T("the configuration at %s cannot be read: %s", path, describe(w.tr, err))}
	}

	draft, err := setup.Open(path)
	if err != nil {
		return nil, config.Config{}, &blocked{code: exitFile,
			msg: w.tr.T("the configuration at %s cannot be edited: %s", path, describe(w.tr, err))}
	}
	cfg, err := draft.Config()
	if err != nil {
		return nil, config.Config{}, &blocked{code: exitFile,
			msg: w.tr.T("the configuration at %s cannot be edited: %s", path, describe(w.tr, err))}
	}
	return draft, *cfg, nil
}

// askServer asks for the URL and checks it on the spot: the shape with the
// loader and, when it answers, that the server is ntfy. A server that does not
// answer is not asked again: the check stays unverified and the report says so.
func (w *wizard) askServer(ctx context.Context, base string) (string, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", ErrCancelled
		}
		answer, err := w.pr.Line(ctx, w.tr.T("server URL (http or https, with no query and no fragment):"), base)
		if err != nil {
			return "", err
		}
		value, err := config.CheckServer(answer)
		if err != nil {
			if warnErr := w.pr.Warn(ctx, w.tr.T("that URL cannot be used: %s", describe(w.tr, err))); warnErr != nil {
				return "", warnErr
			}
			if attempt == attempts {
				return "", &blocked{code: exitFile, msg: w.tr.T("the server URL was left invalid")}
			}
			continue
		}

		res := w.verify(config.Config{ServerURL: value}).Health(ctx)
		w.report.Health = res
		if res.State == ntfy.StateFailed {
			if warnErr := w.pr.Warn(ctx, w.tr.T("%s does not look like an ntfy server: %v", value, res.Err)); warnErr != nil {
				return "", warnErr
			}
			if attempt == attempts {
				return "", &blocked{code: exitServer, msg: w.tr.T("%s is not an ntfy server", value)}
			}
			continue
		}
		return value, nil
	}
}

// askCredentials asks whether the server uses a pair and verifies it against
// /v1/account. A refused pair is asked again —the mistake is one keystroke
// away— until the attempts run out, and then it stops the wizard. An anonymous
// answer is verified too: a server that answers 401 to it asks for credentials
// the configuration does not have.
func (w *wizard) askCredentials(ctx context.Context, server string, base config.Config) (string, string, error) {
	use, err := w.pr.YesNo(ctx, w.tr.T("does the server use a user name and password?"), base.AuthMode() == config.AuthBasic)
	if err != nil {
		return "", "", err
	}
	if !use {
		res := w.verify(config.Config{ServerURL: server}).Account(ctx)
		w.report.Account = res
		if res.State == ntfy.StateFailed {
			return "", "", &blocked{code: resultExit(res),
				msg: w.tr.T("the server asks for credentials and the configuration has none")}
		}
		return "", "", nil
	}

	last := base.User
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", "", ErrCancelled
		}
		user, err := w.pr.Line(ctx, w.tr.T("user name:"), last)
		if err != nil {
			return "", "", err
		}
		user = strings.TrimSpace(user)
		password, err := w.pr.Secret(ctx, w.tr.T("password (it is not shown):"))
		if err != nil {
			return "", "", err
		}
		if user == "" || password == "" {
			if warnErr := w.pr.Warn(ctx, w.tr.T("the user name and the password are both required")); warnErr != nil {
				return "", "", warnErr
			}
			if attempt == attempts {
				return "", "", &blocked{code: exitFile, msg: w.tr.T("the credentials were left incomplete")}
			}
			last = user
			continue
		}

		res := w.verify(config.Config{ServerURL: server, User: user, Password: password}).Account(ctx)
		w.report.Account = res
		switch res.State {
		case ntfy.StateOK, ntfy.StateUnverifiable:
			return user, password, nil
		}
		if warnErr := w.pr.Warn(ctx, w.tr.T("the server refused the credentials: %v", res.Err)); warnErr != nil {
			return "", "", warnErr
		}
		if attempt == attempts || !errors.Is(res.Err, ntfy.ErrCredentials) {
			return "", "", &blocked{code: resultExit(res), msg: w.tr.T("the server refused the credentials: %v", res.Err)}
		}
		last = user
	}
}

// setPair writes the credentials into the draft. A new anonymous configuration
// gets no user and no password lines at all; one that had a pair gets them
// emptied, which is how the file says "subscribe anonymously" without a key the
// document cannot remove.
func (w *wizard) setPair(draft *setup.Draft, base config.Config, user, password string) error {
	if user != "" || password != "" || base.AuthMode() == config.AuthBasic {
		if err := draft.Set(keyUser, user); err != nil {
			return w.editFailed(err)
		}
		if err := draft.Set(keyPassword, password); err != nil {
			return w.editFailed(err)
		}
	}
	return nil
}

// askTopics asks for the list and validates it. A front end may refuse a name
// where it is typed, but the list is checked here as well: the rule is the
// file's, and a front end is not the place where it is decided.
func (w *wizard) askTopics(ctx context.Context, base []string) ([]string, error) {
	current := base
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, ErrCancelled
		}
		answer, err := w.pr.Topics(ctx, current)
		if err != nil {
			return nil, err
		}
		list, err := validateTopics(answer, w.tr)
		if err != nil {
			if warnErr := w.pr.Warn(ctx, w.tr.T("those topics cannot be used: %s", describe(w.tr, err))); warnErr != nil {
				return nil, warnErr
			}
			if attempt == attempts {
				return nil, &blocked{code: exitFile, msg: w.tr.T("the topics were left invalid")}
			}
			current = trimAll(answer)
			continue
		}
		return list, nil
	}
}

// checkControls shows the verification of the candidate, one line per control,
// and stops when one of them said no. A check that reached no conclusion does
// not stop anything: it travels to the summary as a warning.
func (w *wizard) checkControls(ctx context.Context, cfg config.Config) (bool, error) {
	prober := w.verify(cfg)
	w.report.Read = make([]ntfy.Result, 0, len(cfg.Topics))
	for _, topic := range cfg.Topics {
		w.report.Read = append(w.report.Read, prober.Read(ctx, topic))
	}

	if err := w.pr.Report(ctx, w.report, cfg.ServerURL); err != nil {
		return false, err
	}
	for _, res := range w.report.Read {
		if res.State == ntfy.StateFailed {
			return false, &blocked{code: resultExit(res),
				msg: w.tr.T("%s cannot be read: %v", res.Topic, res.Err)}
		}
	}
	return w.report.State() == ntfy.StateUnverifiable, nil
}

// summary is what the last step shows: the file as it will be written, with
// the password masked, and the warning when a check could not be verified. It
// never carries the secret, not even its length.
func (w *wizard) summary(path string, draft *setup.Draft, unverified bool) string {
	var b strings.Builder
	if unverified {
		b.WriteString(w.tr.T("warning: some checks could not be verified; the configuration can still be saved"))
		b.WriteString("\n\n")
	}
	b.WriteString(w.tr.T("this is what will be written to %s:", path))
	b.WriteString("\n\n")
	b.WriteString(strings.TrimRight(string(draft.Masked(maskedPassword)), "\n"))
	b.WriteString("\n\n")
	b.WriteString(w.tr.T("it is written with 0600 permissions; the previous version, if any, is kept as a .bak next to it"))
	return b.String()
}

// done is the closing message: where the file is and where the old one went.
func (w *wizard) done(result setup.Result) string {
	lines := []string{w.tr.T("wrote %s", result.Path)}
	if result.Backup != "" {
		lines = append(lines, w.tr.T("the previous version is in %s", result.Backup))
	}
	return strings.Join(lines, "\n")
}

// editFailed reports an edit the engine refused, which a wizard step never
// provokes on purpose: it means the draft and the values disagree.
func (w *wizard) editFailed(err error) error {
	return &blocked{code: exitFile, msg: w.tr.T("the change was refused: %s", describe(w.tr, err))}
}

// describe turns a configuration error into one line: the parts that come from
// the catalogues are translated, and the rest stays as the package wrote it.
func describe(tr *i18n.Translator, err error) string {
	var line *config.LineError
	if errors.As(err, &line) {
		return tr.T("line %d: %s", line.Line, describe(tr, line.Err))
	}
	var invalid *config.ValidationError
	if errors.As(err, &invalid) {
		return fmt.Sprintf("%s: %s", invalid.Field, tr.T(invalid.Msg, invalid.Args...))
	}
	var syntax *config.SyntaxError
	if errors.As(err, &syntax) {
		if syntax.Key == "" {
			return tr.T("line %d: %s", syntax.Line, tr.T(syntax.Msg))
		}
		return tr.T("line %d: %s %q", syntax.Line, tr.T(syntax.Msg), syntax.Key)
	}
	var edit *config.EditError
	if errors.As(err, &edit) {
		if edit.Line == 0 {
			return fmt.Sprintf("%s: %s", edit.Key, tr.T(edit.Msg, edit.Args...))
		}
		return tr.T("line %d: %s: %s", edit.Line, edit.Key, tr.T(edit.Msg, edit.Args...))
	}
	return err.Error()
}
