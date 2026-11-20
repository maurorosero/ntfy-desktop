// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

func TestPublishRequest(t *testing.T) {
	cases := []struct {
		name      string
		server    string
		topic     string
		message   Publish
		wantPath  string
		wantQuery url.Values
		wantBody  string
	}{
		{
			name:     "body only",
			server:   "https://ntfy.sh",
			topic:    "alerts",
			message:  Publish{Body: "Backup done"},
			wantPath: "/alerts",
			wantBody: "Backup done",
		},
		{
			name:      "a title with accents",
			server:    "https://ntfy.sh",
			topic:     "alerts",
			message:   Publish{Body: "listo", Title: "Copia de café"},
			wantPath:  "/alerts",
			wantQuery: url.Values{"title": {"Copia de café"}},
			wantBody:  "listo",
		},
		{
			name:      "an urgency",
			server:    "https://ntfy.sh",
			topic:     "alerts",
			message:   Publish{Body: "disco lleno", Priority: 5},
			wantPath:  "/alerts",
			wantQuery: url.Values{"priority": {"5"}},
			wantBody:  "disco lleno",
		},
		{
			name:      "a priority that is asked for explicitly",
			server:    "https://ntfy.sh",
			topic:     "alerts",
			message:   Publish{Body: "hola", Priority: 3},
			wantPath:  "/alerts",
			wantQuery: url.Values{"priority": {"3"}},
			wantBody:  "hola",
		},
		{
			name:      "tags are joined with a comma",
			server:    "https://ntfy.sh",
			topic:     "backups",
			message:   Publish{Body: "falló", Tags: []string{"warning", "skull"}},
			wantPath:  "/backups",
			wantQuery: url.Values{"tags": {"warning,skull"}},
			wantBody:  "falló",
		},
		{
			name:      "a click action that carries its own query",
			server:    "https://ntfy.sh",
			topic:     "reddit",
			message:   Publish{Body: "nuevo", Click: "https://example.org/a?b=1&c=2"},
			wantPath:  "/reddit",
			wantQuery: url.Values{"click": {"https://example.org/a?b=1&c=2"}},
			wantBody:  "nuevo",
		},
		{
			name:      "markdown",
			server:    "https://ntfy.sh",
			topic:     "notes",
			message:   Publish{Body: "**fuerte**", Markdown: true},
			wantPath:  "/notes",
			wantQuery: url.Values{"markdown": {"yes"}},
			wantBody:  "**fuerte**",
		},
		{
			name:   "everything at once, on a server with a path",
			server: "http://192.168.1.10:2586/ntfy",
			topic:  "a_b-1",
			message: Publish{
				Body:     "hola",
				Title:    "Prueba",
				Priority: 4,
				Tags:     []string{"rocket"},
				Click:    "https://example.org",
				Markdown: true,
			},
			wantPath: "/ntfy/a_b-1",
			wantQuery: url.Values{
				"title":    {"Prueba"},
				"priority": {"4"},
				"tags":     {"rocket"},
				"click":    {"https://example.org"},
				"markdown": {"yes"},
			},
			wantBody: "hola",
		},
		{
			name:     "a body that looks like flags is sent as it is",
			server:   "https://ntfy.sh",
			topic:    "alerts",
			message:  Publish{Body: "--title no --priority 5"},
			wantPath: "/alerts",
			wantBody: "--title no --priority 5",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := New(config.Config{ServerURL: c.server, Topic: c.topic})
			req, err := client.publishRequest(context.Background(), c.message)
			if err != nil {
				t.Fatalf("publishRequest(%+v) = error %v; want no error", c.message, err)
			}

			if req.Method != http.MethodPost {
				t.Errorf("method = %q; want %q", req.Method, http.MethodPost)
			}
			if req.URL.Path != c.wantPath {
				t.Errorf("path = %q; want %q", req.URL.Path, c.wantPath)
			}
			want := c.wantQuery
			if want == nil {
				want = url.Values{}
			}
			if got := req.URL.Query(); !reflect.DeepEqual(got, want) {
				t.Errorf("query = %v; want %v", got, want)
			}
			if got := req.Header.Get("Content-Type"); got != publishContentType {
				t.Errorf("Content-Type = %q; want %q", got, publishContentType)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading the request body: %v", err)
			}
			if string(body) != c.wantBody {
				t.Errorf("body = %q; want %q", body, c.wantBody)
			}
		})
	}
}

