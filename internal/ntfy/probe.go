// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

const (
	// probeTimeout bounds each check on its own: a server that does not answer
	// in three seconds is a failed step, not a program that waits.
	probeTimeout = 3 * time.Second
	// pathHealth and pathAccount are the two routes of the ntfy API a
	// verification reads before it touches a topic.
	pathHealth  = "v1/health"
	pathAccount = "v1/account"
	// answerLimit is how much of a small answer is read. The two routes send
	// one short JSON object; a bigger body is not one of them.
	answerLimit = 8 << 10
	// snippetLimit is how much of a strange answer travels in the error:
	// enough to see what answered, short enough for one line.
	snippetLimit = 160
)

// The named reasons a check can fail. Each one is wrapped with the detail that
// produced it and read back with errors.Is, so a caller maps a code without
// looking at a string.
var (
	// ErrNotNtfy is a server that does not answer /v1/health the way ntfy
	// does: the URL points at something else.
	ErrNotNtfy = errors.New("not an ntfy server")
	// ErrCredentials is a 401: the server did not accept the pair, or it asks
	// for one and the configuration has none.
	ErrCredentials = errors.New("credentials rejected")
	// ErrNoPermission is a 403: the credentials are accepted, the topic is not.
	// It is never confused with a server that is down, which is unverifiable.
	ErrNoPermission = errors.New("no permission for the topic")
)

// Step names one of the three verifications.
type Step int

const (
	// StepHealth asks whether the server is ntfy: GET /v1/health.
	StepHealth Step = iota
	// StepAccount asks whether the credentials are accepted: GET /v1/account.
	StepAccount
	// StepRead asks whether a topic can be read: the JSON stream is opened and
	// cut on its first event.
	StepRead
)

// String names the step, for messages and logs.
func (s Step) String() string {
	switch s {
	case StepHealth:
		return "health"
	case StepAccount:
		return "account"
	case StepRead:
		return "read"
	}
	return "unknown"
}

// State is what one check concluded. The values are ordered from the best
// outcome to the worst, so a report keeps the worst of its checks.
type State int

const (
	// StateOK is a check that ran and whose answer was accepted.
	StateOK State = iota
	// StateUnverifiable is a check that reached no conclusion: no network, no
	// answer inside the timeout, a route this server does not have. The
	// configuration can still be saved, with the reason shown as a warning.
	StateUnverifiable
	// StateFailed is an answer that says no: the server is not ntfy, it
	// refused the credentials, or it refused the topic.
	StateFailed
)

// String names the state, for messages and logs.
func (s State) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateUnverifiable:
		return "unverifiable"
	case StateFailed:
		return "failed"
	}
	return "unknown"
}

// RoleAnonymous is the role ntfy gives the request that carries no credentials.
// A server that answers it does not ask for a pair, so the configured one was
// never exercised.
const RoleAnonymous = "anonymous"

// Account is what /v1/account answered, reduced to what a verification can say:
// who the credentials are and what role the server gave them.
type Account struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Result is the outcome of one check. State is the conclusion and Err is the
// reason, which is set whenever the state is not StateOK. Topic is filled only
// by StepRead, and Account only by StepAccount when the server answered.
type Result struct {
	Step    Step
	Topic   string
	State   State
	Err     error
	Account Account
}

// OK reports whether the check concluded and the answer was accepted.
func (r Result) OK() bool {
	return r.State == StateOK
}

// Report is the outcome of the three checks over one configuration.
type Report struct {
	Health  Result
	Account Result
	Read    []Result // one per configured topic, in the order the configuration names them
}

// Results returns every result in the order the checks run.
func (r Report) Results() []Result {
	out := make([]Result, 0, 2+len(r.Read))
	out = append(out, r.Health, r.Account)
	return append(out, r.Read...)
}

