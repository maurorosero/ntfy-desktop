// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/history"
	"github.com/maurorosero/ntfy-desktop/internal/i18n"
)

const (
	// lineWidth is how much of the body a line shows before it is cut, so one
	// long message cannot flood the terminal.
	lineWidth = 80
	// historyTime is the layout of the date and time the listing prints, in the
	// clock of whoever runs it.
	historyTime = "2006-01-02 15:04"
)

// runHistory prints what arrived while nobody was looking: one line per stored
// message with the date, the topic, the title and the first line of the body.
func runHistory(opts options, tr *i18n.Translator, stdout, stderr io.Writer) int {
	dir, err := history.DefaultDir()
	if err != nil {
		newLogger(stderr, opts.level).Error("cannot locate the history", "error", err)
		return 1
	}

	store, err := history.Open(dir, history.DefaultLimit)
	if err != nil {
		newLogger(stderr, opts.level).Error("the history cannot be used", "path", dir, "error", err)
		return 1
	}
	// A file that is not a history is already out of the way; say so and go on
	// with what is left, because refusing to list anything would be worse.
	for _, warning := range store.Warnings() {
		newLogger(stderr, opts.level).Warn("a stored history was discarded", "error", warning)
	}

	entries := store.List(history.Filter{Unread: opts.unread})
	if len(entries) == 0 {
		if opts.unread {
			fmt.Fprintln(stdout, tr.T("no unread messages"))
			return 0
		}
		fmt.Fprintln(stdout, tr.T("the history is empty"))
		return 0
	}
	for _, e := range entries {
		fmt.Fprintln(stdout, entryLine(e, tr))
	}
	return 0
}

// entryLine renders one stored message. A star marks what has not been read;
// a space keeps the columns of the rest in place.
func entryLine(e history.Entry, tr *i18n.Translator) string {
	mark := " "
	if !e.Read {
		mark = "*"
	}

	title := strings.TrimSpace(e.Title)
	if title == "" {
		title = tr.T("(no title)")
	}

	line := fmt.Sprintf("%s %s  %s  %s", mark, e.Time.Local().Format(historyTime), e.Topic, title)
	if first := firstLine(e.Body); first != "" {
		line += " — " + first
	}
	return line
}

// firstLine is what the listing shows of the body: its first line, with the
// runs of spaces a sender left in it collapsed.
func firstLine(body string) string {
	body = strings.TrimSpace(body)
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	body = strings.Join(strings.Fields(body), " ")
	if r := []rune(body); len(r) > lineWidth {
		return string(r[:lineWidth]) + "…"
	}
	return body
}
