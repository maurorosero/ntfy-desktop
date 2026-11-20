// SPDX-License-Identifier: GPL-3.0-or-later

package action

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

func TestRunnable(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{action: View, want: true},
		{action: HTTP, want: true},
		{action: "broadcast", want: false},
		{action: "copy", want: false},
		{action: "", want: false},
		{action: "View", want: false},
	}

	for _, c := range cases {
		if got := Runnable(ntfy.Action{Action: c.action}); got != c.want {
			t.Errorf("Runnable(%q) = %v, quiero %v", c.action, got, c.want)
		}
	}
}

func TestFilter(t *testing.T) {
	in := []ntfy.Action{
		{Action: "broadcast", Label: "Other device"},
		{Action: View, Label: "Open"},
		{Action: "copy", Label: "Copy"},
		{Action: HTTP, Label: "Deny"},
	}
	want := []string{"Open", "Deny"}

	var got []string
	for _, a := range Filter(in) {
		got = append(got, Label(a))
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Filter() = %q, quiero %q", got, want)
	}
	if out := Filter([]ntfy.Action{{Action: "broadcast"}}); len(out) != 0 {
		t.Errorf("Filter() con nada que ejecutar devolvió %d acciones, quiero 0", len(out))
	}
}

func TestLabel(t *testing.T) {
	cases := []struct {
		action ntfy.Action
		want   string
	}{
		{action: ntfy.Action{Action: View, Label: "Open portal"}, want: "Open portal"},
		{action: ntfy.Action{Action: HTTP}, want: HTTP},
	}

	for _, c := range cases {
		if got := Label(c.action); got != c.want {
			t.Errorf("Label(%q) = %q, quiero %q", c.action.Action, got, c.want)
		}
	}
}

// newRunner builds a runner whose two exits are the test's own.
func newRunner(client *http.Client, open func(context.Context, string) error) *Runner {
	return &Runner{http: client, open: open}
}

func TestRunView(t *testing.T) {
	var opened []string
	r := newRunner(http.DefaultClient, func(ctx context.Context, target string) error {
		opened = append(opened, target)
		return nil
	})

	err := r.Run(context.Background(), ntfy.Action{Action: View, Label: "Open", URL: "https://home.example/portal"})
	if err != nil {
		t.Fatalf("Run() = %v, quiero nil", err)
	}
	if len(opened) != 1 || opened[0] != "https://home.example/portal" {
		t.Errorf("se abrió %q, quiero https://home.example/portal", opened)
	}
}

func TestRunRejectsUnsafeTargets(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{name: "a local handler", url: "file:///etc/passwd"},
		{name: "a script scheme", url: "javascript:alert(1)"},
		{name: "a bare path", url: "/etc/passwd"},
		{name: "no host", url: "https://"},
		{name: "empty", url: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRunner(http.DefaultClient, func(context.Context, string) error {
				t.Fatal("no se debía abrir nada")
				return nil
			})
			err := r.Run(context.Background(), ntfy.Action{Action: View, URL: c.url})
			if !errors.Is(err, ErrUnsafeURL) {
				t.Errorf("Run(%q) = %v, quiero ErrUnsafeURL", c.url, err)
			}
		})
	}
}

func TestRunRejectsUnknownActions(t *testing.T) {
	r := newRunner(http.DefaultClient, nil)
	err := r.Run(context.Background(), ntfy.Action{Action: "broadcast", URL: "https://example.org/"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Run() = %v, quiero ErrUnsupported", err)
	}
}

func TestRunHTTPDefaultsToPost(t *testing.T) {
	var (
		method  string
		body    string
		header  string
		callURL string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		header = r.Header.Get("X-Token")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("leyendo el cuerpo: %v", err)
		}
		body = string(raw)
		callURL = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := newRunner(server.Client(), nil)
	err := r.Run(context.Background(), ntfy.Action{
		Action:  HTTP,
		Label:   "Turn down",
		URL:     server.URL + "/temperature",
		Headers: map[string]string{"X-Token": "t"},
		Body:    `{"temperature": 65}`,
	})
	if err != nil {
		t.Fatalf("Run() = %v, quiero nil", err)
	}

	if method != http.MethodPost {
		t.Errorf("método = %q, quiero POST", method)
	}
	if callURL != "/temperature" {
		t.Errorf("ruta = %q, quiero /temperature", callURL)
	}
	if header != "t" {
		t.Errorf("cabecera X-Token = %q, quiero t", header)
	}
	if body != `{"temperature": 65}` {
		t.Errorf("cuerpo = %q, quiero el declarado", body)
	}
}

func TestRunHTTPUsesTheDeclaredMethod(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	r := newRunner(server.Client(), nil)
	err := r.Run(context.Background(), ntfy.Action{Action: HTTP, URL: server.URL, Method: "delete"})
	if err != nil {
		t.Fatalf("Run() = %v, quiero nil", err)
	}
	if method != http.MethodDelete {
		t.Errorf("método = %q, quiero DELETE", method)
	}
}

func TestRunHTTPRejectsMethodsItDoesNotCall(t *testing.T) {
	r := newRunner(http.DefaultClient, nil)
	err := r.Run(context.Background(), ntfy.Action{Action: HTTP, URL: "https://example.org/", Method: "TRACE"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Run() = %v, quiero ErrUnsupported", err)
	}
}

func TestRunHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer server.Close()

	r := newRunner(server.Client(), nil)
	err := r.Run(context.Background(), ntfy.Action{Action: HTTP, URL: server.URL})

	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("Run() = %v, quiero *StatusError", err)
	}
	if status.Code != http.StatusForbidden {
		t.Errorf("Code = %d, quiero %d", status.Code, http.StatusForbidden)
	}
}
