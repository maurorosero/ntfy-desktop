// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

// serve starts a server that answers every request with the same status and
// body, and returns its URL. Nothing here reaches the network: httptest listens
// on the loopback interface of the test process.
func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestProbeEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		server   string
		segments []string
		want     string
	}{
		{
			name:     "public server",
			server:   "https://ntfy.sh",
			segments: []string{"v1", "health"},
			want:     "https://ntfy.sh/v1/health",
		},
		{
			name:     "trailing slash in the server",
			server:   "https://ntfy.sh/",
			segments: []string{"v1", "account"},
			want:     "https://ntfy.sh/v1/account",
		},
		{
			name:     "server with a path prefix",
			server:   "https://example.org/ntfy",
			segments: []string{"alerts", pathJSON},
			want:     "https://example.org/ntfy/alerts/json",
		},
		{
			name:     "local server with a port",
			server:   "http://localhost:2586",
			segments: []string{"a_b-1", pathJSON},
			want:     "http://localhost:2586/a_b-1/json",
		},
		{
			name:     "a topic that needs escaping",
			server:   "https://ntfy.sh",
			segments: []string{"a b#c", pathJSON},
			want:     "https://ntfy.sh/a%20b%23c/json",
		},
		{
			// A slash is escaped, not left as a separator: a topic name never
			// becomes two path segments.
			name:     "a topic with a slash stays one segment",
			server:   "https://ntfy.sh",
			segments: []string{"a/b", pathJSON},
			want:     "https://ntfy.sh/a%2Fb/json",
		},
		{
			name:     "a query in the server is dropped",
			server:   "https://ntfy.sh?x=1#frag",
			segments: []string{"v1", "health"},
			want:     "https://ntfy.sh/v1/health",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProbe(config.Config{ServerURL: c.server}, time.Second)
			got, err := p.endpoint(c.segments...)
			if err != nil {
				t.Fatalf("endpoint(%q) with server %q = error %v; want no error", c.segments, c.server, err)
			}
			if got != c.want {
				t.Errorf("endpoint(%q) with server %q = %q; want %q", c.segments, c.server, got, c.want)
			}
		})
	}
}

// TestProbeAPIEndpoint is the one the checks use: a route path is split on its
// slashes, so "/v1/health" never becomes one escaped segment.
func TestProbeAPIEndpoint(t *testing.T) {
	cases := []struct {
		server string
		path   string
		want   string
	}{
		{server: "https://ntfy.sh", path: pathHealth, want: "https://ntfy.sh/v1/health"},
		{server: "https://ntfy.sh/", path: pathAccount, want: "https://ntfy.sh/v1/account"},
		{server: "https://example.org/ntfy", path: pathHealth, want: "https://example.org/ntfy/v1/health"},
	}

	for _, c := range cases {
		t.Run(c.server+c.path, func(t *testing.T) {
			p := newProbe(config.Config{ServerURL: c.server}, time.Second)
			got, err := p.apiEndpoint(c.path)
			if err != nil {
				t.Fatalf("apiEndpoint(%q) with server %q = error %v; want no error", c.path, c.server, err)
			}
			if got != c.want {
				t.Errorf("apiEndpoint(%q) with server %q = %q; want %q", c.path, c.server, got, c.want)
			}
		})
	}
}

