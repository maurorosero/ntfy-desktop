// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
)

// serverCodes says what each route of the fake server answers. Zero is the
// healthy, anonymous server with a readable topic.
type serverCodes struct {
	health  int
	account int
	read    int
	body    string // answer of /v1/account when it is not the default
}

// newNtfyServer starts the three routes the probe reads, on the loopback
// interface of the test process. Nothing here reaches the network.
func newNtfyServer(t *testing.T, codes serverCodes) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if codes.health != 0 && codes.health != http.StatusOK {
			http.Error(w, "not here", codes.health)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"healthy":true,"role":"active"}`)
	})
	mux.HandleFunc("/v1/account", func(w http.ResponseWriter, r *http.Request) {
		if codes.account != 0 && codes.account != http.StatusOK {
			http.Error(w, "not here", codes.account)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := codes.body
		if body == "" {
			body = `{"username":"*","role":"anonymous"}`
		}
		io.WriteString(w, body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if codes.read != 0 && codes.read != http.StatusOK {
			http.Error(w, "not here", codes.read)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"id":"x","time":1,"event":"open","topic":"casa"}`+"\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// deadServer is the URL of a server that refuses every connection: it was
// listening a moment ago and is not any more.
func deadServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// configDir puts XDG_CONFIG_HOME in a temporary directory and returns it.
func configDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// writeConfig writes the file the config commands read, as it is given.
func writeConfig(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the configuration directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}
	return path
}

// readFile is the content of the configuration, or "" when there is none.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// digest is the fingerprint of the file, to prove a command did not touch it.
func digest(t *testing.T, path string) string {
	t.Helper()
	return fmt.Sprintf("%x", sha256.Sum256([]byte(readFile(t, path))))
}

// runConfigCommand runs the command line as the program would and returns the
// exit code with what it printed. The language is pinned to English so the
// assertions do not depend on the locale of whoever runs the tests.
func runConfigCommand(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("LC_ALL", "C")
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestParseConfig(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     configOptions
		wantFail bool
	}{
		{name: "no flags is the wizard", args: []string{"config"}, want: configOptions{}},
		{name: "--show", args: []string{"config", "--show"}, want: configOptions{show: true}},
		{name: "--check", args: []string{"config", "--check"}, want: configOptions{check: true}},
		{name: "--gui", args: []string{"config", "--gui"}, want: configOptions{gui: true}},
		{
			name: "a whole value after the equals sign",
			args: []string{"config", "--set", "server=https://ntfy.sh"},
			want: configOptions{setKey: "server", setValue: "https://ntfy.sh"},
		},
		{
			name: "a value with commas and spaces",
			args: []string{"config", "--set", "topic=casa, avisos"},
			want: configOptions{setKey: "topic", setValue: "casa, avisos"},
		},
		{
			name: "the log level",
			args: []string{"config", "--set", "log-level=debug"},
			want: configOptions{setKey: "log-level", setValue: "debug"},
		},
		{
			name: "the password from stdin next to a key",
			args: []string{"config", "--set", "user=andrea", "--password-stdin"},
			want: configOptions{setKey: "user", setValue: "andrea", passwordStdin: true},
		},
		{name: "only the password", args: []string{"config", "--password-stdin"}, want: configOptions{passwordStdin: true}},
		{name: "add a topic", args: []string{"config", "topic", "--add", "avisos"}, want: configOptions{topicAdd: "avisos"}},
		{name: "remove a topic", args: []string{"config", "topic", "--remove", "avisos"}, want: configOptions{topicRemove: "avisos"}},
		{name: "the spaces around a topic are trimmed", args: []string{"config", "topic", "--add", " avisos "}, want: configOptions{topicAdd: "avisos"}},

		{name: "two actions", args: []string{"config", "--show", "--check"}, wantFail: true},
		{name: "an action and a set", args: []string{"config", "--check", "--set", "user=x"}, wantFail: true},
		{name: "--gui and --show", args: []string{"config", "--gui", "--show"}, wantFail: true},
		{name: "--gui and --check", args: []string{"config", "--gui", "--check"}, wantFail: true},
		{name: "--gui and a set", args: []string{"config", "--gui", "--set", "user=x"}, wantFail: true},
		{name: "--gui with --password-stdin", args: []string{"config", "--gui", "--password-stdin"}, wantFail: true},
		{name: "the password as an argument", args: []string{"config", "--set", "password=x"}, wantFail: true},
		{name: "an empty password as an argument", args: []string{"config", "--set", "password="}, wantFail: true},
		{name: "a set without an equals sign", args: []string{"config", "--set", "server"}, wantFail: true},
		{name: "an empty set", args: []string{"config", "--set", ""}, wantFail: true},
		{name: "an unknown key", args: []string{"config", "--set", "nope=1"}, wantFail: true},
		{name: "--password-stdin with --show", args: []string{"config", "--show", "--password-stdin"}, wantFail: true},
		{name: "--password-stdin with --check", args: []string{"config", "--check", "--password-stdin"}, wantFail: true},
		{name: "an unknown flag", args: []string{"config", "--nope"}, wantFail: true},
		{name: "an extra argument", args: []string{"config", "casa"}, wantFail: true},
		{name: "topic without an action", args: []string{"config", "topic"}, wantFail: true},
		{name: "add and remove at once", args: []string{"config", "topic", "--add", "a", "--remove", "b"}, wantFail: true},
		{name: "an empty add", args: []string{"config", "topic", "--add", ""}, wantFail: true},
		{name: "an empty remove", args: []string{"config", "topic", "--remove", ""}, wantFail: true},
		{name: "an extra argument after topic", args: []string{"config", "topic", "--add", "a", "b"}, wantFail: true},
		{name: "an unknown flag of topic", args: []string{"config", "topic", "--nope", "a"}, wantFail: true},
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
			if opts.command != commandConfig {
				t.Fatalf("parse(%q).command = %q; want %q", c.args, opts.command, commandConfig)
			}
			if opts.config != c.want {
				t.Errorf("parse(%q) = %+v; want %+v", c.args, opts.config, c.want)
			}
		})
	}
}

