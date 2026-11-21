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
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/setup"
)

// Prompter asks the person one question at a time. The wizard consumes it, so
// the text front end, a dialog front end and a test with fixed answers all
// drive the same steps: nothing below reads the terminal itself.
type Prompter interface {
	// Line asks for a line of text, offering current as the value Enter keeps.
	Line(question, current string) (string, error)
	// Secret asks for a value that is not echoed.
	Secret(question string) (string, error)
	// YesNo asks a closed question, where def is what Enter answers.
	YesNo(question string, def bool) (bool, error)
	// Note shows a line that needs no answer.
	Note(text string)
}

// ErrCancelled reports that the person stopped the wizard. Nothing is written.
var ErrCancelled = errors.New("cancelled by the person")

// Prober is the part of the verification the wizard shows while it asks: the
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
		if msg := w.reason(err); msg != "" {
			w.pr.Note(msg)
		}
		w.pr.Note(tr.T("nothing was written"))
		var stop *blocked
		if errors.As(err, &stop) {
			return stop.code
		}
		return exitNothing
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
}

// interview runs the steps in the order of the plan: server, credentials,
// topics, the read check of every topic, and the summary before the file is
// touched.
func (w *wizard) interview(ctx context.Context, path string) error {
	draft, base, err := w.open(path)
	if err != nil {
		return err
	}

	server, err := w.askServer(ctx, base.ServerURL)
	if err != nil {
		return err
	}
	if err := draft.Set(keyServer, server); err != nil {
		return w.editFailed(err)
	}

	user, password, err := w.askCredentials(ctx, server, base)
	if err != nil {
		return err
	}
	if err := w.setPair(draft, base, user, password); err != nil {
		return err
	}

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
	unverified, err := w.checkReads(ctx, *cfg)
	if err != nil {
		return err
	}

	if unverified {
		w.pr.Note(w.tr.T("warning: some checks could not be verified; the configuration can still be saved"))
	}
	w.pr.Note(w.tr.T("this is what will be written to %s:", path))
	w.pr.Note("")
	w.pr.Note(string(draft.Masked(maskedPassword)))
	w.pr.Note("")

	write, err := w.pr.YesNo(w.tr.T("write the configuration?"), true)
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
	w.pr.Note(w.tr.T("wrote %s", result.Path))
	if result.Backup != "" {
		w.pr.Note(w.tr.T("the previous version is in %s", result.Backup))
	}
	return nil
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
// answer is a warning, not a stop: the file can be saved without it.
func (w *wizard) askServer(ctx context.Context, base string) (string, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", ErrCancelled
		}
		answer, err := w.pr.Line(w.tr.T("server URL (http or https, with no query and no fragment):"), base)
		if err != nil {
			return "", err
		}
		value, err := config.CheckServer(answer)
		if err != nil {
			w.pr.Note(w.tr.T("that URL cannot be used: %s", describe(w.tr, err)))
			if attempt == attempts {
				return "", &blocked{code: exitFile, msg: w.tr.T("the server URL was left invalid")}
			}
			continue
		}

		res := w.verify(config.Config{ServerURL: value}).Health(ctx)
		switch res.State {
		case ntfy.StateFailed:
			w.pr.Note(w.tr.T("%s does not look like an ntfy server: %v", value, res.Err))
			if attempt == attempts {
				return "", &blocked{code: exitServer, msg: w.tr.T("%s is not an ntfy server", value)}
			}
			continue
		case ntfy.StateUnverifiable:
			w.pr.Note(w.tr.T("warning: %s does not answer now: %v", value, res.Err))
		}
		return value, nil
	}
}

