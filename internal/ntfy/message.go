// SPDX-License-Identifier: GPL-3.0-or-later

// Package ntfy speaks the ntfy subscription API: it reads the topic's JSON
// stream, authenticates with HTTP Basic when the configuration has a pair of
// credentials, and reconnects with a growing wait, asking the server for
// everything after the last message it saw.
//
// Every event of the stream is delivered as a Message, including "open" and
// "keepalive", because the caller is the one that decides what deserves a
// notification. The message content is never translated: it is what the sender
// wrote.
package ntfy

// Events the server sends on the subscription stream.
const (
	// EventOpen is the first event of every connection: it says the stream is
	// live and carries the topics being followed.
	EventOpen = "open"
	// EventKeepalive arrives when nothing else does, so an idle stream stays
	// alive through proxies.
	EventKeepalive = "keepalive"
	// EventMessage is a real message: the only one with body and title.
	EventMessage = "message"
	// EventPollRequest answers a polling subscription; it is not used yet.
	EventPollRequest = "poll_request"
)

// Message is one line of the JSON stream, as documented in
// https://docs.ntfy.sh/subscribe/api/#json-message-format
type Message struct {
	ID         string      `json:"id"`   // message identifier, used as the resume point
	Time       int64       `json:"time"` // Unix time in seconds
	Event      string      `json:"event"`
	Topic      string      `json:"topic"`
	Message    string      `json:"message"`
	Title      string      `json:"title"`
	Priority   int         `json:"priority"` // 1 (min) to 5 (max), 3 by default
	Tags       []string    `json:"tags"`
	Click      string      `json:"click"`
	Actions    []Action    `json:"actions"`
	Attachment *Attachment `json:"attachment"`
}

// IsMessage reports whether the event carries a real message, as opposed to a
// keepalive or an "open" event.
func (m Message) IsMessage() bool {
	return m.Event == EventMessage
}

// Action is an action button: the client shows it, the desktop decides.
type Action struct {
	Action  string            `json:"action"` // "view", "http", "broadcast" or "copy"
	Label   string            `json:"label"`
	URL     string            `json:"url"`
	Method  string            `json:"method"` // "view" and "http"; POST when empty
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Clear   bool              `json:"clear"` // dismiss the notification once it ran
}

// Attachment describes a file the message carries. Downloading it is not part
// of this package: it is the URL what travels here.
type Attachment struct {
	Name    string `json:"name"`
	Type    string `json:"type"`    // MIME type, when the server uploaded the file
	Size    int64  `json:"size"`    // bytes, when the server uploaded the file
	Expires int64  `json:"expires"` // Unix time in seconds, 0 if it never expires
	URL     string `json:"url"`
}
