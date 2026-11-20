// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"strings"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// maxPrompt is how much of the sender's text a dialog shows. A long body turns
// the dialog into a window taller than the screen, and the decision does not
// get better with it.
const maxPrompt = 400

// markup escapes the three characters both renderers read as markup: Qt's rich
// text in kdialog and Pango's in zenity. The message comes from whoever can
// publish to the topic, so its tags are text, never formatting.
var markup = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// promptText is the text above the list: the sender's title in bold and the
// body under it, clipped. kdialog and zenity break lines differently, so the
// break tag comes from the adapter.
func promptText(tr Translator, m ntfy.Message, br string) string {
	heading := clip(strings.TrimSpace(m.Title), maxPrompt)
	body := clip(strings.TrimSpace(m.Message), maxPrompt)
	if heading == "" && body == "" {
		return markup.Replace(tr.T("Choose an action"))
	}

	lines := make([]string, 0, 2)
	if heading != "" {
		lines = append(lines, "<b>"+markup.Replace(heading)+"</b>")
	}
	if body != "" {
		// Escape first, then break the lines: the break tag is ours, not the
		// sender's, so it must not be escaped with the rest.
		lines = append(lines, strings.ReplaceAll(markup.Replace(body), "\n", br))
	}
	return strings.Join(lines, br)
}

// clip keeps the first n runes and marks that something was left out.
func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
