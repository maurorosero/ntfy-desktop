// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/dialog"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/setup"
)

// Exit codes of the config command. They are part of its contract: --check
// returns them, and the wizard and the commands that edit one key use the same
// number for the same failure, so a script can tell a bad file from a refused
// password.
const (
	exitOK          = 0
	exitNothing     = 1 // nothing was written
	exitFile        = 2 // the file cannot be read, or the change leaves it invalid
	exitServer      = 3 // the server does not answer, or it is not ntfy
	exitCredentials = 4 // the server rejected the credentials (401)
	exitPermission  = 5 // the credentials have no permission for a topic (403)
)

// Keys of the configuration a person can name on the command line. The
// password is not one of them: it would travel in argv, where ps and the
// history of the shell can see it.
const (
	keyServer   = "server"
	keyTopic    = "topic"
	keyUser     = "user"
	keyPassword = "password"
	keyLogLevel = "log-level"
)

// maskedPassword is what stands in for the password. It is a mask, not a text:
// it is never translated and it never carries the length of the secret.
const maskedPassword = "••••••••"

// attempts is how many times a step of the wizard asks again before it gives
// up, so a typo does not end the interview and a loop does not either.
const attempts = 3

// configOptions is what the config command line asked for.
type configOptions struct {
	show          bool
	check         bool
	gui           bool
	setKey        string // key of --set, "" when the flag was not given
	setValue      string
	topicAdd      string
	topicRemove   string
	passwordStdin bool
}

// splitConfig recognises the command line that asks for the config command.
// The subcommand carries flags of its own, so it reads them with its own
// parser instead of widening the flag set every command shares.
func splitConfig(args []string) (rest []string, ok bool) {
	if len(args) == 0 || args[0] != "config" {
		return nil, false
	}
	return args[1:], true
}

// parseConfig reads the flags of the config command into opts.config. The
// command has three forms: no flag (the wizard), one of --show, --check or
// --set, and the topic sub-form that adds or removes one name.
func (o *options) parseConfig(args []string, tr *i18n.Translator, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "topic" {
		return o.parseConfigTopic(args[1:], tr, stderr)
	}

	var c configOptions
	fs := flag.NewFlagSet("ntfy-desktop config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, tr.T("usage: ntfy-desktop config [--gui | --show | --check | --set key=value] [--password-stdin]"))
		fmt.Fprintln(stderr, tr.T("       ntfy-desktop config topic --add name | --remove name"))
		fs.PrintDefaults()
	}
	fs.BoolVar(&c.show, "show", false, tr.T("show the effective configuration and the path of the file"))
	fs.BoolVar(&c.check, "check", false, tr.T("verify the file, the server, the credentials and every topic, and write nothing"))
	fs.BoolVar(&c.gui, "gui", false, tr.T("ask with desktop dialogs, one step per dialog: kdialog on Plasma, zenity as a fallback, the text wizard when there is neither"))
	set := fs.String("set", "", tr.T("change one key: server, topic, user or log-level"))
	fs.BoolVar(&c.passwordStdin, "password-stdin", false, tr.T("read the password from the standard input instead of asking for it without echo"))

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return errUsage
	}
	if rest := fs.Args(); len(rest) > 0 {
		return fmt.Errorf(tr.T("unexpected argument %q"), rest[0])
	}

	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	actions := 0
	for _, name := range []string{"gui", "show", "check", "set"} {
		if given[name] {
			actions++
		}
	}
	if actions > 1 {
		return errors.New(tr.T("config takes one of --gui, --show, --check or --set"))
	}

	if given["set"] {
		key, value, ok := strings.Cut(*set, "=")
		if !ok {
			return errors.New(tr.T("--set expects key=value"))
		}
		switch key = strings.TrimSpace(key); key {
		case keyServer, keyTopic, keyUser, keyLogLevel:
			c.setKey, c.setValue = key, value
		case keyPassword:
			return errors.New(tr.T("the password is never given as an argument: use --password-stdin"))
		default:
			return fmt.Errorf(tr.T("--set does not know the key %q: use server, topic, user or log-level"), key)
		}
	}

	if c.passwordStdin && (c.show || c.check || c.gui) {
		return errors.New(tr.T("--password-stdin goes with --set, or alone to change only the password"))
	}

	o.config = c
	return nil
}