func TestProbeHealth(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    State
		wantErr error
		wantIn  string // the error must name what answered
	}{
		{
			name:   "ntfy healthy",
			status: http.StatusOK,
			body:   `{"healthy":true,"role":"active"}`,
			want:   StateOK,
		},
		{
			name:   "ntfy unhealthy is still ntfy",
			status: http.StatusOK,
			body:   `{"healthy":false}`,
			want:   StateOK,
		},
		{
			name:    "200 with another object",
			status:  http.StatusOK,
			body:    `{"status":"ok","version":"1"}`,
			want:    StateFailed,
			wantErr: ErrNotNtfy,
			wantIn:  `{"status":"ok","version":"1"}`,
		},
		{
			name:    "200 with html",
			status:  http.StatusOK,
			body:    `<html><body>nginx</body></html>`,
			want:    StateFailed,
			wantErr: ErrNotNtfy,
			wantIn:  "nginx",
		},
		{
			name:    "route not there",
			status:  http.StatusNotFound,
			body:    `{"error":"not found"}`,
			want:    StateFailed,
			wantErr: ErrNotNtfy,
			wantIn:  "404",
		},
		{
			name:   "a proxy asks for a password",
			status: http.StatusUnauthorized,
			body:   ``,
			want:   StateUnverifiable,
			wantIn: "401",
		},
		{
			name:   "the server is restarting",
			status: http.StatusServiceUnavailable,
			body:   ``,
			want:   StateUnverifiable,
			wantIn: "503",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewProbe(config.Config{ServerURL: serve(t, c.status, c.body)})
			got := p.Health(context.Background())

			if got.Step != StepHealth {
				t.Errorf("Health().Step = %v; want %v", got.Step, StepHealth)
			}
			if got.State != c.want {
				t.Fatalf("Health() with status %d and body %q = %v (err %v); want %v",
					c.status, c.body, got.State, got.Err, c.want)
			}
			if c.wantErr != nil && !errors.Is(got.Err, c.wantErr) {
				t.Errorf("Health() error = %v; want it to wrap %v", got.Err, c.wantErr)
			}
			if c.wantIn != "" && (got.Err == nil || !strings.Contains(got.Err.Error(), c.wantIn)) {
				t.Errorf("Health() error = %v; want it to contain %q", got.Err, c.wantIn)
			}
		})
	}
}

func TestProbeHealthUsesGet(t *testing.T) {
	methods := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case methods <- r.Method:
		default:
		}
		fmt.Fprint(w, `{"healthy":true}`)
	}))
	defer srv.Close()

	if got := NewProbe(config.Config{ServerURL: srv.URL}).Health(context.Background()); !got.OK() {
		t.Fatalf("Health() = %v (err %v); want ok", got.State, got.Err)
	}
	// HEAD answers 404 on ntfy, so the check has to be a GET.
	if got := <-methods; got != http.MethodGet {
		t.Errorf("health request method = %q; want %q", got, http.MethodGet)
	}
}

func TestProbeAccount(t *testing.T) {
	// marker is what the tests put in the password field. It is a marker, not a
	// credential: no real server is involved, and the point of one of the cases
	// is that it never reaches a message the user reads.
	const marker = "dummy-value-for-tests"
	cases := []struct {
		name     string
		status   int
		body     string
		creds    bool
		want     State
		wantErr  error
		wantIn   string
		wantRole string
		wantUser string
	}{
		{
			name:     "accepted credentials",
			status:   http.StatusOK,
			body:     `{"username":"andrea","role":"user"}`,
			creds:    true,
			want:     StateOK,
			wantRole: "user",
			wantUser: "andrea",
		},
		{
			name:     "the server does not ask for credentials",
			status:   http.StatusOK,
			body:     `{"username":"*","role":"anonymous"}`,
			creds:    true,
			want:     StateOK,
			wantRole: RoleAnonymous,
			wantUser: "*",
		},
		{
			name:     "anonymous without credentials",
			status:   http.StatusOK,
			body:     `{"username":"*","role":"anonymous"}`,
			want:     StateOK,
			wantRole: RoleAnonymous,
			wantUser: "*",
		},
		{
			name:    "rejected credentials",
			status:  http.StatusUnauthorized,
			body:    `{"code":40101,"http":401,"error":"unauthorized"}`,
			creds:   true,
			want:    StateFailed,
			wantErr: ErrCredentials,
		},
		{
			name:    "the server asks for credentials and there are none",
			status:  http.StatusUnauthorized,
			body:    `{"code":40101,"http":401,"error":"unauthorized"}`,
			want:    StateFailed,
			wantErr: ErrCredentials,
		},
		{
			name:    "the account route is forbidden",
			status:  http.StatusForbidden,
			body:    `{"code":40301,"http":403,"error":"forbidden"}`,
			creds:   true,
			want:    StateFailed,
			wantErr: ErrNoPermission,
		},
		{
			name:   "an older server without the route",
			status: http.StatusNotFound,
			body:   `{"error":"not found"}`,
			creds:  true,
			want:   StateUnverifiable,
			wantIn: "no /v1/account route",
		},
		{
			name:   "a 200 that names no role",
			status: http.StatusOK,
			body:   `{"username":"andrea"}`,
			creds:  true,
			want:   StateUnverifiable,
			wantIn: "no role",
		},
		{
			name:   "a 200 that is not json",
			status: http.StatusOK,
			body:   `<html>hola</html>`,
			creds:  true,
			want:   StateUnverifiable,
			wantIn: "reading the account answer",
		},
		{
			name:   "the server is broken",
			status: http.StatusInternalServerError,
			body:   ``,
			want:   StateUnverifiable,
			wantIn: "500",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{ServerURL: serve(t, c.status, c.body)}
			if c.creds {
				cfg.User, cfg.Password = "andrea", marker
			}
			got := NewProbe(cfg).Account(context.Background())

			if got.State != c.want {
				t.Fatalf("Account() with status %d and body %q = %v (err %v); want %v",
					c.status, c.body, got.State, got.Err, c.want)
			}
			if c.wantErr != nil && !errors.Is(got.Err, c.wantErr) {
				t.Errorf("Account() error = %v; want it to wrap %v", got.Err, c.wantErr)
			}
			if c.wantIn != "" && (got.Err == nil || !strings.Contains(got.Err.Error(), c.wantIn)) {
				t.Errorf("Account() error = %v; want it to contain %q", got.Err, c.wantIn)
			}
			if got.Account.Role != c.wantRole {
				t.Errorf("Account() role = %q; want %q", got.Account.Role, c.wantRole)
			}
			if got.Account.Username != c.wantUser {
				t.Errorf("Account() username = %q; want %q", got.Account.Username, c.wantUser)
			}
			// A rejected pair is the case where the password is nearest to a
			// message the user reads: it must never be part of it.
			if got.Err != nil && strings.Contains(got.Err.Error(), marker) {
				t.Errorf("Account() error = %q; it must not carry the password", got.Err)
			}
		})
	}
}

