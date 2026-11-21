// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// fakeDialog writes a shell script that stands in for kdialog or zenity. No
// test of this package opens a real dialog: the script reads the same
// arguments and exits with the same codes, and it logs both so a case can
// assert what the adapter asked for.
func fakeDialog(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing the fake dialog: %v", err)
	}
	return path
}

// logPath puts the argv log of the fake dialog in a temporary file.
func logPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv")
	t.Setenv("NTFY_DIALOG_LOG", path)
	return path
}

// argv reads the arguments of the last run of the fake dialog, one per line.
func argv(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log of the fake dialog: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestKDialogPromptLine(t *testing.T) {
	log := logPath(t)
	tool := fakeDialog(t, "kdialog", `printf '%s\n' "$@" >> "$NTFY_DIALOG_LOG"
printf '  andrea  \n'`)

	p := NewKDialogPrompt(tool, source{})
	p.Step(2, 5, "Credentials")

	got, err := p.Line(context.Background(), "user name:", "andrea")
	if err != nil {
		t.Fatalf("Line() = %v; want no error", err)
	}
	if got != "  andrea  " {
		t.Errorf("Line() = %q; want %q", got, "  andrea  ")
	}

	want := []string{
		"--title", "Configure ntfy-desktop — 2 of 5 · Credentials",
		"--ok-label", "Continue", "--cancel-label", "Cancel",
		"--inputbox", "user name:", "andrea",
	}
	if got := argv(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("argv =\n  %q\nwant\n  %q", got, want)
	}
}

// TestKDialogPromptCodes reads the codes of kdialog the way the wizard sees
// them: a value, a no, a cancel and a broken tool.
func TestKDialogPromptCodes(t *testing.T) {
	cases := []struct {
		name      string
		code      int
		want      bool
		cancelled bool
		wantErr   bool
	}{
		{name: "yes", code: 0, want: true},
		{name: "no", code: 1, want: false},
		{name: "cancel", code: 2, cancelled: true},
		{name: "something else", code: 7, wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool := fakeDialog(t, "kdialog", "exit "+strconv.Itoa(c.code))
			p := NewKDialogPrompt(tool, source{})

			got, err := p.YesNo(context.Background(), "does it?", true)
			switch {
			case c.cancelled:
				if !errors.Is(err, ErrPromptCancelled) {
					t.Fatalf("YesNo() = %v; want ErrPromptCancelled", err)
				}
			case c.wantErr:
				if err == nil {
					t.Fatalf("YesNo() = %v, nil; want an error", got)
				}
			default:
				if err != nil {
					t.Fatalf("YesNo() = %v; want no error", err)
				}
				if got != c.want {
					t.Errorf("YesNo() = %v; want %v", got, c.want)
				}
			}
		})
	}
}

// TestKDialogPromptCancelsInput covers the dialogs that take a value: kdialog
// exits 1 when the field is closed and 2 when the window is cancelled, and both
// are the person stopping the interview.
func TestKDialogPromptCancelsInput(t *testing.T) {
	for _, code := range []int{1, 2} {
		t.Run("exit "+strconv.Itoa(code), func(t *testing.T) {
			tool := fakeDialog(t, "kdialog", "exit "+strconv.Itoa(code))
			p := NewKDialogPrompt(tool, source{})
			if _, err := p.Line(context.Background(), "server URL:", ""); !errors.Is(err, ErrPromptCancelled) {
				t.Errorf("Line() with exit %d = %v; want ErrPromptCancelled", code, err)
			}
			if _, err := p.Secret(context.Background(), "password:"); !errors.Is(err, ErrPromptCancelled) {
				t.Errorf("Secret() with exit %d = %v; want ErrPromptCancelled", code, err)
			}
		})
	}
}