// parseConfigTopic reads the flags of `config topic`, which takes exactly one
// of --add and --remove: both at once is a command line worth reporting, not
// something to guess about.
func (o *options) parseConfigTopic(args []string, tr *i18n.Translator, stderr io.Writer) error {
	var c configOptions
	fs := flag.NewFlagSet("ntfy-desktop config topic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, tr.T("usage: ntfy-desktop config topic --add name | --remove name"))
		fs.PrintDefaults()
	}
	fs.StringVar(&c.topicAdd, "add", "", tr.T("add a topic to the list"))
	fs.StringVar(&c.topicRemove, "remove", "", tr.T("remove a topic from the list"))

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return errUsage
	}
	if rest := fs.Args(); len(rest) > 0 {
		return fmt.Errorf(tr.T("unexpected argument %q"), rest[0])
	}

	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	switch {
	case given["add"] == given["remove"]:
		return errors.New(tr.T("config topic takes exactly one of --add or --remove"))
	case given["add"] && strings.TrimSpace(c.topicAdd) == "":
		return errors.New(tr.T("--add needs a topic name"))
	case given["remove"] && strings.TrimSpace(c.topicRemove) == "":
		return errors.New(tr.T("--remove needs a topic name"))
	}
	c.topicAdd = strings.TrimSpace(c.topicAdd)
	c.topicRemove = strings.TrimSpace(c.topicRemove)

	o.config = c
	return nil
}

// runConfig runs the config command: show, check, edit or interview, over the
// file the program reads.
func runConfig(ctx context.Context, opts options, env platform.Env, tr *i18n.Translator, stdin io.Reader, stdout, stderr io.Writer) int {
	path, err := config.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitNothing
	}

	c := opts.config
	switch {
	case c.show:
		return showConfiguration(path, tr, stdout, stderr)
	case c.check:
		return checkConfiguration(ctx, path, tr, stdout, stderr)
	case c.topicAdd != "" || c.topicRemove != "" || c.setKey != "" || c.passwordStdin:
		return editConfiguration(ctx, c, path, tr, stdin, stdout, stderr)
	default:
		pr := prompterFor(c, env, tr, stdin, stdout, stderr)
		return runWizard(ctx, path, pr, func(cfg config.Config) Prober { return ntfy.NewProbe(cfg) }, tr)
	}
}

// prompterFor picks the front end of the interview. --gui asks the desktop for
// its dialog tool; when there is none, or no session to draw it in, the text
// wizard runs and says so in one line instead of failing.
func prompterFor(c configOptions, env platform.Env, tr *i18n.Translator, stdin io.Reader, stdout, stderr io.Writer) Prompter {
	text := func() Prompter { return newTextPrompter(tr, stdin, stdout, stderr) }
	if !c.gui {
		return text()
	}
	switch {
	case env.DialogTool == "":
		fmt.Fprintln(stderr, tr.T("no kdialog or zenity in the PATH: the interview continues in text mode"))
		return text()
	case !hasDisplay():
		fmt.Fprintln(stderr, tr.T("there is no desktop session: the interview continues in text mode"))
		return text()
	}
	switch filepath.Base(env.DialogTool) {
	case "kdialog":
		return dialog.NewKDialogPrompt(env.DialogTool, tr)
	case "zenity":
		return dialog.NewZenityPrompt(env.DialogTool, tr)
	default:
		fmt.Fprintln(stderr, tr.T("%s is not a dialog tool this program knows: the interview continues in text mode", env.DialogTool))
		return text()
	}
}

