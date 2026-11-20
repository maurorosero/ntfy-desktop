// SPDX-License-Identifier: GPL-3.0-or-later

package notify

import (
	"strings"
	"time"

	dbusnotify "github.com/esiqveland/notify"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

const (
	// lowTimeout, normalTimeout and criticalTimeout are how long the bubble
	// stays on screen. None of them is zero: Plasma uses the value as the
	// interval of its own timer and closes the notification at once when it is
	// zero, so an explicit lifetime is the only way to keep the bubble visible.
	lowTimeout      = 30 * time.Second
	normalTimeout   = 45 * time.Second
	criticalTimeout = 60 * time.Second
)

// compose turns a message into the notification the service will draw.
func compose(m ntfy.Message) dbusnotify.Notification {
	note := dbusnotify.Notification{
		AppName:       appName,
		AppIcon:       appIcon,
		Summary:       summary(m),
		Body:          m.Message,
		ExpireTimeout: timeoutFor(m.Priority),
	}
	note.SetUrgency(urgencyFor(m.Priority))
	return note
}

// summary is the headline of the notification: the message title, the topic
// when the sender gave no title, and the program name when there is neither.
func summary(m ntfy.Message) string {
	if title := strings.TrimSpace(m.Title); title != "" {
		return title
	}
	if topic := strings.TrimSpace(m.Topic); topic != "" {
		return topic
	}
	return appName
}

// urgencyFor maps the ntfy priority to the freedesktop urgency. ntfy numbers
// them 1 (min) to 5 (max) with 3 as the default; an unset or nonsensical value
// is treated as the default, and anything above the range as the loudest.
func urgencyFor(priority int) dbusnotify.Urgency {
	switch {
	case priority <= 0:
		return dbusnotify.UrgencyNormal
	case priority <= 2:
		return dbusnotify.UrgencyLow
	case priority == 3:
		return dbusnotify.UrgencyNormal
	default:
		return dbusnotify.UrgencyCritical
	}
}

// timeoutFor is how long the notification of a priority stays. Critical
// messages hold the longest and low ones the shortest, always between the
// explicit bounds of the design, never zero.
func timeoutFor(priority int) time.Duration {
	switch urgencyFor(priority) {
	case dbusnotify.UrgencyLow:
		return lowTimeout
	case dbusnotify.UrgencyCritical:
		return criticalTimeout
	default:
		return normalTimeout
	}
}