func TestPublishRequestAuth(t *testing.T) {
	cases := []struct {
		name string
		user string
		pass string
	}{
		{name: "anonymous"},
		{name: "user and password", user: "andrea", pass: "s3cr3t"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{ServerURL: "https://ntfy.sh", Topic: "alerts", User: c.user, Password: c.pass}
			req, err := New(cfg).publishRequest(context.Background(), Publish{Body: "hola"})
			if err != nil {
				t.Fatalf("publishRequest() = error %v; want no error", err)
			}

			want := ""
			if c.user != "" {
				want = "Basic " + base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass))
			}
			if got := req.Header.Get("Authorization"); got != want {
				t.Errorf("Authorization header = %q; want %q", got, want)
			}
			if strings.Contains(req.URL.String(), c.pass) && c.pass != "" {
				t.Errorf("the url %q carries the password", req.URL)
			}
		})
	}
}

// TestPublishSendsTheMessage is the acceptance criterion of the client: a POST
// to the topic with the body, the fields in the query and the id back.
func TestPublishSendsTheMessage(t *testing.T) {
	type seen struct {
		method, path, query, contentType, body string
	}
	requests := make(chan seen, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the body the client sent: %v", err)
		}
		select {
		case requests <- seen{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"hwQ2YpKdmg","time":1661960156,"event":"message","topic":"casa","message":"hola"}`)
	}))
	defer srv.Close()

	client := New(config.Config{ServerURL: srv.URL, Topic: "casa"})
	m, err := client.Publish(context.Background(), Publish{
		Body:     "hola",
		Title:    "Prueba",
		Priority: 4,
		Tags:     []string{"rocket"},
		Click:    "https://example.org",
		Markdown: true,
	})
	if err != nil {
		t.Fatalf("Publish() = error %v; want no error", err)
	}
	if m.ID != "hwQ2YpKdmg" {
		t.Errorf("Publish() id = %q; want %q", m.ID, "hwQ2YpKdmg")
	}

	var got seen
	select {
	case got = <-requests:
	default:
		t.Fatal("the server did not see the publish request")
	}
	if got.method != http.MethodPost {
		t.Errorf("method = %q; want %q", got.method, http.MethodPost)
	}
	if got.path != "/casa" {
		t.Errorf("path = %q; want %q", got.path, "/casa")
	}
	if got.body != "hola" {
		t.Errorf("body = %q; want %q", got.body, "hola")
	}
	if got.contentType != publishContentType {
		t.Errorf("Content-Type = %q; want %q", got.contentType, publishContentType)
	}
	for key, want := range map[string]string{
		"title":    "Prueba",
		"priority": "4",
		"tags":     "rocket",
		"click":    "https://example.org",
		"markdown": "yes",
	} {
		values, err := url.ParseQuery(got.query)
		if err != nil {
			t.Fatalf("parsing the query %q: %v", got.query, err)
		}
		if have := values.Get(key); have != want {
			t.Errorf("query %s = %q; want %q", key, have, want)
		}
	}
}

func TestPublishReportsTheStatusCode(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		status string
	}{
		{name: "a bad request", code: http.StatusBadRequest, status: "400 Bad Request"},
		{name: "rejected credentials", code: http.StatusUnauthorized, status: "401 Unauthorized"},
		{name: "a refused publish", code: http.StatusForbidden, status: "403 Forbidden"},
		{name: "too many requests", code: http.StatusTooManyRequests, status: "429 Too Many Requests"},
		{name: "a broken server", code: http.StatusInternalServerError, status: "500 Internal Server Error"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "no", c.code)
			}))
			defer srv.Close()

			cfg := config.Config{ServerURL: srv.URL, Topic: "alerts", User: "andrea", Password: "s3cr3t"}
			_, err := New(cfg).Publish(context.Background(), Publish{Body: "hola"})
			if err == nil {
				t.Fatalf("Publish() with status %d = no error; want one", c.code)
			}

			var status *StatusError
			if !errors.As(err, &status) {
				t.Fatalf("Publish() error is %T (%v); want *StatusError", err, err)
			}
			if status.Code != c.code {
				t.Errorf("StatusError.Code = %d; want %d", status.Code, c.code)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(c.code)) {
				t.Errorf("error %q; want the status code %d in it", err, c.code)
			}
			if strings.Contains(err.Error(), cfg.Password) {
				t.Errorf("error %q exposes the password", err)
			}
		})
	}
}

// TestPublishWithoutAnID covers a server that answers 200 but no identifier:
// the client reports what it got instead of inventing one.
func TestPublishWithoutAnID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"event":"message","topic":"alerts"}`)
	}))
	defer srv.Close()

	m, err := New(config.Config{ServerURL: srv.URL, Topic: "alerts"}).Publish(context.Background(), Publish{Body: "hola"})
	if err != nil {
		t.Fatalf("Publish() = error %v; want no error", err)
	}
	if m.ID != "" {
		t.Errorf("Publish() id = %q; want an empty one for the caller to report", m.ID)
	}
}