// TestKDialogPromptConfirmShowsTheBody proves the summary reaches the dialog
// through a file, and that the file does not outlive the step.
func TestKDialogPromptConfirmShowsTheBody(t *testing.T) {
	log := logPath(t)
	tool := fakeDialog(t, "kdialog", `printf '%s\n' "$*" >> "$NTFY_DIALOG_LOG"
prev=""
for a in "$@"; do
	if [ "$prev" = "--textbox" ]; then cat "$a" >> "$NTFY_DIALOG_LOG"; fi
	prev="$a"
done
case " $* " in *" --textbox "*) exit 0;; esac
exit 0`)

	p := NewKDialogPrompt(tool, source{})
	p.Step(5, 5, "Summary")
	ok, err := p.Confirm(context.Background(), "write the configuration?", "server = \"https://ntfy.example\"")
	if err != nil {
		t.Fatalf("Confirm() = %v; want no error", err)
	}
	if !ok {
		t.Error("Confirm() = false; want true")
	}
	logged := strings.Join(argv(t, log), "\n")
	if !strings.Contains(logged, `server = "https://ntfy.example"`) {
		t.Errorf("log = %q; want the body read from the file", logged)
	}
	if !strings.Contains(logged, "--yesno") || !strings.Contains(logged, "write the configuration?") {
		t.Errorf("log = %q; want the question in a --yesno", logged)
	}
}

// TestKDialogPromptTopics walks the checklist and the field that adds a name:
// the configured ones come back marked, and the added one is appended.
func TestKDialogPromptTopics(t *testing.T) {
	log := logPath(t)
	count := filepath.Join(t.TempDir(), "count")
	t.Setenv("NTFY_DIALOG_COUNT", count)
	tool := fakeDialog(t, "kdialog", `printf '%s\n' "$*" >> "$NTFY_DIALOG_LOG"
n=$(cat "$NTFY_DIALOG_COUNT" 2>/dev/null || echo 0)
case " $* " in
*" --checklist "*) printf 'casa\n'; exit 0;;
*" --inputbox "*)
	n=$((n+1)); echo "$n" > "$NTFY_DIALOG_COUNT"
	case "$n" in
	1) printf 'avisos\n';;
	*) printf '\n';;
	esac
	exit 0;;
esac
exit 0`)

	p := NewKDialogPrompt(tool, source{})
	got, err := p.Topics(context.Background(), []string{"casa", "alertas"})
	if err != nil {
		t.Fatalf("Topics() = %v; want no error", err)
	}
	if want := []string{"casa", "avisos"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Topics() = %q; want %q", got, want)
	}

	logged := strings.Join(argv(t, log), " ")
	for _, want := range []string{"--checklist", "--separate-output", "casa casa on", "alertas alertas on"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log = %q; want %q in it", logged, want)
		}
	}
}

// TestZenityPromptYesNoIsNotACancel is the trap of the brief: zenity exits 1
// for its cancel button and, without ZENITY_EXTRA, also for the extra button.
// With our environment the extra button exits 2, so "no" is not a cancel, and
// the labels swap with def so Enter always answers what the caller proposed.
func TestZenityPromptYesNoIsNotACancel(t *testing.T) {
	cases := []struct {
		name     string
		def      bool
		code     int
		want     bool
		wantOK   string
		wantElse string
	}{
		{name: "the answer proposed is yes and Enter answers it", def: true, code: 0, want: true, wantOK: "Yes", wantElse: "No"},
		{name: "the other answer comes from the extra button", def: true, code: 2, want: false, wantOK: "Yes", wantElse: "No"},
		{name: "the answer proposed is no and Enter answers it", def: false, code: 0, want: false, wantOK: "No", wantElse: "Yes"},
		{name: "the other answer comes from the extra button", def: false, code: 2, want: true, wantOK: "No", wantElse: "Yes"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := logPath(t)
			t.Setenv("ZENITY_EXTRA", "5") // a value of the session must not win
			tool := fakeDialog(t, "zenity", `[ "$ZENITY_EXTRA" = "2" ] || exit 9
printf '%s\n' "$*" >> "$NTFY_DIALOG_LOG"
exit `+strconv.Itoa(c.code))

			p := NewZenityPrompt(tool, source{})
			got, err := p.YesNo(context.Background(), "does it use a pair?", c.def)
			if err != nil {
				t.Fatalf("YesNo() = %v; want no error", err)
			}
			if got != c.want {
				t.Errorf("YesNo(def=%v) with exit %d = %v; want %v", c.def, c.code, got, c.want)
			}
			logged := strings.Join(argv(t, log), " ")
			for _, want := range []string{"--extra-button " + c.wantElse, "--ok-label " + c.wantOK} {
				if !strings.Contains(logged, want) {
					t.Errorf("log = %q; want %q in it", logged, want)
				}
			}
		})
	}
}

