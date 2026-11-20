// SPDX-License-Identifier: GPL-3.0-or-later

package notify

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// openable reports whether the link is one this program hands to the desktop.
//
// The link arrives from the network, so only http and https pass: any other
// scheme would let the sender choose the program that runs.
func openable(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

// linkFor returns the link worth remembering for a click, or an empty string
// when there is nothing that can be opened.
func linkFor(raw string, o opener) string {
	if o == nil || !openable(raw) {
		return ""
	}
	return strings.TrimSpace(raw)
}

// xdgOpen hands a URL to whatever the session has registered for it.
type xdgOpen struct{ path string }

// Open runs the desktop's URL handler and waits for it to start.
func (o xdgOpen) Open(ctx context.Context, rawURL string) error {
	if err := exec.CommandContext(ctx, o.path, rawURL).Run(); err != nil {
		return fmt.Errorf("running %s: %w", o.path, err)
	}
	return nil
}
