// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Config
	}{
		{
			name: "anonymous topic",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}},
		},
		{
			name: "user and password",
			in:   "server = \"https://ntfy.example.org\"\ntopic = \"builds\"\nuser = \"andrea\"\npassword = \"s3cr3t\"\n",
			want: Config{ServerURL: "https://ntfy.example.org", Topics: []string{"builds"}, User: "andrea", Password: "s3cr3t"},
		},
		{
			name: "comments, blank lines and crlf",
			in:   "# my server\r\n\r\nserver = \"https://ntfy.sh\"  # trailing comment\r\ntopic = \"alerts\"\r\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}},
		},
		{
			name: "trailing slash is removed",
			in:   "server = \"https://ntfy.sh/\"\ntopic = \"alerts\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}},
		},
		{
			name: "server with a path prefix",
			in:   "server = \"https://example.org/ntfy\"\ntopic = \"a_b-1\"\n",
			want: Config{ServerURL: "https://example.org/ntfy", Topics: []string{"a_b-1"}},
		},
		{
			name: "escapes in the password",
			in:   "server = \"http://localhost:2586\"\ntopic = \"alerts\"\nuser = \"u\"\npassword = \"a\\\"b\\\\c d\"\n",
			want: Config{ServerURL: "http://localhost:2586", Topics: []string{"alerts"}, User: "u", Password: "a\"b\\c d"},
		},
		{
			name: "log level",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nlog-level = \"debug\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}, LogLevel: "debug"},
		},
		{
			name: "two topics",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"casa,pruebas-multi\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"casa", "pruebas-multi"}},
		},
		{
			name: "two topics with spaces around them",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"casa, pruebas-multi\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"casa", "pruebas-multi"}},
		},
		{
			name: "three topics keep the written order",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"zeta, alfa , medio\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"zeta", "alfa", "medio"}},
		},
		{
			name: "log level in another case",
			in:   "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nlog-level = \"WARN\"\n",
			want: Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}, LogLevel: "WARN"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse([]byte(c.in))
			if err != nil {
				t.Fatalf("parse(%q) = error %v; want no error", c.in, err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("parse(%q) = %+v; want %+v", c.in, *got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantMsg string
	}{
		{
			name:    "empty file",
			in:      "",
			wantMsg: "server: must not be empty",
		},
		{
			name:    "missing topic",
			in:      "server = \"https://ntfy.sh\"\n",
			wantMsg: "topic: must not be empty",
		},
		{
			name:    "empty server",
			in:      "server = \"\"\ntopic = \"alerts\"\n",
			wantMsg: "server: must not be empty",
		},
		{
			name:    "server without scheme",
			in:      "server = \"ntfy.sh\"\ntopic = \"alerts\"\n",
			wantMsg: "server: must be an http or https URL",
		},
		{
			name:    "server with another scheme",
			in:      "server = \"ftp://ntfy.sh\"\ntopic = \"alerts\"\n",
			wantMsg: "server: must be an http or https URL",
		},
		{
			name:    "server with a query",
			in:      "server = \"https://ntfy.sh/?auth=x\"\ntopic = \"alerts\"\n",
			wantMsg: "server: must not have a query or a fragment",
		},
		{
			name:    "topic with a slash",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"a/b\"\n",
			wantMsg: "topic: must be 1 to 64 characters of letters, digits, dashes or underscores",
		},
		{
			name:    "topic too long",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"" + strings.Repeat("a", 65) + "\"\n",
			wantMsg: "topic: must be 1 to 64 characters of letters, digits, dashes or underscores",
		},
		{
			name:    "empty topic in the list",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa,,x\"\n",
			wantMsg: `topic: "" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "trailing comma names the empty topic",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa, x ,\"\n",
			wantMsg: `topic: "" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "empty topic value",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"\"\n",
			wantMsg: "topic: must not be empty",
		},
		{
			name:    "topic value of only spaces",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"   \"\n",
			wantMsg: "topic: must not be empty",
		},
		{
			name:    "second topic too long",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa," + strings.Repeat("a", 65) + "\"\n",
			wantMsg: `topic: "` + strings.Repeat("a", 65) + `" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "second topic with a strange character",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa,pruebas/multi\"\n",
			wantMsg: `topic: "pruebas/multi" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "second topic with a comma inside quotes",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"casa,pruebas multi\"\n",
			wantMsg: `topic: "pruebas multi" is not a valid topic: use 1 to 64 characters of letters, digits, dashes or underscores`,
		},
		{
			name:    "user with a colon",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nuser = \"a:b\"\npassword = \"x\"\n",
			wantMsg: "user: must not contain a colon",
		},
		{
			name:    "password without user",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\npassword = \"x\"\n",
			wantMsg: "user: requires a user name",
		},
		{
			name:    "user without password",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nuser = \"andrea\"\n",
			wantMsg: "password: requires a password",
		},
		{
			name:    "password with a control character",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nuser = \"andrea\"\npassword = \"a\\tb\"\n",
			wantMsg: "password: must not contain control characters",
		},
		{
			name:    "unknown log level",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nlog-level = \"trace\"\n",
			wantMsg: "log-level: must be debug, info, warn or error",
		},
		{
			name:    "line without assignment",
			in:      "server = \"https://ntfy.sh\"\nhello\n",
			wantMsg: "line 2: expected key = value",
		},
		{
			name:    "key with a space",
			in:      "ser ver = \"https://ntfy.sh\"\n",
			wantMsg: "line 1: invalid key",
		},
		{
			name:    "value without quotes",
			in:      "server = https://ntfy.sh\n",
			wantMsg: "line 1: value must be a quoted string",
		},
		{
			name:    "unterminated value",
			in:      "server = \"https://ntfy.sh\ntopic = \"alerts\"\n",
			wantMsg: "line 1: unterminated quoted value",
		},
		{
			name:    "text after the closing quote",
			in:      "server = \"https://ntfy.sh\" x\n",
			wantMsg: "line 1: unexpected text after the closing quote",
		},
		{
			name:    "unknown escape",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"a\\qb\"\n",
			wantMsg: "line 2: invalid escape sequence",
		},
		{
			name:    "duplicated key",
			in:      "server = \"https://ntfy.sh\"\nserver = \"https://other.sh\"\ntopic = \"alerts\"\n",
			wantMsg: `line 2: duplicate key "server"`,
		},
		{
			name:    "unsupported key",
			in:      "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\nsecret = \"x\"\n",
			wantMsg: `line 3: unsupported key "secret"`,
		},
		{
			name:    "sections are not supported yet",
			in:      "[auth]\nuser = \"andrea\"\n",
			wantMsg: "line 1: expected key = value",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse([]byte(c.in))
			if err == nil {
				t.Fatalf("parse(%q) = nil; want the error %q", c.in, c.wantMsg)
			}
			if got := err.Error(); got != c.wantMsg {
				t.Errorf("parse(%q) = %q; want %q", c.in, got, c.wantMsg)
			}

			var vErr *ValidationError
			var sErr *SyntaxError
			switch {
			case errors.As(err, &vErr):
			case errors.As(err, &sErr):
			default:
				t.Errorf("parse(%q) returned %T; want *ValidationError or *SyntaxError", c.in, err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(good, []byte("server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	bad := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(bad, []byte("topic = \"alerts\"\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	cfg, err := Load(good)
	if err != nil {
		t.Fatalf("Load(%q) = error %v; want no error", good, err)
	}
	if cfg.Topic() != "alerts" {
		t.Errorf("Load(%q).Topic = %q; want %q", good, cfg.Topic(), "alerts")
	}

	_, err = Load(bad)
	if err == nil {
		t.Fatalf("Load(%q) = nil; want a validation error", bad)
	}
	if !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), "server: must not be empty") {
		t.Errorf("Load(%q) = %q; want the path and the reason", bad, err)
	}

	_, err = Load(filepath.Join(dir, "missing.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load(missing file) = %v; want a wrapped fs.ErrNotExist", err)
	}
}

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() = error %v; want no error", err)
	}
	want := filepath.Join(home, "ntfy-desktop", "config.toml")
	if got != want {
		t.Errorf("DefaultPath() = %q; want %q", got, want)
	}
}

func TestTopic(t *testing.T) {
	cases := []struct {
		name   string
		topics []string
		want   string
	}{
		{name: "one topic", topics: []string{"casa"}, want: "casa"},
		{name: "several topics have no single one", topics: []string{"casa", "pruebas"}, want: ""},
		{name: "no topics", topics: nil, want: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Config{Topics: c.topics}
			if got := cfg.Topic(); got != c.want {
				t.Errorf("Config{Topics: %q}.Topic() = %q; want %q", c.topics, got, c.want)
			}
		})
	}
}

func TestAuthMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want AuthMode
	}{
		{name: "anonymous", cfg: Config{ServerURL: "https://ntfy.sh", Topics: []string{"t"}}, want: AuthNone},
		{name: "user and password", cfg: Config{User: "andrea", Password: "x"}, want: AuthBasic},
		{name: "password only", cfg: Config{Password: "x"}, want: AuthNone},
		{name: "user only", cfg: Config{User: "andrea"}, want: AuthNone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.AuthMode(); got != c.want {
				t.Errorf("Config{%q, %q}.AuthMode() = %d; want %d",
					c.cfg.User, c.cfg.Password, got, c.want)
			}
		})
	}
}