// hasDisplay reports whether the session has a screen a dialog could be drawn
// on. Without one, kdialog and zenity have nowhere to open, and a wizard that
// hung on an error no window could show would be worse than the text one.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// loadConfig reads the file and says why it cannot be used: a missing one is
// not an invalid one, so the message names the command that creates it.
func loadConfig(path string, tr *i18n.Translator, stderr io.Writer) (*config.Config, error) {
	cfg, err := config.Load(path)
	switch {
	case err == nil:
		return cfg, nil
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(stderr, tr.T("there is no configuration at %s: run ntfy-desktop config to create one", path))
		return nil, err
	default:
		fmt.Fprintf(stderr, "%s\n", tr.T("the configuration at %s cannot be read: %s", path, describe(tr, err)))
		return nil, err
	}
}

// showConfiguration prints the configuration in use and where it lives. The
// password is a mask: the command exists to look at the file without opening
// it, not to read the secret out of it.
func showConfiguration(path string, tr *i18n.Translator, stdout, stderr io.Writer) int {
	cfg, err := loadConfig(path, tr, stderr)
	if err != nil {
		return exitFile
	}

	fmt.Fprintf(stdout, "%-12s %s\n", tr.T("path"), path)
	fmt.Fprintf(stdout, "%-12s %s\n", tr.T("server"), cfg.ServerURL)
	fmt.Fprintf(stdout, "%-12s %s\n", tr.T("topic"), strings.Join(cfg.Topics, ", "))
	if cfg.AuthMode() == config.AuthBasic {
		fmt.Fprintf(stdout, "%-12s %s\n", tr.T("user"), cfg.User)
		fmt.Fprintf(stdout, "%-12s %s\n", tr.T("password"), maskedPassword)
	} else {
		fmt.Fprintf(stdout, "%-12s %s\n", tr.T("credentials"), tr.T("none: the subscription is anonymous"))
	}
	level := cfg.LogLevel
	if level == "" {
		level = tr.T("info (default)")
	}
	fmt.Fprintf(stdout, "%-12s %s\n", tr.T("log level"), level)
	return exitOK
}

// checkConfiguration verifies the file, the server, the credentials and every
// topic, and writes nothing. The exit code says what failed: 0 none, 2 the
// file, 3 the server, 4 the credentials, 5 the permission for a topic.
func checkConfiguration(ctx context.Context, path string, tr *i18n.Translator, stdout, stderr io.Writer) int {
	cfg, err := loadConfig(path, tr, stderr)
	if err != nil {
		return exitFile
	}

	fmt.Fprintf(stdout, "%-12s %s\n", tr.T("path"), path)
	report := ntfy.NewProbe(*cfg).Run(ctx)
	for _, res := range report.Results() {
		fmt.Fprintf(stdout, "  %-12s %-11s %s\n",
			tr.T(stepLabel(res.Step)), tr.T(stateLabel(res.State)), checkDetail(*cfg, res, tr))
	}

	code := checkExit(report)
	if code != exitOK {
		fmt.Fprintln(stderr, explainExit(code, tr))
	}
	return code
}

// checkExit maps a verification to the exit code of the command. The server is
// the first thing that has to be right: a URL that is not ntfy makes every
// other answer meaningless, and a server that does not answer is what 3 means.
// A route the server does not have is not a failure of the credentials, so an
// account check that could not be verified leaves the code at 0 when the rest
// was verified.
func checkExit(report ntfy.Report) int {
	if report.Health.State == ntfy.StateFailed {
		return exitServer
	}
	for _, res := range append([]ntfy.Result{report.Account}, report.Read...) {
		if res.State == ntfy.StateFailed {
			return resultExit(res)
		}
	}
	for _, res := range report.Read {
		if res.State == ntfy.StateUnverifiable {
			return exitServer
		}
	}
	if report.Health.State == ntfy.StateUnverifiable {
		return exitServer
	}
	return exitOK
}

// resultExit is the code of a check that said no. A 401 is always the
// credentials and a 403 is always a permission, whatever the route.
func resultExit(res ntfy.Result) int {
	switch {
	case errors.Is(res.Err, ntfy.ErrCredentials):
		return exitCredentials
	case errors.Is(res.Err, ntfy.ErrNoPermission):
		return exitPermission
	default:
		return exitServer
	}
}

