// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePublish(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     publishOptions
		wantFail bool
	}{
		{
			name: "the subcommand and the topic",
			args: []string{"publish", "casa"},
			want: publishOptions{topic: "casa"},
		},
		{
			name: "the --publish flag of the issue",
			args: []string{"--publish", "casa"},
			want: publishOptions{topic: "casa"},
		},
		{
			name: "the --publish flag with an equals sign",
			args: []string{"--publish=casa"},
			want: publishOptions{topic: "casa"},
		},
		{
			name: "the --topic flag",
			args: []string{"publish", "--topic", "casa"},
			want: publishOptions{topic: "casa"},
		},
		{
			name: "the issue's command line",
			args: []string{"--publish", "casa", "--title", "Prueba", "--priority", "4"},
			want: publishOptions{topic: "casa", title: "Prueba", priority: 4},
		},
		{
			name: "flags after the topic",
			args: []string{"publish", "casa", "--title", "Prueba", "--markdown"},
			want: publishOptions{topic: "casa", title: "Prueba", markdown: true},
		},
		{
			name: "tags with spaces around them",
			args: []string{"publish", "casa", "--tags", "warning, skull ,"},
			want: publishOptions{topic: "casa", tags: []string{"warning", "skull"}},
		},
		{
			name: "a priority by name",
			args: []string{"publish", "casa", "--priority", "URGENT"},
			want: publishOptions{topic: "casa", priority: 5},
		},
		{
			name: "a click action",
			args: []string{"publish", "casa", "--click", "https://example.org/a?b=1"},
			want: publishOptions{topic: "casa", click: "https://example.org/a?b=1"},
		},
		{
			name: "no topic falls back to the configuration",
			args: []string{"publish"},
			want: publishOptions{},
		},
		{name: "an invalid priority", args: []string{"publish", "casa", "--priority", "6"}, wantFail: true},
		{name: "a priority that is not a name", args: []string{"publish", "casa", "--priority", "alta"}, wantFail: true},
		{name: "two topics", args: []string{"publish", "casa", "otra"}, wantFail: true},
		{name: "a topic and the flag", args: []string{"publish", "casa", "--topic", "otra"}, wantFail: true},
		{name: "an unknown flag", args: []string{"publish", "casa", "--nope"}, wantFail: true},
		{name: "--publish without a topic", args: []string{"--publish"}, wantFail: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := parse(c.args, testTranslator(), io.Discard)
			if c.wantFail {
				if err == nil {
					t.Fatalf("parse(%q) = %+v; want an error", c.args, opts)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q) = error %v; want no error", c.args, err)
			}
			if opts.command != commandPublish {
				t.Fatalf("parse(%q).command = %q; want %q", c.args, opts.command, commandPublish)
			}
			got := opts.publish
			if got.topic != c.want.topic {
				t.Errorf("parse(%q).topic = %q; want %q", c.args, got.topic, c.want.topic)
			}
			if got.title != c.want.title {
				t.Errorf("parse(%q).title = %q; want %q", c.args, got.title, c.want.title)
			}
			if got.priority != c.want.priority {
				t.Errorf("parse(%q).priority = %d; want %d", c.args, got.priority, c.want.priority)
			}
			if !slices.Equal(got.tags, c.want.tags) {
				t.Errorf("parse(%q).tags = %v; want %v", c.args, got.tags, c.want.tags)
			}
			if got.click != c.want.click {
				t.Errorf("parse(%q).click = %q; want %q", c.args, got.click, c.want.click)
			}
			if got.markdown != c.want.markdown {
				t.Errorf("parse(%q).markdown = %v; want %v", c.args, got.markdown, c.want.markdown)
			}
		})
	}
}