// TestConfigCheckExitCodes is the table of the contract: the five codes, each
// with the reason that produces it.
func TestConfigCheckExitCodes(t *testing.T) {
	valid := "server = %q\ntopic = \"casa\"\n"
	withCredentials := "server = %q\ntopic = \"casa\"\nuser = \"andrea\"\npassword = \"s3cr3t\"\n"

	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string) // writes the file or leaves it out
		want    int
		wantErr string
	}{
		{
			name: "a healthy anonymous configuration",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(valid, newNtfyServer(t, serverCodes{})))
			},
			want: exitOK,
		},
		{
			name: "no file at all",
			setup: func(t *testing.T, dir string) {
			},
			want:    exitFile,
			wantErr: "there is no configuration",
		},
		{
			name: "a file with a typo",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, "server = \"https://ntfy.sh\"\ntopic = \"casa\"\ntopics = \"x\"\n")
			},
			want:    exitFile,
			wantErr: "cannot be read",
		},
		{
			name: "a URL that is not http",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, "server = \"ftp://ntfy.sh\"\ntopic = \"casa\"\n")
			},
			want:    exitFile,
			wantErr: "cannot be read",
		},
		{
			name: "a server that does not answer",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(valid, deadServer(t)))
			},
			want:    exitServer,
			wantErr: "does not answer",
		},
		{
			name: "a server that is not ntfy",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(valid, newNtfyServer(t, serverCodes{health: http.StatusNotFound})))
			},
			want:    exitServer,
			wantErr: "not an ntfy server",
		},
		{
			name: "credentials the server rejects",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(withCredentials, newNtfyServer(t, serverCodes{account: http.StatusUnauthorized})))
			},
			want:    exitCredentials,
			wantErr: "rejected the credentials",
		},
		{
			name: "a topic the credentials cannot read",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(valid, newNtfyServer(t, serverCodes{read: http.StatusForbidden})))
			},
			want:    exitPermission,
			wantErr: "no permission for a topic",
		},
		{
			name: "an account route the server does not have",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, fmt.Sprintf(valid, newNtfyServer(t, serverCodes{account: http.StatusNotFound})))
			},
			want: exitOK,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := configDir(t)
			path := filepath.Join(dir, "ntfy-desktop", "config.toml")
			c.setup(t, dir)
			before := digest(t, path)

			code, out, errOut := runConfigCommand(t, "config", "--check")
			if code != c.want {
				t.Fatalf("config --check = %d; want %d. stdout: %s stderr: %s", code, c.want, out, errOut)
			}
			if c.wantErr != "" && !strings.Contains(errOut, c.wantErr) {
				t.Errorf("stderr = %q; want %q in it", errOut, c.wantErr)
			}
			if after := digest(t, path); after != before {
				t.Errorf("--check changed the file: %s -> %s", before, after)
			}
			if c.want == exitOK && !strings.Contains(out, "ok") {
				t.Errorf("stdout = %q; want the checks listed", out)
			}
		})
	}
}