// askCredentials asks whether the server uses a pair, and verifies it against
// /v1/account. A refused pair is asked again —the mistake is one keystroke
// away— until the attempts run out, and then it stops the wizard.
func (w *wizard) askCredentials(ctx context.Context, server string, base config.Config) (string, string, error) {
	use, err := w.pr.YesNo(w.tr.T("does the server use a user name and password?"), base.AuthMode() == config.AuthBasic)
	if err != nil {
		return "", "", err
	}
	if !use {
		return "", "", nil
	}

	last := base.User
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", "", ErrCancelled
		}
		user, err := w.pr.Line(w.tr.T("user name:"), last)
		if err != nil {
			return "", "", err
		}
		user = strings.TrimSpace(user)
		password, err := w.pr.Secret(w.tr.T("password (it is not shown):"))
		if err != nil {
			return "", "", err
		}
		if user == "" || password == "" {
			w.pr.Note(w.tr.T("the user name and the password are both required"))
			if attempt == attempts {
				return "", "", &blocked{code: exitFile, msg: w.tr.T("the credentials were left incomplete")}
			}
			last = user
			continue
		}

		res := w.verify(config.Config{ServerURL: server, User: user, Password: password}).Account(ctx)
		switch res.State {
		case ntfy.StateOK:
			if res.Account.Role == ntfy.RoleAnonymous {
				w.pr.Note(w.tr.T("the server asks for no credentials: it answered as %s", res.Account.Role))
			} else {
				w.pr.Note(w.tr.T("credentials accepted: %s, role %s", res.Account.Username, res.Account.Role))
			}
			return user, password, nil
		case ntfy.StateUnverifiable:
			w.pr.Note(w.tr.T("warning: the credentials could not be verified: %v", res.Err))
			return user, password, nil
		default:
			w.pr.Note(w.tr.T("the server refused the credentials: %v", res.Err))
			if attempt == attempts || !errors.Is(res.Err, ntfy.ErrCredentials) {
				return "", "", &blocked{code: resultExit(res), msg: w.tr.T("the credentials were refused")}
			}
			last = user
		}
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

// askTopics asks for the list, validates every name, and refuses an empty or a
// repeated one before anything else happens.
func (w *wizard) askTopics(ctx context.Context, base []string) ([]string, error) {
	current := strings.Join(base, ", ")
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, ErrCancelled
		}
		answer, err := w.pr.Line(w.tr.T("topics, separated by commas:"), current)
		if err != nil {
			return nil, err
		}
		list, err := parseTopics(answer, w.tr)
		if err != nil {
			w.pr.Note(w.tr.T("those topics cannot be used: %s", describe(w.tr, err)))
			if attempt == attempts {
				return nil, &blocked{code: exitFile, msg: w.tr.T("the topics were left invalid")}
			}
			continue
		}
		return list, nil
	}
}

// checkReads opens the stream of every topic and cuts it on the first event.
// A refusal stops the wizard; a check that could not be made is a warning that
// travels to the summary.
func (w *wizard) checkReads(ctx context.Context, cfg config.Config) (bool, error) {
	prober := w.verify(cfg)
	unverified := false
	for _, topic := range cfg.Topics {
		res := prober.Read(ctx, topic)
		switch res.State {
		case ntfy.StateOK:
			w.pr.Note(w.tr.T("read %s: ok", topic))
		case ntfy.StateUnverifiable:
			w.pr.Note(w.tr.T("warning: %s could not be read now: %v", topic, res.Err))
			unverified = true
		default:
			return false, &blocked{code: resultExit(res),
				msg: w.tr.T("%s cannot be read: %v", topic, res.Err)}
		}
	}
	return unverified, nil
}

// editFailed reports an edit the engine refused, which a wizard step never
// provokes on purpose: it means the draft and the values disagree.
func (w *wizard) editFailed(err error) error {
	return &blocked{code: exitFile, msg: w.tr.T("the change was refused: %s", describe(w.tr, err))}
}

// reason is the line that explains why the wizard stopped, empty when the
// person cancelled: there is nothing to explain about that.
func (w *wizard) reason(err error) string {
	if errors.Is(err, ErrCancelled) {
		return ""
	}
	return fmt.Sprint(err)
}
