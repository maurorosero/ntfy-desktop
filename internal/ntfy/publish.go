// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

const (
	// publishTimeout bounds one publish. Unlike the subscription, this is a
	// single request, so a server that never answers must not hang the command.
	publishTimeout = 30 * time.Second
	// publishAnswerLimit is how much of the answer is read: the server sends
	// one small JSON object, and anything bigger is not a message it published.
	publishAnswerLimit = 64 << 10
	// publishContentType is what the body travels as. Markdown is asked for
	// with a parameter, not with a different content type, so the body stays
	// readable on a server that ignores it.
	publishContentType = "text/plain; charset=utf-8"
)

// Publish is the message to send to the topic of the configuration. Priority is
// 0 when the server default is wanted and Markdown is off unless it is asked
// for, so the zero value publishes a body and nothing else.
type Publish struct {
	Body     string
	Title    string
	Priority int // 1 (min) to 5 (max), as ntfy numbers them
	Tags     []string
	Click    string // URL the desktop opens when the notification is clicked
	Markdown bool   // render the body as Markdown
}

// Publish sends one message and returns the answer of the server, whose ID is
// the one that names the message from then on. A rejected request is reported
// as a StatusError, which carries the status and never the credentials.
func (c *Client) Publish(ctx context.Context, p Publish) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	req, err := c.publishRequest(ctx, p)
	if err != nil {
		return Message{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("publishing to %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Message{}, &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}

	var m Message
	if err := json.NewDecoder(io.LimitReader(resp.Body, publishAnswerLimit)).Decode(&m); err != nil {
		return Message{}, fmt.Errorf("reading the publish answer: %w", err)
	}
	return m, nil
}

// publishRequest builds the POST that sends p to the configured topic.
//
// The fields travel as query parameters, which ntfy documents next to the
// headers: a UTF-8 title is escaped there instead of depending on how the
// server reads the header bytes, and no value typed on the command line can
// inject a header. The body is the message exactly as it was read.
func (c *Client) publishRequest(ctx context.Context, p Publish) (*http.Request, error) {
	endpoint, err := c.publishEndpoint()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing the publish url: %w", err)
	}

	q := u.Query()
	if p.Title != "" {
		q.Set("title", p.Title)
	}
	if p.Priority != 0 {
		q.Set("priority", strconv.Itoa(p.Priority))
	}
	if len(p.Tags) > 0 {
		q.Set("tags", strings.Join(p.Tags, ","))
	}
	if p.Click != "" {
		q.Set("click", p.Click)
	}
	if p.Markdown {
		q.Set("markdown", "yes")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(p.Body))
	if err != nil {
		return nil, fmt.Errorf("building the publish request: %w", err)
	}
	req.Header.Set("Content-Type", publishContentType)
	if c.cfg.AuthMode() == config.AuthBasic {
		req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	}
	return req, nil
}

// publishEndpoint builds the publish URL: <server>/<topic>. It is the same path
// the subscription uses, without the /json suffix, and it needs exactly one
// topic: publishing to a list is not something ntfy defines.
func (c *Client) publishEndpoint() (string, error) {
	topic := c.cfg.Topic()
	if topic == "" {
		return "", errors.New("a publish needs one topic and this configuration names several")
	}
	u, err := url.Parse(c.cfg.ServerURL)
	if err != nil {
		return "", fmt.Errorf("parsing the server url: %w", err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + url.PathEscape(topic)
	u.RawPath = "" // let url rebuild the escaped path
	return u.String(), nil
}