// State is the worst state of the report: StateOK when every check passed,
// StateUnverifiable while none was refused, and StateFailed as soon as one was.
// It is the one bit the caller needs to decide between saving and saving with a
// warning.
func (r Report) State() State {
	worst := StateOK
	for _, res := range r.Results() {
		if res.State > worst {
			worst = res.State
		}
	}
	return worst
}

// Probe verifies a configuration against its server before anything is saved.
//
// The three checks are independent and each one has its own short timeout, so a
// caller can run them one by one and show the progress, or run them all with
// Run. None of them writes anything on the server, and none of them touches the
// message cache beyond the first event of a stream.
//
// What a probe does not say, because the client cannot see it:
//
//   - Whether a topic "exists". In ntfy a topic exists once something is
//     published; an empty one answers like any other, so what is verified is
//     the permission, not the existence.
//   - Whether the user may publish. Proving it would leave a real message in
//     the topic, so the role and the limits of /v1/account are reported instead.
//   - What the server's ACL allows. The client only sees the 401 or the 403.
type Probe struct {
	cfg     config.Config
	http    *http.Client
	timeout time.Duration
}

// NewProbe builds the probe for a configuration. The server URL and the
// credentials are the parts it uses; the topics are checked by StepRead.
func NewProbe(cfg config.Config) *Probe {
	return newProbe(cfg, probeTimeout)
}

// newProbe exists so a test can shrink the timeout of a check that has to
// expire: the policy is the same, only the wait changes.
func newProbe(cfg config.Config, timeout time.Duration) *Probe {
	return &Probe{
		cfg:     cfg,
		timeout: timeout,
		http: &http.Client{
			// No Client.Timeout: every request carries its own context, so the
			// deadline is per check and not per client.
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
				TLSHandshakeTimeout: timeout,
				// A probe makes a handful of requests and then goes away. A
				// pooled connection buys nothing, and the read check has to end
				// with the connection closed instead of back in a pool.
				DisableKeepAlives: true,
			},
		},
	}
}

// Run performs the three checks, in order, and returns what each one said. One
// failing check does not skip the next ones: the caller gets the whole picture
// —bad server and bad credentials at once— instead of one reason per run.
func (p *Probe) Run(ctx context.Context) Report {
	report := Report{
		Health:  p.Health(ctx),
		Account: p.Account(ctx),
	}
	report.Read = make([]Result, 0, len(p.cfg.Topics))
	for _, topic := range p.cfg.Topics {
		report.Read = append(report.Read, p.Read(ctx, topic))
	}
	return report
}

// Health reports whether the server is ntfy.
//
// The signature is the answer of GET /v1/health: 200 with a JSON object that
// carries the "healthy" field. The field is looked for, not its value, because a
// server that says false is still ntfy, only unhealthy. HEAD is never used:
// ntfy answers 404 to it, which would read like a server without the route.
func (p *Probe) Health(ctx context.Context) Result {
	res := Result{Step: StepHealth}
	endpoint, err := p.apiEndpoint(pathHealth)
	if err != nil {
		return res.unverifiable(err)
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	resp, body, err := p.get(ctx, endpoint)
	if err != nil {
		return res.unverifiable(fmt.Errorf("asking %s for its health: %w", p.cfg.ServerURL, err))
	}

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		// The object is read below.
	case code == http.StatusUnauthorized || code == http.StatusForbidden,
		code == http.StatusTooManyRequests, code >= 500:
		// The server is there but does not answer the route usefully: a proxy
		// or an instance in the middle of a restart. Nothing is concluded.
		return res.unverifiable(fmt.Errorf("%s answered %s to /%s", p.cfg.ServerURL, resp.Status, pathHealth))
	default:
		return res.failed(fmt.Errorf("%s answered %s to /%s: %w", p.cfg.ServerURL, resp.Status, pathHealth, ErrNotNtfy))
	}

	var health struct {
		Healthy *bool `json:"healthy"`
	}
	if err := json.Unmarshal(body, &health); err != nil || health.Healthy == nil {
		return res.failed(fmt.Errorf(`%s answered "%s" to /%s, which is not ntfy's health object: %w`,
			p.cfg.ServerURL, snippet(body), pathHealth, ErrNotNtfy))
	}
	return res.ok()
}