func TestProbeAccountSendsBasicAuth(t *testing.T) {
	headers := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case headers <- r.Header.Get("Authorization"):
		default:
		}
		fmt.Fprint(w, `{"username":"andrea","role":"user"}`)
	}))
	defer srv.Close()

	cfg := config.Config{ServerURL: srv.URL, User: "andrea", Password: "s3cr3t"}
	if got := NewProbe(cfg).Account(context.Background()); !got.OK() {
		t.Fatalf("Account() = %v (err %v); want ok", got.State, got.Err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("andrea:s3cr3t"))
	if got := <-headers; got != want {
		t.Errorf("Authorization header = %q; want %q", got, want)
	}
}

func TestProbeRead(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		events  []string
		want    State
		wantErr error
		wantIn  string
	}{
		{
			name:   "open then a message the check never reads",
			status: http.StatusOK,
			events: []string{`{"id":"","time":1,"event":"open","topic":"alerts"}`},
			want:   StateOK,
		},
		{
			name:   "a keepalive before the open",
			status: http.StatusOK,
			events: []string{
				`{"id":"","time":0,"event":"keepalive","topic":"alerts"}`,
				`{"id":"","time":1,"event":"open","topic":"alerts"}`,
			},
			want: StateOK,
		},
		{
			name:    "no permission",
			status:  http.StatusForbidden,
			want:    StateFailed,
			wantErr: ErrNoPermission,
			wantIn:  "alerts",
		},
		{
			name:    "bad credentials",
			status:  http.StatusUnauthorized,
			want:    StateFailed,
			wantErr: ErrCredentials,
		},
		{
			name:   "the route is not there",
			status: http.StatusNotFound,
			want:   StateUnverifiable,
			wantIn: "404",
		},
		{
			name:   "the stream ends before the open",
			status: http.StatusOK,
			want:   StateUnverifiable,
			wantIn: "ended before the open event",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				for _, event := range c.events {
					fmt.Fprintln(w, event)
				}
				w.(http.Flusher).Flush()
			}))
			defer srv.Close()

			p := NewProbe(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}})
			got := p.Read(context.Background(), "alerts")

			if got.Topic != "alerts" {
				t.Errorf("Read().Topic = %q; want %q", got.Topic, "alerts")
			}
			if got.State != c.want {
				t.Fatalf("Read() with status %d and events %q = %v (err %v); want %v",
					c.status, c.events, got.State, got.Err, c.want)
			}
			if c.wantErr != nil && !errors.Is(got.Err, c.wantErr) {
				t.Errorf("Read() error = %v; want it to wrap %v", got.Err, c.wantErr)
			}
			if c.wantIn != "" && (got.Err == nil || !strings.Contains(got.Err.Error(), c.wantIn)) {
				t.Errorf("Read() error = %v; want it to contain %q", got.Err, c.wantIn)
			}
		})
	}
}

