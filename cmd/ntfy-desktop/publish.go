// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// maxBody is the largest message read from stdin. ntfy publishes a few
// kilobytes as the notification body and turns longer ones into attachments,
// which this client does not send yet: a megabyte is already far past that and
// keeps a mistyped redirection from filling the memory of the process.
const maxBody = 1 << 20

// publishOptions is what the publish command line asked for.
type publishOptions struct {
	topic    string // "" leaves the topic of the configuration
	title    string
	priority int // 0 leaves the server default
	tags     []string
	click    string
	markdown bool
}

// priorities are the names ntfy documents for its five levels, so a person can
// write `--priority high` instead of remembering the number.
var priorities = map[string]int{
	"min":     1,
	"low":     2,
	"default": 3,
	"high":    4,
	"max":     5,
	"urgent":  5,
}

// splitPublish recognises the command lines that ask for the publish command:
// the subcommand, and the --publish flag that carries its topic, which is the
// form the issue documents.
func splitPublish(args []string) (rest []string, ok bool) {
	if len(args) == 0 {
		return nil, false
	}
	switch {
	case args[0] == "publish":
		return args[1:], true
	case args[0] == "--publish":
		// --publish <topic> is the same as publish --topic <topic>.
		return append([]string{"--topic"}, args[1:]...), true
	case strings.HasPrefix(args[0], "--publish="):
		return append([]string{"--topic=" + strings.TrimPrefix(args[0], "--publish=")}, args[1:]...), true
	}
	return nil, false
}

// parsePublish reads the flags of the publish command into opts.publish. The
// body is never part of the command line: it comes from stdin, so a message
// that looks like a flag is a message.
func (o *options) parsePublish(args []string, tr *i18n.Translator, stderr io.Writer) error {
	var p publishOptions

	// The flag package stops at the first argument that is not a flag, so the
	// topic is taken out before parsing: `publish casa --title Prueba` reads
	// the way it looks.
	var positional []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional = append(positional, args[0])
		args = args[1:]
	}

	fs := flag.NewFlagSet("ntfy-desktop publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, tr.T("usage: ntfy-desktop publish [topic] [flags]"))
		fs.PrintDefaults()
	}
	fs.StringVar(&p.topic, "topic", "", tr.T("topic to publish to (default: the one in the configuration)"))
	fs.StringVar(&p.title, "title", "", tr.T("message title"))
	priority := fs.String("priority", "", tr.T("priority from 1 to 5, or min, low, default, high or max"))
	tags := fs.String("tags", "", tr.T("tags, separated by commas"))
	fs.StringVar(&p.click, "click", "", tr.T("url to open when the notification is clicked"))
	fs.BoolVar(&p.markdown, "markdown", false, tr.T("render the message as Markdown"))

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return errUsage
	}

	positional = append(positional, fs.Args()...)
	switch {
	case len(positional) > 1:
		return fmt.Errorf(tr.T("unexpected argument %q"), positional[1])
	case len(positional) == 1 && p.topic != "":
		return errors.New(tr.T("the topic is given both as an argument and with --topic"))
	case len(positional) == 1:
		p.topic = positional[0]
	}

	if *priority != "" {
		n, ok := parsePriority(*priority)
		if !ok {
			return fmt.Errorf(tr.T("invalid priority %q: use 1 to 5, min, low, default, high or max"), *priority)
		}
		p.priority = n
	}
	p.tags = splitTags(*tags)

	o.publish = p
	return nil
}

// parsePriority turns a number or a documented name into the number ntfy takes.
func parsePriority(value string) (int, bool) {
	name := strings.ToLower(strings.TrimSpace(value))
	if n, ok := priorities[name]; ok {
		return n, true
	}
	n, err := strconv.Atoi(name)
	if err != nil || n < 1 || n > 5 {
		return 0, false
	}
	return n, true
}

// splitTags turns the comma separated flag into the list ntfy takes, dropping
// the spaces around each tag and the empty pieces.
func splitTags(value string) []string {
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// runPublish sends one message and prints the id the server assigned to it, so
// a script can keep it. The topic is the argument or the flag, and the topic of
// the configuration when neither is given.
func runPublish(ctx context.Context, opts options, tr *i18n.Translator, stdin io.Reader, stdout, stderr io.Writer) int {
	path, err := config.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg, err := config.Load(path)
	if err != nil {
		reportConfigError(newLogger(stderr, opts.level), tr, path, err)
		return 1
	}
	// Publishing to another topic is the normal case, so the command line wins
	// over the file. The credentials and the server are the configured ones:
	// that is what makes both authentication modes work the same as the daemon.
	if opts.publish.topic != "" {
		cfg.Topic = opts.publish.topic
	}

	body, err := readBody(stdin, tr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	msg, err := ntfy.New(*cfg).Publish(ctx, ntfy.Publish{
		Body:     body,
		Title:    opts.publish.title,
		Priority: opts.publish.priority,
		Tags:     opts.publish.tags,
		Click:    opts.publish.click,
		Markdown: opts.publish.markdown,
	})
	if err != nil {
		fmt.Fprintln(stderr, publishFailure(tr, cfg.Topic, err))
		return 1
	}
	if msg.ID == "" {
		fmt.Fprintln(stderr, tr.T("the server did not return a message id"))
		return 1
	}
	fmt.Fprintln(stdout, msg.ID)
	return 0
}

// readBody reads the message from stdin. One trailing newline is dropped
// because that is what a shell pipe adds, not part of the message; every other
// byte is kept, including the ones that look like flags.
func readBody(r io.Reader, tr *i18n.Translator) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return "", fmt.Errorf("%s: %w", tr.T("reading the message from stdin"), err)
	}
	if len(data) > maxBody {
		return "", fmt.Errorf(tr.T("the message is larger than %d bytes"), maxBody)
	}
	body := strings.TrimSuffix(string(data), "\n")
	body = strings.TrimSuffix(body, "\r")
	if strings.TrimSpace(body) == "" {
		return "", errors.New(tr.T("the message body is empty: write it to stdin"))
	}
	return body, nil
}

// publishFailure turns the error of the client into what the person at the
// terminal needs: a refused publish says what to look at, and every other
// failure keeps the status or the network reason.
func publishFailure(tr *i18n.Translator, topic string, err error) error {
	var status *ntfy.StatusError
	if errors.As(err, &status) && status.Code == http.StatusForbidden {
		return fmt.Errorf("%s (%s)",
			tr.T("the server refused to publish to %s: the topic may be read-only, or the credentials may not be allowed to write to it", topic),
			status)
	}
	return fmt.Errorf("%s: %w", tr.T("publishing to %s failed", topic), err)
}