func TestStringDoesNotLeakPassword(t *testing.T) {
	cfg := Config{ServerURL: "https://ntfy.sh", Topics: []string{"alerts"}, User: "andrea", Password: "s3cr3t"}

	for _, format := range []string{"%v", "%s"} {
		got := fmt.Sprintf(format, cfg)
		if strings.Contains(got, "s3cr3t") {
			t.Errorf("fmt.Sprintf(%q, cfg) = %q; must not contain the password", format, got)
		}
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n",
		"server = \"https://ntfy.sh\"\ntopic = \"a\\\"b\"\n",
		"server = \"https://ntfy.sh\"\ntopic = \"casa, pruebas\"\n",
		"server = \"https://ntfy.sh\"\ntopic = \"casa,,x\"\n",
		"server = \"",
		"= \"x\"",
		"server = \"https://ntfy.sh\"\r\ntopic = \"alerts\"\r\n",
		"# comment\n\nkey = \"value\"\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		cfg, err := parse([]byte(in))
		if err != nil {
			var vErr *ValidationError
			var sErr *SyntaxError
			if !errors.As(err, &vErr) && !errors.As(err, &sErr) {
				t.Fatalf("parse(%q) returned %T; want a typed error of this package", in, err)
			}
			return
		}
		if err := cfg.validate(); err != nil {
			t.Fatalf("parse(%q) accepted %+v and validate() rejects it: %v", in, *cfg, err)
		}
	})
}