func TestProbeReadNeedsATopic(t *testing.T) {
	got := NewProbe(config.Config{ServerURL: "http://127.0.0.1:1"}).Read(context.Background(), "")
	if got.State != StateUnverifiable {
		t.Fatalf("Read(\"\") = %v; want %v", got.State, StateUnverifiable)
	}
	if got.Err == nil {
		t.Error("Read(\"\") returned no reason; want one")
	}
}

// TestProbeReadClosesOnOpen is the check that the connection is really cut: the
// server counts how many handlers saw their request cancelled, which only
// happens when the client closes the socket.
func TestProbeReadClosesOnOpen(t *testing.T) {
	var disconnects atomic.Int32
	var requests atomic.Int32
	closed := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Has("poll") || r.URL.Query().Has("since") {
			t.Errorf("stream request carries a query (%q); the read check must be the live stream",
				r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"","time":1,"event":"open","topic":"alerts"}`)
		w.(http.Flusher).Flush()

		// The open event was sent. If the client cuts here, this returns; a
		// client that kept reading would hold it until the test ends.
		<-r.Context().Done()
		disconnects.Add(1)
		select {
		case closed <- struct{}{}:
		default:
		}
	}))
	defer srv.Close()

	p := NewProbe(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}})
	if got := p.Read(context.Background(), "alerts"); !got.OK() {
		t.Fatalf("Read() = %v (err %v); want ok", got.State, got.Err)
	}

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the server never saw the connection close; the read check did not cut the stream")
	}
	if got := disconnects.Load(); got != 1 {
		t.Errorf("server counted %d disconnected streams; want 1", got)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("server counted %d requests; want 1 (the check must not reconnect)", got)
	}
}

func TestProbeReadStopsOnTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"","time":0,"event":"keepalive","topic":"alerts"}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := newProbe(config.Config{ServerURL: srv.URL, Topics: []string{"alerts"}}, 50*time.Millisecond)
	start := time.Now()
	got := p.Read(context.Background(), "alerts")
	if got.State != StateUnverifiable {
		t.Fatalf("Read() on a silent stream = %v (err %v); want %v", got.State, got.Err, StateUnverifiable)
	}
	if got.Err == nil || !strings.Contains(got.Err.Error(), "open") {
		t.Errorf("Read() error = %v; want it to say no open event arrived", got.Err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Read() took %v; want it to stop at its own timeout", elapsed)
	}
}

// TestProbeRun covers the four states the plan names: healthy, credentials
// rejected, no permission, and unverifiable.
func TestProbeRun(t *testing.T) {
	cases := []struct {
		name     string
		health   int
		account  int
		stream   int
		want     State
		wantStep Step // the first result that is not StateOK
	}{
		{name: "healthy", health: 200, account: 200, stream: 200, want: StateOK},
		{name: "not ntfy", health: 404, account: 200, stream: 200, want: StateFailed, wantStep: StepHealth},
		{name: "credentials rejected", health: 200, account: 401, stream: 200, want: StateFailed, wantStep: StepAccount},
		{name: "no read permission", health: 200, account: 200, stream: 403, want: StateFailed, wantStep: StepRead},
		{name: "account route missing", health: 200, account: 404, stream: 200, want: StateUnverifiable, wantStep: StepAccount},
		{name: "topic stream missing", health: 200, account: 200, stream: 404, want: StateUnverifiable, wantStep: StepRead},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/v1/health"):
					w.WriteHeader(c.health)
					fmt.Fprint(w, `{"healthy":true}`)
				case strings.HasSuffix(r.URL.Path, "/v1/account"):
					w.WriteHeader(c.account)
					fmt.Fprint(w, `{"username":"andrea","role":"user"}`)
				default:
					w.WriteHeader(c.stream)
					fmt.Fprintln(w, `{"id":"","time":1,"event":"open","topic":"a,b"}`)
					w.(http.Flusher).Flush()
				}
				if r.URL.Path != "/v1/health" && r.URL.Path != "/v1/account" {
					<-r.Context().Done()
				}
			}))
			defer srv.Close()

			cfg := config.Config{ServerURL: srv.URL, Topics: []string{"a", "b"}, User: "andrea", Password: "x"}
			report := NewProbe(cfg).Run(context.Background())

			if got := report.State(); got != c.want {
				t.Fatalf("Run() over health %d, account %d, stream %d = %v; want %v (results %+v)",
					c.health, c.account, c.stream, got, c.want, report.Results())
			}
			if got := len(report.Read); got != 2 {
				t.Errorf("Run() read results = %d; want 2 (one per topic)", got)
			}
			if c.wantStep != 0 || c.want != StateOK {
				// The first result that is not ok has to be the step that
				// failed, so the caller can name it.
				var first Step
				found := false
				for _, res := range report.Results() {
					if res.State != StateOK {
						first, found = res.Step, true
						break
					}
				}
				if !found {
					t.Fatal("Run() reported no failing step; want one")
				}
				if first != c.wantStep {
					t.Errorf("first failing step = %v; want %v", first, c.wantStep)
				}
			}
		})
	}
}

func TestProbeRunUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // every request from here on is refused

	p := NewProbe(config.Config{ServerURL: url, Topics: []string{"alerts"}})
	report := p.Run(context.Background())

	if got := report.State(); got != StateUnverifiable {
		t.Fatalf("Run() against a closed server = %v; want %v (results %+v)", got, StateUnverifiable, report.Results())
	}
	for _, res := range report.Results() {
		if res.Err == nil {
			t.Errorf("result of %v carries no reason; want one", res.Step)
		}
	}
}

func TestProbeRunWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewProbe(config.Config{ServerURL: "http://127.0.0.1:1", Topics: []string{"alerts"}})
	report := p.Run(ctx)

	if got := report.State(); got != StateUnverifiable {
		t.Fatalf("Run() with a cancelled context = %v; want %v", got, StateUnverifiable)
	}
}

func TestReportState(t *testing.T) {
	ok := Result{State: StateOK}
	maybe := Result{State: StateUnverifiable}
	bad := Result{State: StateFailed}

	cases := []struct {
		name   string
		report Report
		want   State
	}{
		{name: "nothing ran", report: Report{}, want: StateOK},
		{name: "all ok", report: Report{Health: ok, Account: ok, Read: []Result{ok}}, want: StateOK},
		{name: "one unverifiable", report: Report{Health: ok, Account: maybe, Read: []Result{ok}}, want: StateUnverifiable},
		{name: "one failed wins over unverifiable", report: Report{Health: ok, Account: maybe, Read: []Result{bad}}, want: StateFailed},
		{name: "health failed", report: Report{Health: bad, Account: ok, Read: []Result{ok}}, want: StateFailed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.report.State(); got != c.want {
				t.Errorf("Report.State() = %v; want %v", got, c.want)
			}
		})
	}
}

func TestStepAndStateString(t *testing.T) {
	steps := []struct {
		step Step
		want string
	}{
		{StepHealth, "health"},
		{StepAccount, "account"},
		{StepRead, "read"},
		{Step(99), "unknown"},
	}
	for _, c := range steps {
		if got := c.step.String(); got != c.want {
			t.Errorf("Step(%d).String() = %q; want %q", int(c.step), got, c.want)
		}
	}

	states := []struct {
		state State
		want  string
	}{
		{StateOK, "ok"},
		{StateUnverifiable, "unverifiable"},
		{StateFailed, "failed"},
		{State(99), "unknown"},
	}
	for _, c := range states {
		if got := c.state.String(); got != c.want {
			t.Errorf("State(%d).String() = %q; want %q", int(c.state), got, c.want)
		}
	}
}

func FuzzWaitForOpen(f *testing.F) {
	seeds := []string{
		`{"id":"","event":"open","topic":"t"}`,
		`{"event":"keepalive"}` + "\n" + `{"event":"open"}`,
		`{"event":"keepalive"}`,
		`{"event":"message","message":"hola"}`,
		``,
		`null`,
		`{`,
		strings.Repeat(`{"event":"keepalive"}`+"\n", 3),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		err := waitForOpen(strings.NewReader(in))

		// The same stream decoded by hand says whether it carries the event:
		// the check may only succeed when it does, and must not hang either.
		dec := json.NewDecoder(strings.NewReader(in))
		sawOpen := false
		for {
			var event struct {
				Event string `json:"event"`
			}
			if dec.Decode(&event) != nil {
				break
			}
			if event.Event == EventOpen {
				sawOpen = true
				break
			}
		}
		if (err == nil) != sawOpen {
			t.Fatalf("waitForOpen(%q) = %v; the stream has an open event: %t", in, err, sawOpen)
		}
	})
}