func TestParsePriority(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{in: "1", want: 1, ok: true},
		{in: "5", want: 5, ok: true},
		{in: " 4 ", want: 4, ok: true},
		{in: "min", want: 1, ok: true},
		{in: "low", want: 2, ok: true},
		{in: "default", want: 3, ok: true},
		{in: "high", want: 4, ok: true},
		{in: "max", want: 5, ok: true},
		{in: "Urgent", want: 5, ok: true},
		{in: "0", want: 0, ok: false},
		{in: "6", want: 0, ok: false},
		{in: "alta", want: 0, ok: false},
		{in: "", want: 0, ok: false},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := parsePriority(c.in)
			if ok != c.ok || got != c.want {
				t.Errorf("parsePriority(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestReadBody(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "the newline of a pipe", in: "hola\n", want: "hola"},
		{name: "no newline at all", in: "hola", want: "hola"},
		{name: "a windows newline", in: "hola\r\n", want: "hola"},
		{name: "only one newline is dropped", in: "hola\n\n", want: "hola\n"},
		{name: "a body that looks like flags", in: "--title no\n", want: "--title no"},
		{name: "several lines", in: "uno\ndos\n", want: "uno\ndos"},
		{name: "nothing", in: "", wantErr: true},
		{name: "only whitespace", in: " \n\t\n", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := readBody(strings.NewReader(c.in), testTranslator())
			if c.wantErr {
				if err == nil {
					t.Fatalf("readBody(%q) = %q; want an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("readBody(%q) = error %v; want no error", c.in, err)
			}
			if got != c.want {
				t.Errorf("readBody(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}

	t.Run("a body over the limit", func(t *testing.T) {
		big := strings.NewReader(strings.Repeat("x", maxBody+1))
		if got, err := readBody(big, testTranslator()); err == nil {
			t.Fatalf("readBody(%d bytes) = %d bytes; want an error", maxBody+1, len(got))
		}
	})
}

// TestRunPublish is the acceptance criterion end to end: the body comes from
// stdin, the topic and the flags from the command line, and the id the server
// assigned is what the command prints.
func TestRunPublish(t *testing.T) {
	type seen struct {
		method, path, query, body string
	}
	requests := make(chan seen, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the body the client sent: %v", err)
		}
		select {
		case requests <- seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body)}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"hwQ2YpKdmg","time":1661960156,"event":"message","topic":"casa","message":"hola"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeTestConfig(t, dir, srv.URL, "alerts", "", "")
	withStdin(t, "--title impostor\nhola\n")

	var out, errOut bytes.Buffer
	args := []string{"publish", "casa", "--title", "Prueba", "--priority", "4", "--tags", "rocket, check"}
	if code := run(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatalf("run(%q) = %d; want 0. stderr: %s", args, code, errOut.String())
	}
	if out.String() != "hwQ2YpKdmg\n" {
		t.Errorf("stdout = %q; want the id the server returned", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q; want nothing", errOut.String())
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
	if want := "--title impostor\nhola"; got.body != want {
		t.Errorf("body = %q; want %q", got.body, want)
	}
	values, err := url.ParseQuery(got.query)
	if err != nil {
		t.Fatalf("parsing the query %q: %v", got.query, err)
	}
	for key, want := range map[string]string{
		"title":    "Prueba",
		"priority": "4",
		"tags":     "rocket,check",
	} {
		if have := values.Get(key); have != want {
			t.Errorf("query %s = %q; want %q", key, have, want)
		}
	}
}

// TestRunPublishUsesTheConfiguredTopic covers the topic nobody typed: the one
// in the configuration, which is where the daemon reads it too.
func TestRunPublishUsesTheConfiguredTopic(t *testing.T) {
	paths := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}
		io.WriteString(w, `{"id":"x1","event":"message","topic":"alerts"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeTestConfig(t, dir, srv.URL, "alerts", "", "")
	withStdin(t, "hola\n")

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"publish"}, &out, &errOut); code != 0 {
		t.Fatalf("run(publish) = %d; want 0. stderr: %s", code, errOut.String())
	}

	select {
	case got := <-paths:
		if want := "/alerts"; got != want {
			t.Errorf("path = %q; want %q", got, want)
		}
	default:
		t.Fatal("the server did not see the publish request")
	}
}

// TestRunPublishReportsAForbiddenTopic is the second acceptance criterion: a
// 403 says what to look at, and never what the password is.
func TestRunPublishReportsAForbiddenTopic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeTestConfig(t, dir, srv.URL, "alerts", "andrea", "s3cr3t")
	withStdin(t, "hola\n")

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--publish", "readonly"}, &out, &errOut); code != 1 {
		t.Fatalf("run(--publish readonly) = %d; want 1", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q; want nothing", out.String())
	}
	got := errOut.String()
	for _, want := range []string{"readonly", "403", "refused"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q; want %q in it", got, want)
		}
	}
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("stderr = %q; it exposes the password", got)
	}
}

func TestRunPublishReportsAnEmptyBody(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeTestConfig(t, dir, "https://ntfy.invalid", "alerts", "", "")
	withStdin(t, "\n")

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"publish", "casa"}, &out, &errOut); code != 1 {
		t.Fatalf("run(publish) with an empty body = %d; want 1", code)
	}
	if !strings.Contains(errOut.String(), "empty") {
		t.Errorf("stderr = %q; want the empty body reported", errOut.String())
	}
}

func TestRunPublishReportsAMissingConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	withStdin(t, "hola\n")

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"publish", "casa"}, &out, &errOut); code != 1 {
		t.Fatalf("run(publish) with no configuration = %d; want 1", code)
	}
	if !strings.Contains(errOut.String(), "config.toml") {
		t.Errorf("stderr = %q; want the path of the configuration", errOut.String())
	}
}

// writeTestConfig writes the file the commands read under XDG_CONFIG_HOME.
func writeTestConfig(t *testing.T, dir, server, topic, user, password string) {
	t.Helper()
	cfgDir := filepath.Join(dir, "ntfy-desktop")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("creating the configuration directory: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "server = %q\ntopic = %q\n", server, topic)
	if user != "" {
		fmt.Fprintf(&b, "user = %q\npassword = %q\n", user, password)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}
}

// withStdin puts body under the process input until the test ends, which is how
// the command reads the message.
func withStdin(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the input: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the input: %v", err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = old
		f.Close()
	})
}