// stepLabel names a check for the listing: the words are msgids of the
// catalogues.
func stepLabel(step ntfy.Step) string {
	switch step {
	case ntfy.StepHealth:
		return "server"
	case ntfy.StepAccount:
		return "credentials"
	case ntfy.StepRead:
		return "read"
	}
	return "unknown"
}

// stateLabel names the conclusion of a check.
func stateLabel(state ntfy.State) string {
	switch state {
	case ntfy.StateOK:
		return "ok"
	case ntfy.StateUnverifiable:
		return "unverified"
	case ntfy.StateFailed:
		return "failed"
	}
	return "unknown"
}

// checkDetail is what one line says about its check: what was verified when it
// went well, and the reason the probe gives when it did not. The reason is
// technical and stays as the probe wrote it, like the error of publish.
func checkDetail(cfg config.Config, res ntfy.Result, tr *i18n.Translator) string {
	if res.State != ntfy.StateOK {
		if res.Err == nil {
			return ""
		}
		return res.Err.Error()
	}
	switch res.Step {
	case ntfy.StepHealth:
		return tr.T("%s answers as an ntfy server", cfg.ServerURL)
	case ntfy.StepAccount:
		if res.Account.Role == ntfy.RoleAnonymous {
			return tr.T("the server asks for no credentials: it answered as %s", res.Account.Role)
		}
		return tr.T("%s, role %s", res.Account.Username, res.Account.Role)
	default:
		return tr.T("the stream of %s opened", res.Topic)
	}
}

// explainExit is the line that closes a command whose code is not 0.
func explainExit(code int, tr *i18n.Translator) string {
	switch code {
	case exitFile:
		return tr.T("the configuration file cannot be read or is not valid")
	case exitServer:
		return tr.T("the server does not answer, or it is not an ntfy server")
	case exitCredentials:
		return tr.T("the server rejected the credentials")
	case exitPermission:
		return tr.T("the credentials have no permission for a topic")
	}
	return ""
}

// editConfiguration applies --set, --password-stdin, --add or --remove to the
// file and writes only after the candidate loads and the server accepts it.
func editConfiguration(ctx context.Context, c configOptions, path string, tr *i18n.Translator, stdin io.Reader, stdout, stderr io.Writer) int {
	draft, err := editDraft(path, tr, stderr)
	if err != nil {
		return exitFile
	}

	if c.setKey != "" {
		value, err := normalise(c.setKey, c.setValue, tr)
		if err != nil {
			fmt.Fprintln(stderr, describe(tr, err))
			return exitFile
		}
		if err := draft.Set(c.setKey, value); err != nil {
			fmt.Fprintf(stderr, "%s\n", tr.T("the change was refused: %s", describe(tr, err)))
			return exitFile
		}
	}
	if c.passwordStdin {
		secret, err := readPassword(stdin)
		if err != nil {
			fmt.Fprintln(stderr, tr.T("reading the password from the standard input failed: %s", err))
			return exitNothing
		}
		if err := draft.Set(keyPassword, secret); err != nil {
			fmt.Fprintf(stderr, "%s\n", tr.T("the change was refused: %s", describe(tr, err)))
			return exitFile
		}
	}
	if c.topicAdd != "" {
		if err := draft.AddTopic(c.topicAdd); err != nil {
			fmt.Fprintf(stderr, "%s\n", tr.T("the change was refused: %s", describe(tr, err)))
			return exitFile
		}
	}
	if c.topicRemove != "" {
		if err := draft.RemoveTopic(c.topicRemove); err != nil {
			fmt.Fprintf(stderr, "%s\n", tr.T("the change was refused: %s", describe(tr, err)))
			return exitFile
		}
	}

	return writeDraft(ctx, draft, touchesServer(c), tr, stdout, stderr)
}

// editDraft opens the file an edit changes. An edit does not create a missing
// file: there is nothing to change yet, and a half configuration is not what
// anybody wants.
func editDraft(path string, tr *i18n.Translator, stderr io.Writer) (*setup.Draft, error) {
	draft, err := setup.Open(path)
	switch {
	case err == nil:
		return draft, nil
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(stderr, tr.T("there is no configuration at %s: run ntfy-desktop config to create one", path))
	default:
		fmt.Fprintf(stderr, "%s\n", tr.T("the configuration at %s cannot be edited: %s", path, describe(tr, err)))
	}
	return nil, err
}