func TestConfigShow(t *testing.T) {
	cases := []struct {
		name     string
		write    string
		wantCode int
		want     []string
		notWant  []string
	}{
		{
			name:     "a configuration with a password",
			write:    "server = \"https://ntfy.sh\"\ntopic = \"casa\"\nuser = \"andrea\"\npassword = \"s3cr3t\"\n",
			wantCode: exitOK,
			want:     []string{"https://ntfy.sh", "casa", "andrea", maskedPassword, "info (default)"},
			notWant:  []string{"s3cr3t"},
		},
		{
			name:     "a configuration without credentials",
			write:    "server = \"https://ntfy.sh\"\ntopic = \"casa, avisos\"\nlog-level = \"debug\"\n",
			wantCode: exitOK,
			want:     []string{"casa, avisos", "anonymous", "debug"},
		},
		{
			name:     "no file",
			wantCode: exitFile,
			want:     []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := configDir(t)
			path := filepath.Join(dir, "ntfy-desktop", "config.toml")
			if c.write != "" {
				writeConfig(t, dir, c.write)
			}

			code, out, errOut := runConfigCommand(t, "config", "--show")
			if code != c.wantCode {
				t.Fatalf("config --show = %d; want %d. stderr: %s", code, c.wantCode, errOut)
			}
			if c.wantCode == exitOK && !strings.Contains(out, path) {
				t.Errorf("stdout = %q; want the path %s in it", out, path)
			}
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("stdout = %q; want %q in it", out, want)
				}
			}
			for _, not := range c.notWant {
				if strings.Contains(out, not) {
					t.Errorf("stdout = %q; it exposes %q", out, not)
				}
			}
		})
	}
}