// TestCheckServer is the rule of the loader on its own, which is what a front
// end that asks for the URL one field at a time calls.
func TestCheckServer(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "https", in: "https://ntfy.sh", want: "https://ntfy.sh"},
		{name: "http with a port", in: "http://10.0.0.5:2586", want: "http://10.0.0.5:2586"},
		{name: "a trailing slash is trimmed", in: "https://ntfy.sh/", want: "https://ntfy.sh"},
		{name: "two trailing slashes", in: "https://ntfy.sh//", want: "https://ntfy.sh"},
		{name: "a path prefix is kept", in: "https://example.org/ntfy", want: "https://example.org/ntfy"},
		{name: "empty", in: "", wantErr: "must not be empty"},
		{name: "only slashes", in: "///", wantErr: "must not be empty"},
		{name: "ftp", in: "ftp://ntfy.sh", wantErr: "must be an http or https URL"},
		{name: "no scheme", in: "ntfy.sh", wantErr: "must be an http or https URL"},
		{name: "no host", in: "https://", wantErr: "must be an http or https URL"},
		{name: "a query", in: "https://ntfy.sh?x=1", wantErr: "must not have a query or a fragment"},
		{name: "a fragment", in: "https://ntfy.sh#top", wantErr: "must not have a query or a fragment"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CheckServer(c.in)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("CheckServer(%q) = %q; want an error", c.in, got)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("CheckServer(%q) = %v; want %q in it", c.in, err, c.wantErr)
				}
				if got != "" {
					t.Errorf("CheckServer(%q) = %q; want \"\" with an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckServer(%q) = error %v; want no error", c.in, err)
			}
			if got != c.want {
				t.Errorf("CheckServer(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCheckTopic(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "a name", in: "casa"},
		{name: "dashes and underscores", in: "a_b-1"},
		{name: "64 characters", in: strings.Repeat("a", 64)},
		{name: "empty", in: "", wantErr: true},
		{name: "65 characters", in: strings.Repeat("a", 65), wantErr: true},
		{name: "a space", in: "mis avisos", wantErr: true},
		{name: "a comma", in: "casa,avisos", wantErr: true},
		{name: "a slash", in: "casa/avisos", wantErr: true},
		{name: "a newline", in: "casa\n", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckTopic(c.in)
			if c.wantErr && err == nil {
				t.Fatalf("CheckTopic(%q) = nil; want an error", c.in)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("CheckTopic(%q) = %v; want no error", c.in, err)
			}
		})
	}
}