func TestZenityPromptCancelIsOne(t *testing.T) {
	tool := fakeDialog(t, "zenity", "exit 1")
	p := NewZenityPrompt(tool, source{})
	if _, err := p.Line(context.Background(), "server URL:", ""); !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("Line() = %v; want ErrPromptCancelled", err)
	}
	// The summary window is the first of the two of Confirm: closing it is the
	// person stopping the step, which is a cancellation and not a "no".
	if _, err := p.Confirm(context.Background(), "write?", "body"); !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("Confirm() = %v; want ErrPromptCancelled", err)
	}
}

func TestReportLines(t *testing.T) {
	report := ntfy.Report{
		Health:  ntfy.Result{Step: ntfy.StepHealth, State: ntfy.StateOK},
		Account: ntfy.Result{Step: ntfy.StepAccount, State: ntfy.StateUnverifiable, Err: errors.New("no route")},
		Read: []ntfy.Result{
			{Step: ntfy.StepRead, Topic: "casa", State: ntfy.StateOK},
			{Step: ntfy.StepRead, Topic: "avisos", State: ntfy.StateFailed, Err: errors.New("403")},
		},
	}

	lines := ReportLines(report, "https://ntfy.example", source{})
	if len(lines) != 4 {
		t.Fatalf("ReportLines() = %d lines; want 4", len(lines))
	}
	want := []string{
		"server: ok — https://ntfy.example answers as an ntfy server",
		"credentials: unverified — no route",
		"topic casa: ok",
		"topic avisos: failed — 403",
	}
	for i, line := range lines {
		if got := line.String(source{}); got != want[i] {
			t.Errorf("line %d = %q; want %q", i, got, want[i])
		}
	}
	if report.State() != ntfy.StateFailed {
		t.Fatalf("the report should be failed for the footer test")
	}
	if footer := ReportFooter(report, source{}); footer != "" {
		t.Errorf("ReportFooter() = %q; a failed report has nothing to excuse", footer)
	}
	if footer := ReportFooter(ntfy.Report{Health: report.Health, Account: report.Account}, source{}); footer == "" {
		t.Error("ReportFooter() = \"\"; want the line about an unverified check")
	}
}

// TestCatalogCoversThePrompt keeps the new strings in the catalogues: a msgid
// missing from locales/*.po reaches the person in English.
func TestCatalogCoversThePrompt(t *testing.T) {
	cases := []struct {
		locale string
		msgid  string
		want   string
	}{
		{locale: "en", msgid: "Continue", want: "Continue"},
		{locale: "es", msgid: "Continue", want: "Continuar"},
		{locale: "es", msgid: "Cancel", want: "Cancelar"},
		{locale: "es", msgid: "Write", want: "Escribir"},
		{locale: "es", msgid: "Configure ntfy-desktop", want: "Configurar ntfy-desktop"},
		{locale: "es", msgid: "topics to subscribe to", want: "topics a los que suscribirse"},
		{locale: "es", msgid: "add a topic (leave it empty to finish)", want: "agregá un topic (dejalo vacío para terminar)"},
		{locale: "es", msgid: "topic %s", want: "topic %s"},
		{locale: "es", msgid: "the list needs at least one topic", want: "la lista necesita al menos un topic"},
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