// TestConfigSet is the table of the commands that change one key: what is
// written, what is refused, and what the file looks like afterwards.
func TestConfigSet(t *testing.T) {
	cases := []struct {
		name     string
		write    func(t *testing.T) string // the file before, with its path
		args     func(t *testing.T) []string
		env      string // what --password-stdin reads
		wantCode int
		want     []string // in the file afterwards
		notWant  []string // in the file afterwards
		wantErr  string
	}{
		{
			name: "the server, keeping the comment behind it",
			write: func(t *testing.T) string {
				dir := t.TempDir()
				return writeConfig(t, dir, "server = \"https://old.invalid\"  # el de casa\ntopic = \"casa\"\n")
			},
			args: func(t *testing.T) []string {
				return []string{"config", "--set", "server=" + newNtfyServer(t, serverCodes{})}
			},
			wantCode: exitOK,
			want:     []string{"# el de casa", "topic = \"casa\""},
		},
		{
			name: "the log level is local and needs no server",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "log-level=DEBUG"} },
			wantCode: exitOK,
			want:     []string{"log-level = \"debug\""},
		},
		{
			name: "a log level the file does not take",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "log-level=verbose"} },
			wantCode: exitFile,
			wantErr:  "invalid",
		},
		{
			name: "a URL the file does not take",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "server=ftp://nope"} },
			wantCode: exitFile,
			wantErr:  "http",
		},
		{
			name: "a user without a password is half a pair",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "user=andrea"} },
			wantCode: exitFile,
			wantErr:  "invalid",
		},
		{
			name: "the password from the standard input",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "user=andrea", "--password-stdin"} },
			env:      "s3cr3t\n",
			wantCode: exitOK,
			want:     []string{"user = \"andrea\"", "password = \"s3cr3t\""},
		},
		{
			name: "a server that does not answer is a warning, not a stop",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args: func(t *testing.T) []string {
				return []string{"config", "--set", "server=" + deadServer(t)}
			},
			wantCode: exitOK,
			want:     []string{"warning"},
			wantErr:  "warning",
		},
		{
			name: "a server that is not ntfy blocks the change",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args: func(t *testing.T) []string {
				return []string{"config", "--set", "server=" + newNtfyServer(t, serverCodes{health: http.StatusNotFound})}
			},
			wantCode: exitServer,
			wantErr:  "not an ntfy server",
		},
		{
			name: "credentials the server rejects block the change",
			write: func(t *testing.T) string {
				return writeConfig(t, t.TempDir(), "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
			},
			args: func(t *testing.T) []string {
				return []string{"config", "--set", "server=" + newNtfyServer(t, serverCodes{account: http.StatusUnauthorized})}
			},
			wantCode: exitCredentials,
			wantErr:  "rejected the credentials",
		},
		{
			name: "no file to change",
			write: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "ntfy-desktop", "config.toml")
			},
			args:     func(t *testing.T) []string { return []string{"config", "--set", "log-level=debug"} },
			wantCode: exitFile,
			wantErr:  "there is no configuration",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := c.write(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(path)))
			if got := filepath.Base(filepath.Dir(path)); got != "ntfy-desktop" {
				t.Fatalf("the test put the file in %q; want a ntfy-desktop directory", got)
			}
			before := readFile(t, path)
			if c.env != "" {
				withStdin(t, c.env)
			}
			args := c.args(t)

			code, out, errOut := runConfigCommand(t, args...)
			if code != c.wantCode {
				t.Fatalf("%q = %d; want %d. stdout: %s stderr: %s", args, code, c.wantCode, out, errOut)
			}
			if c.wantErr != "" && !strings.Contains(errOut+out, c.wantErr) {
				t.Errorf("output = %q / %q; want %q in it", out, errOut, c.wantErr)
			}
			after := readFile(t, path)
			if code != exitOK && after != before {
				t.Errorf("the file changed on a failure:\nbefore: %s\nafter: %s", before, after)
			}
			for _, want := range c.want {
				if !strings.Contains(after+out+errOut, want) {
					t.Errorf("file = %q, output = %q / %q; want %q somewhere", after, out, errOut, want)
				}
			}
			for _, not := range c.notWant {
				if strings.Contains(after, not) {
					t.Errorf("file = %q; it still has %q", after, not)
				}
			}
		})
	}
}

// TestConfigTopics covers the two operations that change the list: one name
// added or removed, and the refusal to leave it empty.
func TestConfigTopics(t *testing.T) {
	cases := []struct {
		name     string
		topics   string
		args     []string
		wantCode int
		want     string
		wantErr  string
	}{
		{
			name: "add", topics: "casa", args: []string{"config", "topic", "--add", "avisos"},
			wantCode: exitOK, want: "topic = \"casa, avisos\"",
		},
		{
			name: "add to a list", topics: "casa, avisos", args: []string{"config", "topic", "--add", "pruebas"},
			wantCode: exitOK, want: "topic = \"casa, avisos, pruebas\"",
		},
		{
			name: "add a name that is already there", topics: "casa", args: []string{"config", "topic", "--add", "casa"},
			wantCode: exitFile, wantErr: "already in the topic list",
		},
		{
			name: "add a name the loader does not take", topics: "casa", args: []string{"config", "topic", "--add", "con espacio"},
			wantCode: exitFile, wantErr: "not a valid topic",
		},
		{
			name: "remove", topics: "casa, avisos", args: []string{"config", "topic", "--remove", "casa"},
			wantCode: exitOK, want: "topic = \"avisos\"",
		},
		{
			name: "remove a name that is not there", topics: "casa", args: []string{"config", "topic", "--remove", "avisos"},
			wantCode: exitFile, wantErr: "is not in the topic list",
		},
		{
			name: "remove the only topic", topics: "casa", args: []string{"config", "topic", "--remove", "casa"},
			wantCode: exitFile, wantErr: "cannot remove the only topic",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := configDir(t)
			path := writeConfig(t, dir, fmt.Sprintf("server = %q\n# los de casa\ntopic = %q\n",
				newNtfyServer(t, serverCodes{}), c.topics))
			before := readFile(t, path)

			code, out, errOut := runConfigCommand(t, c.args...)
			if code != c.wantCode {
				t.Fatalf("%q = %d; want %d. stdout: %s stderr: %s", c.args, code, c.wantCode, out, errOut)
			}
			after := readFile(t, path)
			switch c.wantCode {
			case exitOK:
				if !strings.Contains(after, c.want) {
					t.Errorf("file = %q; want %q in it", after, c.want)
				}
				if !strings.Contains(after, "# los de casa") {
					t.Errorf("file = %q; the comment was lost", after)
				}
			default:
				if after != before {
					t.Errorf("the file changed on a failure:\nbefore: %s\nafter: %s", before, after)
				}
				if !strings.Contains(errOut, c.wantErr) {
					t.Errorf("stderr = %q; want %q in it", errOut, c.wantErr)
				}
			}
		})
	}
}