// Account reports whether the credentials of the configuration are accepted.
//
// The answer is the one of GET /v1/account: 200 means the pair works, 401 means
// it was rejected, and 200 with role "anonymous" means the server does not ask
// for credentials, so the configured pair was never exercised. A 404 is a
// server without that route —an older instance— and leaves the check
// unverifiable instead of blocking the save.
func (p *Probe) Account(ctx context.Context) Result {
	res := Result{Step: StepAccount}
	endpoint, err := p.apiEndpoint(pathAccount)
	if err != nil {
		return res.unverifiable(err)
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	resp, body, err := p.get(ctx, endpoint)
	if err != nil {
		return res.unverifiable(fmt.Errorf("asking %s for the account: %w", p.cfg.ServerURL, err))
	}

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		// The object is read below.
	case code == http.StatusUnauthorized:
		if p.cfg.AuthMode() != config.AuthBasic {
			return res.failed(fmt.Errorf("%s asks for credentials and the configuration has none: %w", p.cfg.ServerURL, ErrCredentials))
		}
		return res.failed(fmt.Errorf("%s rejected the configured credentials: %w", p.cfg.ServerURL, ErrCredentials))
	case code == http.StatusForbidden:
		return res.failed(fmt.Errorf("%s refused the account route: %w", p.cfg.ServerURL, ErrNoPermission))
	case code == http.StatusNotFound:
		return res.unverifiable(fmt.Errorf("%s has no /%s route: a server older than the one that documents it", p.cfg.ServerURL, pathAccount))
	default:
		return res.unverifiable(fmt.Errorf("%s answered %s to /%s", p.cfg.ServerURL, resp.Status, pathAccount))
	}

	var account Account
	if err := json.Unmarshal(body, &account); err != nil {
		return res.unverifiable(fmt.Errorf("reading the account answer of %s: %w", p.cfg.ServerURL, err))
	}
	if account.Role == "" {
		return res.unverifiable(fmt.Errorf(`%s answered "%s" to /%s: no role to read`, p.cfg.ServerURL, snippet(body), pathAccount))
	}
	res.Account = account
	return res.ok()
}

// Read reports whether topic can be read.
//
// The check opens the JSON stream of the topic and cuts it as soon as the
// "open" event arrives. That event is the first one of a live subscription and
// it is enough: the connection is closed without reading a single message, so
// the cache is not consumed and nothing is published. A poll (poll=1) is not
// used because it does not send "open" at all, and without that event there is
// nothing to tell a working stream from a silent one.
//
// What it does not say: whether the topic "exists" (in ntfy a topic exists once
// something is published, and an empty one answers the same), whether the user
// may publish (that would leave a message), or what the server's ACL allows
// (only the 401 and the 403 are visible from here).
func (p *Probe) Read(ctx context.Context, topic string) Result {
	res := Result{Step: StepRead, Topic: topic}
	if topic == "" {
		return res.unverifiable(errors.New("a read check needs one topic name"))
	}
	endpoint, err := p.endpoint(topic, pathJSON)
	if err != nil {
		return res.unverifiable(err)
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := p.request(ctx, endpoint)
	if err != nil {
		return res.unverifiable(err)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return res.unverifiable(fmt.Errorf("opening the stream of %q: %w", topic, err))
	}
	// Closing a stream that was not read to the end closes the connection
	// instead of returning it to a pool: that is the cut this check wants, and
	// the reason it stops at the first event.
	defer resp.Body.Close()

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		// The stream is read below.
	case code == http.StatusUnauthorized:
		return res.failed(fmt.Errorf("%q: the server rejected the credentials: %w", topic, ErrCredentials))
	case code == http.StatusForbidden:
		return res.failed(fmt.Errorf("%q: the server refused the subscription: %w", topic, ErrNoPermission))
	case code == http.StatusNotFound:
		return res.unverifiable(fmt.Errorf("%q: %s answered %s to the stream route", topic, p.cfg.ServerURL, resp.Status))
	default:
		return res.unverifiable(fmt.Errorf("%q: %s answered %s", topic, p.cfg.ServerURL, resp.Status))
	}

	if err := waitForOpen(resp.Body); err != nil {
		if ctx.Err() != nil {
			return res.unverifiable(fmt.Errorf("%q sent no %q event within %s", topic, EventOpen, p.timeout))
		}
		return res.unverifiable(fmt.Errorf("%q: %w", topic, err))
	}
	return res.ok()
}

