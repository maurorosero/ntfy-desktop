// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/version"
)

// command names one subcommand. The dispatch below is the only place that knows
// them, so a new one adds its own file and one case here.
type command string

const (
	// commandEnv prints the detected environment. It is what no argument means.
	commandEnv command = "env"
	// commandDaemon subscribes, shows what arrives and asks for decisions until
	// the process is told to stop.
	commandDaemon command = "daemon"
	// commandVersion prints the version and exits.
	commandVersion command = "version"
	// commandPublish sends one message, read from stdin, to a topic.
	commandPublish command = "publish"
	// commandHistory lists the messages kept in the local history.
	commandHistory command = "history"
	// commandConfig shows, verifies and edits the configuration.
	commandConfig command = "config"
)

// errUsage reports a command line that was already explained on stderr by the
// flag package, so the caller only has to choose the exit code.
var errUsage = errors.New("invalid command line")

// errHelp reports that the usage was printed because it was asked for.
var errHelp = errors.New("usage printed")

// options is what the command line asked for.
type options struct {
	command  command
	explicit bool           // a subcommand or a flag chose the command
	level    slog.Level     // level of the logger
	levelSet bool           // the flag carried the level, so it wins over the file
	publish  publishOptions // flags of the publish command
	config   configOptions  // flags of the config command
	unread   bool           // history lists only what has not been read
}

// run parses the command line and runs the subcommand it names. It returns the
// exit code of the process, so main only has to exit with it.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	env := platform.Detect()
	tr := i18n.New(env.Locale)

	opts, err := parse(args, tr, stderr)
	switch {
	case errors.Is(err, errUsage):
		return 2
	case errors.Is(err, errHelp):
		return 0
	case err != nil:
		fmt.Fprintln(stderr, err)
		return 2
	}

	switch opts.command {
	case commandDaemon:
		return runDaemon(ctx, opts, env, tr, stderr)
	case commandPublish:
		return runPublish(ctx, opts, tr, os.Stdin, stdout, stderr)
	case commandHistory:
		return runHistory(opts, tr, stdout, stderr)
	case commandConfig:
		return runConfig(ctx, opts, tr, os.Stdin, stdout, stderr)
	case commandVersion:
		fmt.Fprintf(stdout, "ntfy-desktop %s\n", version.Version)
		return 0
	default:
		return runEnv(env, tr, stdout)
	}
}

// parse reads the command line. The subcommand may come first, and the flags
// that select one (--daemon, --version) are its aliases: the service unit uses
// --daemon, a person at a terminal can write either form.
func parse(args []string, tr *i18n.Translator, stderr io.Writer) (options, error) {
	opts := options{command: commandEnv, level: slog.LevelInfo}

	// publish carries its own flags, so it reads its command line with its own
	// parser instead of widening the flag set every command shares.
	if rest, ok := splitPublish(args); ok {
		opts.command, opts.explicit = commandPublish, true
		return opts, opts.parsePublish(rest, tr, stderr)
	}
	// config has three forms of its own, so it parses its command line the same
	// way publish does.
	if rest, ok := splitConfig(args); ok {
		opts.command, opts.explicit = commandConfig, true
		return opts, opts.parseConfig(rest, tr, stderr)
	}

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if err := opts.choose(command(args[0]), tr); err != nil {
			return options{}, err
		}
		args = args[1:]
	}

	fs := flag.NewFlagSet("ntfy-desktop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, tr.T("usage: ntfy-desktop [command] [flags]"))
		fs.PrintDefaults()
	}
	daemon := fs.Bool("daemon", false, tr.T("run until stopped: subscribe, notify and ask for decisions"))
	version := fs.Bool("version", false, tr.T("prints the version and exits"))
	historyCmd := fs.Bool("history", false, tr.T("lists the messages kept in the local history"))
	unread := fs.Bool("unread", false, tr.T("with history, lists only the messages that have not been read"))
	level := fs.String("log-level", "", tr.T("log level: debug, info, warn or error (default: info)"))

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, errHelp
		}
		return options{}, errUsage
	}
	if rest := fs.Args(); len(rest) > 0 {
		return options{}, fmt.Errorf(tr.T("unexpected argument %q"), rest[0])
	}

	for _, flagCmd := range []struct {
		on   bool
		name command
	}{
		{*daemon, commandDaemon},
		{*version, commandVersion},
		{*historyCmd, commandHistory},
	} {
		if !flagCmd.on {
			continue
		}
		if err := opts.choose(flagCmd.name, tr); err != nil {
			return options{}, err
		}
	}

	if *level != "" {
		l, ok := parseLevel(*level)
		if !ok {
			return options{}, fmt.Errorf(tr.T("invalid log level %q: use debug, info, warn or error"), *level)
		}
		opts.level, opts.levelSet = l, true
	}
	// --unread narrows a listing that only `history` prints. On its own it is
	// the same as asking for the history; next to another command it is a
	// mistake worth reporting, not a flag to ignore.
	if *unread {
		if opts.explicit && opts.command != commandHistory {
			return options{}, errors.New(tr.T("--unread only makes sense with history"))
		}
		if err := opts.choose(commandHistory, tr); err != nil {
			return options{}, err
		}
		opts.unread = true
	}
	return opts, nil
}