// TestConfigPasswordStdinAlone changes only the password: the form a script
// uses to rotate a secret without putting it in the command line.
func TestConfigPasswordStdinAlone(t *testing.T) {
	dir := configDir(t)
	path := writeConfig(t, dir, "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\nuser = \"andrea\"\npassword = \"old\"\n")
	withStdin(t, "nueva\n")

	code, out, errOut := runConfigCommand(t, "config", "--password-stdin")
	if code != exitOK {
		t.Fatalf("config --password-stdin = %d; want 0. stderr: %s", code, errOut)
	}
	if !strings.Contains(readFile(t, path), `password = "nueva"`) {
		t.Errorf("file = %q; want the new password", readFile(t, path))
	}
	if strings.Contains(out, "nueva") || strings.Contains(errOut, "nueva") {
		t.Errorf("output = %q / %q; it exposes the password", out, errOut)
	}
}

// TestConfigCheckReadsOnlyWhatItNeeds proves the command does not spend a
// request per topic more than once: --check makes one health call, one account
// call and one stream per topic, and nothing else.
func TestConfigCheckReadsOnlyWhatItNeeds(t *testing.T) {
	count := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		count["health"]++
		io.WriteString(w, `{"healthy":true}`)
	})
	mux.HandleFunc("/v1/account", func(w http.ResponseWriter, r *http.Request) {
		count["account"]++
		io.WriteString(w, `{"username":"*","role":"anonymous"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		count["read"]++
		io.WriteString(w, `{"event":"open"}`+"\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := configDir(t)
	writeConfig(t, dir, fmt.Sprintf("server = %q\ntopic = \"casa, avisos\"\n", srv.URL))
	if code, _, errOut := runConfigCommand(t, "config", "--check"); code != exitOK {
		t.Fatalf("config --check = %d; want 0. stderr: %s", code, errOut)
	}

	want := map[string]int{"health": 1, "account": 1, "read": 2}
	for route, n := range want {
		if count[route] != n {
			t.Errorf("%s was requested %d times; want %d", route, count[route], n)
		}
	}
}

// TestCatalogCoversTheConfig keeps the msgids of the config command in the
// catalogues: one that is missing reaches the person in English, and the
// Spanish one has to load or it falls back without saying so.
func TestCatalogCoversTheConfig(t *testing.T) {
	cases := []struct {
		locale string
		msgid  string
		want   string
	}{
		{locale: "en", msgid: "nothing was written", want: "nothing was written"},
		{locale: "es", msgid: "nothing was written", want: "no se escribió nada"},
		{locale: "es", msgid: "wrote %s", want: "escribí %s"},
		{locale: "es", msgid: "the server rejected the credentials", want: "el servidor rechazó las credenciales"},
		{locale: "es", msgid: "y/n", want: "s/n"},
		{locale: "es", msgid: "the password is never given as an argument: use --password-stdin",
			want: "la contraseña no va nunca como argumento: usá --password-stdin"},
	}

	for _, c := range cases {
		t.Run(c.locale+"/"+c.msgid, func(t *testing.T) {
			tr := i18n.New(c.locale)
			if got := tr.T(c.msgid); got != c.want {
				t.Errorf("catalog %s for %q = %q; want %q", c.locale, c.msgid, got, c.want)
			}
		})
	}
}