// get performs a GET bounded by ctx and returns the answer with at most
// answerLimit bytes of its body. The body comes already read and closed: only
// the status is left to look at.
func (p *Probe) get(ctx context.Context, endpoint string) (*http.Response, []byte, error) {
	req, err := p.request(ctx, endpoint)
	if err != nil {
		return nil, nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	if err != nil {
		return nil, nil, fmt.Errorf("reading the answer of %s: %w", endpoint, err)
	}
	return resp, body, nil
}

// request builds one GET. The credentials travel as HTTP Basic only when the
// configuration has a pair: sending an empty one would be a different request,
// and the server would answer it as an anonymous one anyway.
func (p *Probe) request(ctx context.Context, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building the request for %s: %w", endpoint, err)
	}
	req.Header.Set("Accept", "application/json")
	if p.cfg.AuthMode() == config.AuthBasic {
		req.SetBasicAuth(p.cfg.User, p.cfg.Password)
	}
	return req, nil
}

// apiEndpoint builds the URL of one route of the API, splitting the path into
// the segments endpoint escapes: "/v1/health" is two segments and never one
// with an escaped slash.
func (p *Probe) apiEndpoint(path string) (string, error) {
	return p.endpoint(strings.Split(path, "/")...)
}

// endpoint builds <server>/<segments...>, keeping the path prefix of the server
// and escaping each segment on its own, so a name with a slash or a comma stays
// one segment.
func (p *Probe) endpoint(segments ...string) (string, error) {
	u, err := url.Parse(p.cfg.ServerURL)
	if err != nil {
		return "", fmt.Errorf("parsing the server url: %w", err)
	}
	base := strings.TrimSuffix(u.Path, "/")
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	u.Path = base + "/" + strings.Join(segments, "/")
	// RawPath keeps the escaping: Path alone would turn %2F into a separator.
	u.RawPath = (&url.URL{Path: base}).EscapedPath() + "/" + strings.Join(escaped, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// waitForOpen reads events until the "open" one arrives. Anything else —a
// keepalive, or a message that a strange server sent first— is discarded, which
// is what the caller wants: the point is the event, not the content.
func waitForOpen(r io.Reader) error {
	dec := json.NewDecoder(r)
	for {
		var event struct {
			Event string `json:"event"`
		}
		if err := dec.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("the stream ended before the open event")
			}
			return fmt.Errorf("reading the stream: %w", err)
		}
		if event.Event == EventOpen {
			return nil
		}
	}
}

// snippet reduces an answer to one short line: enough to see what answered,
// without dumping a page into an error message. It cuts on runes so a multibyte
// character is never left half written.
func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if runes := []rune(s); len(runes) > snippetLimit {
		s = string(runes[:snippetLimit]) + "…"
	}
	return s
}

// ok marks the result as accepted.
func (r Result) ok() Result {
	r.State = StateOK
	return r
}

// failed marks the result as a definite no.
func (r Result) failed(err error) Result {
	r.State = StateFailed
	r.Err = err
	return r
}

// unverifiable marks the result as one that reached no conclusion.
func (r Result) unverifiable(err error) Result {
	r.State = StateUnverifiable
	r.Err = err
	return r
}
