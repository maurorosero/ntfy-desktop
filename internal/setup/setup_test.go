// SPDX-License-Identifier: GPL-3.0-or-later

package setup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixture = `# my configuration
server    = "https://old.example.org"   # the server
topic     = "casa, pruebas"             # one stream, two topics

user     = "andrea"
password = "s3cr3t"
`

// writeFixture puts content in a new directory and returns the file path.
func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ntfy-desktop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestOpenRefusesAnInvalidFile(t *testing.T) {
	const broken = "topic = \"casa\"\n"
	path := writeFixture(t, broken)

	if _, err := Open(path); err == nil {
		t.Fatalf("Open(%q) = nil; want the loader to refuse the file", path)
	}
	if got := readFile(t, path); got != broken {
		t.Errorf("Open(%q) changed the file to %q; want it untouched", path, got)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat(%q) = %v; want no backup beside a file that was never written", path+".bak", err)
	}
}

func TestOpenWrapsAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := Open(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(%q) = %v; want a wrapped fs.ErrNotExist", path, err)
	}
}

func TestSetOnlyRewritesItsLine(t *testing.T) {
	path := writeFixture(t, fixture)

	draft, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) = %v; want no error", path, err)
	}
	if err := draft.Set("server", "https://new.example.org"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", "server", err)
	}

	// Until Write, the file on disk is exactly what it was.
	if got := readFile(t, path); got != fixture {
		t.Errorf("Set wrote to the disk: %q; want the original %q", got, fixture)
	}

	cfg, err := draft.Config()
	if err != nil {
		t.Fatalf("Config() = %v; want the candidate to load", err)
	}
	if cfg.ServerURL != "https://new.example.org" {
		t.Errorf("Config().ServerURL = %q; want %q", cfg.ServerURL, "https://new.example.org")
	}

	result, err := draft.Write()
	if err != nil {
		t.Fatalf("Write() = %v; want no error", err)
	}
	if result.Path != path {
		t.Errorf("Result.Path = %q; want %q", result.Path, path)
	}
	if want := path + ".bak"; result.Backup != want {
		t.Errorf("Result.Backup = %q; want %q", result.Backup, want)
	}
	if result.Config.ServerURL != "https://new.example.org" {
		t.Errorf("Result.Config.ServerURL = %q; want %q", result.Config.ServerURL, "https://new.example.org")
	}
	if got := readFile(t, result.Backup); got != fixture {
		t.Errorf("the backup holds %q; want the previous %q", got, fixture)
	}

	before := strings.Split(fixture, "\n")
	after := strings.Split(readFile(t, path), "\n")
	if len(before) != len(after) {
		t.Fatalf("the write changed the line count: %d before, %d after", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
			if i != 1 {
				t.Errorf("line %d is %q; want only the server line to change", i+1, after[i])
			}
		}
	}
	if changed != 1 {
		t.Errorf("the write changed %d lines; want 1", changed)
	}
}

func TestTopicsAcrossWrites(t *testing.T) {
	path := writeFixture(t, fixture)

	draft, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) = %v; want no error", path, err)
	}
	if err := draft.AddTopic("avisos"); err != nil {
		t.Fatalf("AddTopic(%q) = %v; want no error", "avisos", err)
	}
	if _, err := draft.Write(); err != nil {
		t.Fatalf("Write() = %v; want no error", err)
	}

	cfg, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) after adding = %v; want no error", path, err)
	}
	if got, err := cfg.Config(); err != nil {
		t.Errorf("Config() after adding = %v; want no error", err)
	} else if want := []string{"casa", "pruebas", "avisos"}; !reflect.DeepEqual(got.Topics, want) {
		t.Errorf("topics after adding = %q; want %q", got.Topics, want)
	}

	if err := cfg.RemoveTopic("casa"); err != nil {
		t.Fatalf("RemoveTopic(%q) = %v; want no error", "casa", err)
	}
	if _, err := cfg.Write(); err != nil {
		t.Fatalf("Write() after removing = %v; want no error", err)
	}

	last, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) after removing = %v; want no error", path, err)
	}
	got, err := last.Config()
	if err != nil {
		t.Fatalf("Config() after removing = %v; want no error", err)
	}
	if want := []string{"pruebas", "avisos"}; !reflect.DeepEqual(got.Topics, want) {
		t.Errorf("topics after removing = %q; want %q", got.Topics, want)
	}
	if line := afterLine(t, path, "topic"); line != `topic     = "pruebas, avisos"             # one stream, two topics` {
		t.Errorf("the topic line is %q; want the comment and the alignment kept", line)
	}
}

// afterLine returns the line of the file that assigns key.
func afterLine(t *testing.T, path, key string) string {
	t.Helper()
	for _, line := range strings.Split(readFile(t, path), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			return line
		}
	}
	t.Fatalf("%s has no line for %q", path, key)
	return ""
}

func TestRemovingTheOnlyTopicKeepsTheFile(t *testing.T) {
	const single = "server = \"https://ntfy.sh\"\ntopic = \"casa\"\n"
	path := writeFixture(t, single)

	draft, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) = %v; want no error", path, err)
	}
	if err := draft.RemoveTopic("casa"); err == nil {
		t.Fatalf("RemoveTopic(%q) = nil; want an error", "casa")
	}
	if got := readFile(t, path); got != single {
		t.Errorf("the file is %q; want the original %q", got, single)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat(%q) = %v; want no backup when nothing was written", path+".bak", err)
	}
}

func TestNewWritesWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ntfy-desktop", "config.toml")

	draft := New(path)
	if err := draft.Set("server", "https://ntfy.sh"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", "server", err)
	}
	if err := draft.Set("topic", "alerts"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", "topic", err)
	}

	result, err := draft.Write()
	if err != nil {
		t.Fatalf("Write() = %v; want no error", err)
	}
	if result.Backup != "" {
		t.Errorf("Result.Backup = %q; want none for a new file", result.Backup)
	}
	if got := readFile(t, path); got != "server = \"https://ntfy.sh\"\ntopic = \"alerts\"\n" {
		t.Errorf("the new file is %q; want the two keys", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the new file has mode %04o; want 0600", got)
	}
}

func TestWriteRefusesAnIncompleteDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	draft := New(path)
	if err := draft.Set("server", "https://ntfy.sh"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", "server", err)
	}
	if _, err := draft.Write(); err == nil {
		t.Fatalf("Write() = nil; want the loader to refuse a draft without a topic")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat(%q) = %v; want no file for a candidate that does not load", path, err)
	}
}

func TestSetRefusesAnUnknownKey(t *testing.T) {
	path := writeFixture(t, fixture)

	draft, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) = %v; want no error", path, err)
	}
	if err := draft.Set("secret", "x"); err == nil {
		t.Fatalf("Set(%q) = nil; want an error", "secret")
	}
	if got := string(draft.Text()); got != fixture {
		t.Errorf("the draft is %q; want the original %q", got, fixture)
	}
}
