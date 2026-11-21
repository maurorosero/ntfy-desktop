// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/config"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// scriptedPrompter answers the wizard with fixed values: no test reads the
// terminal, and every question the wizard asks is data of the case.
type scriptedPrompter struct {
	lines    []string
	secrets  []string
	yes      []bool
	cancelAt int // when > 0, the answer with this number is a cancellation
	asked    int

	notes []string
}

func (s *scriptedPrompter) answer() error {
	s.asked++
	if s.cancelAt > 0 && s.asked == s.cancelAt {
		return ErrCancelled
	}
	return nil
}

func (s *scriptedPrompter) Line(question, current string) (string, error) {
	if err := s.answer(); err != nil {
		return "", err
	}
	if len(s.lines) == 0 {
		return "", errors.New("the script ran out of answers")
	}
	answer := s.lines[0]
	s.lines = s.lines[1:]
	return answer, nil
}

func (s *scriptedPrompter) Secret(question string) (string, error) {
	if err := s.answer(); err != nil {
		return "", err
	}
	if len(s.secrets) == 0 {
		return "", errors.New("the script ran out of secrets")
	}
	answer := s.secrets[0]
	s.secrets = s.secrets[1:]
	return answer, nil
}

func (s *scriptedPrompter) YesNo(question string, def bool) (bool, error) {
	if err := s.answer(); err != nil {
		return false, err
	}
	if len(s.yes) == 0 {
		return def, nil
	}
	answer := s.yes[0]
	s.yes = s.yes[1:]
	return answer, nil
}

func (s *scriptedPrompter) Note(text string) { s.notes = append(s.notes, text) }

// said is everything the wizard printed, in order.
func (s *scriptedPrompter) said() string { return strings.Join(s.notes, "\n") }

// probe is the verification the wizard runs, over the real Probe pointed at a
// server of the test.
func probe(cfg config.Config) Prober { return ntfy.NewProbe(cfg) }

func TestWizardWritesAnAnonymousConfiguration(t *testing.T) {
	dir := configDir(t)
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	srv := newNtfyServer(t, serverCodes{})

	pr := &scriptedPrompter{
		lines: []string{srv, "casa, avisos"},
		yes:   []bool{false, true},
	}
	if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != exitOK {
		t.Fatalf("wizard = %d; want 0. said: %s", code, pr.said())
	}

	file := readFile(t, path)
	for _, want := range []string{`server = "` + srv + `"`, `topic = "casa, avisos"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file = %q; want %q in it", file, want)
		}
	}
	if strings.Contains(file, "user") {
		t.Errorf("file = %q; an anonymous configuration has no user line", file)
	}
	if !strings.Contains(pr.said(), "this is what will be written") {
		t.Errorf("said = %q; want the summary of the file", pr.said())
	}
}

func TestWizardWritesCredentialsWithoutShowingThem(t *testing.T) {
	dir := configDir(t)
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	srv := newNtfyServer(t, serverCodes{body: `{"username":"andrea","role":"user"}`})

	pr := &scriptedPrompter{
		lines:   []string{srv, "andrea", "casa"},
		secrets: []string{"malpass-9z"},
		yes:     []bool{true, true},
	}
	if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != exitOK {
		t.Fatalf("wizard = %d; want 0. said: %s", code, pr.said())
	}

	file := readFile(t, path)
	for _, want := range []string{`user = "andrea"`, `password = "malpass-9z"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file = %q; want %q in it", file, want)
		}
	}
	if strings.Contains(pr.said(), "malpass-9z") {
		t.Errorf("said = %q; it exposes the password", pr.said())
	}
	if !strings.Contains(pr.said(), maskedPassword) {
		t.Errorf("said = %q; want the mask in the summary", pr.said())
	}
}

// TestWizardSavesWithoutTheServer is the decision of the plan: a server that
// does not answer is a warning in the summary and the file is written anyway.
func TestWizardSavesWithoutTheServer(t *testing.T) {
	dir := configDir(t)
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")

	pr := &scriptedPrompter{
		lines: []string{deadServer(t), "casa"},
		yes:   []bool{false, true},
	}
	if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != exitOK {
		t.Fatalf("wizard = %d; want 0. said: %s", code, pr.said())
	}
	if !strings.Contains(pr.said(), "warning") {
		t.Errorf("said = %q; want the warning", pr.said())
	}
	if readFile(t, path) == "" {
		t.Error("the file was not written")
	}
}