// choose records the command to run and refuses to run two at once: `daemon`
// with `--version` is a mistake worth reporting, not something to guess about.
func (o *options) choose(c command, tr *i18n.Translator) error {
	switch c {
	case commandEnv, commandDaemon, commandVersion, commandHistory, commandConfig:
	default:
		return fmt.Errorf(tr.T("unknown command %q"), string(c))
	}
	if o.explicit && o.command != c {
		return fmt.Errorf(tr.T("cannot run %q and %q at once"), string(o.command), string(c))
	}
	o.command, o.explicit = c, true
	return nil
}

// parseLevel turns the name of a level into a slog level. The four names are the
// ones the configuration and the flag document.
func parseLevel(name string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// runEnv prints what the program detected about the session. It is the default
// command and it does not touch the network.
func runEnv(env platform.Env, tr *i18n.Translator, stdout io.Writer) int {
	tool := env.DialogTool
	if tool == "" {
		tool = tr.T("no dialog tool found")
	}

	fmt.Fprintf(stdout, "ntfy-desktop %s\n", version.Version)
	fmt.Fprintf(stdout, "  %-10s %s\n", tr.T("user"), env.User)
	fmt.Fprintf(stdout, "  %-10s %s\n", tr.T("locale"), env.Locale)
	fmt.Fprintf(stdout, "  %-10s %s\n", tr.T("desktop"), env.Desktop)
	fmt.Fprintf(stdout, "  %-10s %s\n", tr.T("dialogs"), tool)
	return 0
}

// newLogger builds the logger of the program: text to stderr, which is where
// systemd reads it from, at the level it was asked for.
func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// reportConfigError writes why the configuration cannot be used, with the part
// that came from the catalogues translated and the technical keys as they are
// written in the file.
func reportConfigError(log *slog.Logger, tr *i18n.Translator, path string, err error) {
	var invalid *config.ValidationError
	if errors.As(err, &invalid) {
		log.Error("the configuration is not usable",
			"path", path, "key", invalid.Field, "reason", tr.T(invalid.Msg, invalid.Args...))
		return
	}
	var syntax *config.SyntaxError
	if errors.As(err, &syntax) {
		log.Error("the configuration file could not be read",
			"path", path, "line", syntax.Line, "key", syntax.Key, "reason", tr.T(syntax.Msg))
		return
	}
	if errors.Is(err, fs.ErrNotExist) {
		log.Error("there is no configuration file", "path", path)
		return
	}
	log.Error("the configuration could not be loaded", "path", path, "error", err)
}