// touchesServer reports whether the change can affect what the server sees, so
// the verification runs only when it has something to say: changing the log
// level is local and does not need the network.
func touchesServer(c configOptions) bool {
	if c.passwordStdin || c.topicAdd != "" || c.topicRemove != "" {
		return true
	}
	switch c.setKey {
	case keyServer, keyTopic, keyUser:
		return true
	}
	return false
}

// normalise applies the rules of the file to the value typed on the command
// line and returns the form the file keeps.
func normalise(key, value string, tr *i18n.Translator) (string, error) {
	switch key {
	case keyServer:
		return config.CheckServer(strings.TrimSpace(value))
	case keyTopic:
		list, err := parseTopics(value, tr)
		if err != nil {
			return "", err
		}
		return strings.Join(list, ", "), nil
	case keyUser:
		return strings.TrimSpace(value), nil
	default:
		return strings.ToLower(strings.TrimSpace(value)), nil
	}
}

// parseTopics reads the list a person typed: comma separated, where the spaces
// around a name are typing and not part of it.
func parseTopics(value string, tr *i18n.Translator) ([]string, error) {
	return validateTopics(strings.Split(value, ","), tr)
}

// validateTopics applies the rules of the file to a list of names: every one
// is valid on its own, none repeats, and the list is not empty. An empty name
// and a repeated one are refused by name, because "the topics are invalid" says
// nothing when there are several.
func validateTopics(list []string, tr *i18n.Translator) ([]string, error) {
	if len(list) == 0 {
		return nil, errors.New(tr.T("the list needs at least one topic"))
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, raw := range list {
		name := strings.TrimSpace(raw)
		if err := config.CheckTopic(name); err != nil {
			return nil, fmt.Errorf(tr.T("%q is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores"), name)
		}
		if seen[name] {
			return nil, fmt.Errorf(tr.T("%q is already in the topic list"), name)
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// trimAll trims the names a front end already split, so asking again starts
// from what the person chose instead of from the raw text.
func trimAll(list []string) []string {
	out := make([]string, len(list))
	for i, name := range list {
		out[i] = strings.TrimSpace(name)
	}
	return out
}

// readPassword reads one line from the standard input. It is the only way a
// password enters the command: never as an argument, where ps and the history
// of the shell would keep it.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// writeDraft validates the candidate, verifies it against the server when the
// change can affect what the server sees, and only then writes it. A server
// that does not answer is a warning (the decision of the plan: the file can be
// saved without it); a refusal is not, and leaves the file untouched.
func writeDraft(ctx context.Context, draft *setup.Draft, verify bool, tr *i18n.Translator, stdout, stderr io.Writer) int {
	cfg, err := draft.Config()
	if err != nil {
		fmt.Fprintln(stderr, tr.T("the change would leave the configuration invalid: %s", describe(tr, err)))
		return exitFile
	}

	if verify {
		report := ntfy.NewProbe(*cfg).Run(ctx)
		for _, res := range report.Results() {
			switch res.State {
			case ntfy.StateFailed:
				code := resultExit(res)
				fmt.Fprintf(stderr, "%s: %v\n", tr.T(stepLabel(res.Step)), res.Err)
				fmt.Fprintln(stderr, explainExit(code, tr))
				return code
			case ntfy.StateUnverifiable:
				fmt.Fprintln(stderr, tr.T("warning: %v", res.Err))
			}
		}
	}

	result, err := draft.Write()
	if err != nil {
		fmt.Fprintln(stderr, tr.T("the configuration could not be written: %s", describe(tr, err)))
		return exitNothing
	}
	fmt.Fprintln(stdout, tr.T("wrote %s", result.Path))
	if result.Backup != "" {
		fmt.Fprintln(stdout, tr.T("the previous version is in %s", result.Backup))
	}
	return exitOK
}