func TestWizardStopsOnTheCasesThatBlock(t *testing.T) {
	cases := []struct {
		name     string
		codes    serverCodes
		want     int
		wantSaid string
	}{
		{
			name:     "a server that is not ntfy",
			codes:    serverCodes{health: http.StatusNotFound},
			want:     exitServer,
			wantSaid: "does not look like an ntfy server",
		},
		{
			name:     "credentials the server rejects",
			codes:    serverCodes{account: http.StatusUnauthorized},
			want:     exitCredentials,
			wantSaid: "refused the credentials",
		},
		{
			name:     "a topic the credentials cannot read",
			codes:    serverCodes{read: http.StatusForbidden},
			want:     exitPermission,
			wantSaid: "cannot be read",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := configDir(t)
			path := filepath.Join(dir, "ntfy-desktop", "config.toml")
			srv := newNtfyServer(t, c.codes)

			pr := &scriptedPrompter{}
			switch c.want {
			case exitServer:
				// Three attempts at the URL, and the anonymous answer first.
				pr.lines = []string{srv, srv, srv}
				pr.yes = []bool{false}
			case exitCredentials:
				// Three attempts at the pair, each with the same wrong secret.
				pr.lines = []string{srv, "andrea", "andrea", "andrea"}
				pr.secrets = []string{"malpass-9z", "malpass-9z", "malpass-9z"}
				pr.yes = []bool{true}
			default:
				pr.lines = []string{srv, "casa"}
				pr.yes = []bool{false}
			}

			if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != c.want {
				t.Fatalf("wizard = %d; want %d. said: %s", code, c.want, pr.said())
			}
			if !strings.Contains(pr.said(), c.wantSaid) {
				t.Errorf("said = %q; want %q in it", pr.said(), c.wantSaid)
			}
			if !strings.Contains(pr.said(), "nothing was written") {
				t.Errorf("said = %q; want the line that says nothing was written", pr.said())
			}
			if readFile(t, path) != "" {
				t.Errorf("file = %q; want nothing written", readFile(t, path))
			}
			if strings.Contains(pr.said(), "malpass-9z") {
				t.Errorf("said = %q; it exposes the password", pr.said())
			}
		})
	}
}

// TestWizardCancelLeavesTheFileAlone walks the wizard and stops it at each
// question in turn: the file has the same digest every time.
func TestWizardCancelLeavesTheFileAlone(t *testing.T) {
	dir := configDir(t)
	path := writeConfig(t, dir, "# la de casa\nserver = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
	srv := newNtfyServer(t, serverCodes{})
	before := digest(t, path)

	for stop := 1; stop <= 4; stop++ {
		t.Run(fmt.Sprintf("stop at question %d", stop), func(t *testing.T) {
			pr := &scriptedPrompter{
				lines:    []string{srv, "casa, avisos"},
				yes:      []bool{false, true},
				cancelAt: stop,
			}
			code := runWizard(context.Background(), path, pr, probe, testTranslator())
			if code != exitNothing {
				t.Fatalf("wizard cancelled at %d = %d; want %d. said: %s", stop, code, exitNothing, pr.said())
			}
			if !strings.Contains(pr.said(), "nothing was written") {
				t.Errorf("said = %q; want the line that says nothing was written", pr.said())
			}
			if after := digest(t, path); after != before {
				t.Errorf("the file changed: %s -> %s", before, after)
			}
		})
	}
}

func TestWizardRefusesAnInvalidFile(t *testing.T) {
	dir := configDir(t)
	path := writeConfig(t, dir, "server = \"https://ntfy.sh\"\ntopic = \"casa\"\ntopics = \"x\"\n")
	before := digest(t, path)

	pr := &scriptedPrompter{}
	code := runWizard(context.Background(), path, pr, probe, testTranslator())
	if code != exitFile {
		t.Fatalf("wizard over an invalid file = %d; want %d. said: %s", code, exitFile, pr.said())
	}
	if !strings.Contains(pr.said(), "cannot be edited") {
		t.Errorf("said = %q; want the file reported as not editable", pr.said())
	}
	if after := digest(t, path); after != before {
		t.Errorf("the file changed: %s -> %s", before, after)
	}
}

// TestWizardRemovesThePair covers the way back: a configuration with
// credentials becomes anonymous, and the pair is emptied instead of left half
// written.
func TestWizardRemovesThePair(t *testing.T) {
	dir := configDir(t)
	path := writeConfig(t, dir, "server = \"https://ntfy.invalid\"\ntopic = \"casa\"\nuser = \"andrea\"\npassword = \"vieja\"\n")
	srv := newNtfyServer(t, serverCodes{})

	pr := &scriptedPrompter{
		lines: []string{srv, "casa"},
		yes:   []bool{false, true},
	}
	if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != exitOK {
		t.Fatalf("wizard = %d; want 0. said: %s", code, pr.said())
	}
	file := readFile(t, path)
	for _, want := range []string{`user = ""`, `password = ""`} {
		if !strings.Contains(file, want) {
			t.Errorf("file = %q; want %q in it", file, want)
		}
	}
	if strings.Contains(file, "vieja") {
		t.Errorf("file = %q; the old password is still there", file)
	}
}

// TestWizardReAsksWithoutWriting checks the step that re-asks: a value the
// loader refuses is asked again, and the second answer is the one used.
func TestWizardReAsksWithoutWriting(t *testing.T) {
	dir := configDir(t)
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	srv := newNtfyServer(t, serverCodes{})

	pr := &scriptedPrompter{
		lines: []string{"no es una url", srv, "con espacio", "casa"},
		yes:   []bool{false, true},
	}
	if code := runWizard(context.Background(), path, pr, probe, testTranslator()); code != exitOK {
		t.Fatalf("wizard = %d; want 0. said: %s", code, pr.said())
	}
	file := readFile(t, path)
	for _, want := range []string{`server = "` + srv + `"`, `topic = "casa"`} {
		if !strings.Contains(file, want) {
			t.Errorf("file = %q; want %q in it", file, want)
		}
	}
}
