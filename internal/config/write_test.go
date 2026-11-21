// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const initial = "# the file the user wrote\nserver = \"https://old.example.org\"   # where it points\ntopic = \"casa\"\n"

// tempFiles lists what a failed or finished write could have left beside the
// configuration.
func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatalf("globbing the temporary files: %v", err)
	}
	return names
}

func TestWriteKeepsThePreviousVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	doc := NewDocument([]byte(initial))
	if err := doc.Set(keyServer, "https://new.example.org"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyServer, err)
	}
	backup, err := doc.Write(path)
	if err != nil {
		t.Fatalf("Write(%q) = %v; want no error", path, err)
	}

	if want := path + ".bak"; backup != want {
		t.Errorf("Write(%q) = backup %q; want %q", path, backup, want)
	}
	previous, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("reading the backup: %v", err)
	}
	if string(previous) != initial {
		t.Errorf("the backup holds %q; want the previous %q", previous, initial)
	}

	for _, name := range []string{path, backup} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat(%q): %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s has mode %04o; want 0600", filepath.Base(name), got)
		}
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) = %v; want the new file to load", path, err)
	}
	if cfg.ServerURL != "https://new.example.org" {
		t.Errorf("Load(%q).ServerURL = %q; want %q", path, cfg.ServerURL, "https://new.example.org")
	}
	want := strings.Replace(initial, "https://old.example.org", "https://new.example.org", 1)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the new file: %v", err)
	}
	if string(got) != want {
		t.Errorf("the file is %q; want %q", got, want)
	}
	if left := tempFiles(t, filepath.Dir(path)); len(left) != 0 {
		t.Errorf("Write left temporary files: %q", left)
	}
}

func TestWriteCreatesTheFileAndItsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")

	doc := NewDocument(nil)
	if err := doc.Set(keyServer, "https://ntfy.sh"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyServer, err)
	}
	if err := doc.Set(keyTopic, "alerts"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyTopic, err)
	}
	backup, err := doc.Write(path)
	if err != nil {
		t.Fatalf("Write(%q) = %v; want no error", path, err)
	}
	if backup != "" {
		t.Errorf("Write(%q) = backup %q; want none, there was no previous file", path, backup)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the new file has mode %04o; want 0600", got)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("Load(%q) = %v; want the new file to load", path, err)
	}
}

func TestWriteRefusesAnInvalidCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	doc := NewDocument([]byte(initial))
	if err := doc.Set(keyServer, "not a url"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyServer, err)
	}
	if _, err := doc.Write(path); err == nil {
		t.Fatalf("Write(%q) = nil; want the loader to refuse the value", path)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if string(got) != initial {
		t.Errorf("the file is %q; want the original %q", got, initial)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat(%q) = %v; want no backup for a candidate that never loaded", path+".bak", err)
	}
	if left := tempFiles(t, dir); len(left) != 0 {
		t.Errorf("Write left temporary files: %q", left)
	}
}

func TestWriteFailureLeavesTheOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	doc := NewDocument([]byte(initial))
	if err := doc.Set(keyServer, "https://new.example.org"); err != nil {
		t.Fatalf("Set(%q) = %v; want no error", keyServer, err)
	}

	failingRename := func(string, string) error { return errors.New("the disk said no") }
	if _, err := writeFile(path, doc.Bytes(), failingRename); err == nil {
		t.Fatalf("writeFile(%q) = nil; want the rename failure", path)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if string(got) != initial {
		t.Errorf("after a failed write the file is %q; want the original %q", got, initial)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) = %v; want the original file to still load", path, err)
	}
	if cfg.ServerURL != "https://old.example.org" {
		t.Errorf("Load(%q).ServerURL = %q; want the original %q", path, cfg.ServerURL, "https://old.example.org")
	}
	if left := tempFiles(t, dir); len(left) != 0 {
		t.Errorf("the failed write left temporary files: %q", left)
	}
}
