// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

// source is the English catalog as the adapters receive it: the message ids are
// the text the person reads, which keeps the expected command lines readable.
type source struct{}

func (source) T(msgid string, vars ...any) string {
	if len(vars) == 0 {
		return msgid
	}
	return fmt.Sprintf(msgid, vars...)
}

func view(label, url string) ntfy.Action {
	return ntfy.Action{Action: "view", Label: label, URL: url}
}

// exitCode builds the *exec.ExitError a dialog leaves behind when it exits with
// that code, so the test reads the same error the adapter reads.
func exitCode(code int) error {
	p, err := os.StartProcess("/bin/sh", []string{"sh", "-c", fmt.Sprintf("exit %d", code)}, &os.ProcAttr{})
	if err != nil {
		panic(err)
	}
	state, err := p.Wait()
	if err != nil {
		panic(err)
	}
	return &exec.ExitError{ProcessState: state}
}

func TestArgs(t *testing.T) {
	cases := []struct {
		name    string
		adapter string // "kdialog" or "zenity"
		pos     int
		total   int
		msg     ntfy.Message
		actions []ntfy.Action
		want    []string
	}{
		{
			// Two actions are the common case, and both desktops can draw them as
			// real buttons: the labels are ours and the answer is one click.
			name:    "kdialog with two actions uses three buttons",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "Unauthorized access", Message: "Movement in the yard", Priority: 3},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
			},
			want: []string{
				"--title", "Decision",
				"--icon", "dialog-question",
				"--yesnocancel", "<b>Unauthorized access</b><br/>Movement in the yard",
				"--yes-label", "Open portal",
				"--no-label", "Turn down",
				"--cancel-label", "Dismiss",
			},
		},
		{
			name:    "kdialog with one action puts it on the only button",
			adapter: "kdialog",
			pos:     1,
			total:   2,
			msg:     ntfy.Message{Title: "Unauthorized access"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision (1 of 2)",
				"--icon", "dialog-question",
				"--msgbox", "<b>Unauthorized access</b>",
				"--ok-label", "Open portal",
			},
		},
		{
			name:    "kdialog escapes the sender's markup",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "<i>bold</i>", Message: "a & b\nsecond line"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--icon", "dialog-question",
				"--msgbox", "<b>&lt;i&gt;bold&lt;/i&gt;</b><br/>a &amp; b<br/>second line",
				"--ok-label", "Open portal",
			},
		},
		{
			name:    "kdialog without text asks for an action",
			adapter: "kdialog",
			actions: []ntfy.Action{view("", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--icon", "dialog-question",
				"--msgbox", "Choose an action",
				"--ok-label", "view", // no label: the kind of action is all there is
			},
		},
		{
			// Three actions do not fit on two buttons, so the menu comes back and
			// carries the way out as a row.
			name:    "kdialog with three actions keeps the menu",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "Intruder", Priority: 5},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
				view("Call me", "https://call.example/"),
			},
			want: []string{
				"--title", "Decision",
				"--icon", "dialog-warning",
				"--menu", "<b>Intruder</b>",
				"--default", "1",
				"1", "Open portal",
				"2", "Turn down",
				"3", "Call me",
				"0", "Dismiss",
			},
		},
		{
			name:    "kdialog says how loud a quiet message is",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "Backup done", Priority: 1},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--icon", "dialog-information",
				"--msgbox", "<b>Backup done</b>",
				"--ok-label", "Open portal",
			},
		},
		{
			name:    "zenity with two actions uses three buttons",
			adapter: "zenity",
			msg:     ntfy.Message{Title: "Unauthorized access", Message: "Movement in the yard", Priority: 4},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
			},
			want: []string{
				"--title", "Decision",
				"--question", "--text", "<b>Unauthorized access</b>\nMovement in the yard",
				"--ok-label", "Open portal",
				"--extra-button", "Turn down",
				"--cancel-label", "Dismiss",
			},
		},
		{
			name:    "zenity with one action puts it on the only button",
			adapter: "zenity",
			pos:     2,
			total:   2,
			msg:     ntfy.Message{Title: "Denied request"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision (2 of 2)",
				"--info", "--text", "<b>Denied request</b>",
				"--ok-label", "Open portal",
			},
		},
		{
			name:    "zenity escapes the sender's markup",
			adapter: "zenity",
			msg:     ntfy.Message{Title: "<b>x</b>", Message: "a & b"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--info", "--text", "<b>&lt;b&gt;x&lt;/b&gt;</b>\na &amp; b",
				"--ok-label", "Open portal",
			},
		},
		{
			// The height grows with the rows: a fixed one clipped the last row
			// behind the buttons.
			name:    "zenity with three actions keeps the list and sizes it",
			adapter: "zenity",
			msg:     ntfy.Message{Title: "Intruder"},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
				view("Call me", "https://call.example/"),
			},
			want: []string{
				"--title", "Decision",
				"--list", "--radiolist",
				"--text", "<b>Intruder</b>",
				"--column", "", "--column", "", "--column", "Action",
				"--hide-header", "--hide-column=2", "--print-column=2",
				"--width", "520", "--height", "310",
				"--ok-label", "Select",
				"--cancel-label", "Dismiss",
				"TRUE", "1", "Open portal",
				"FALSE", "2", "Turn down",
				"FALSE", "3", "Call me",
				"FALSE", "0", "Dismiss",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if c.pos > 0 {
				ctx = WithPending(ctx, c.pos, c.total)
			}

			var got []string
			switch c.adapter {
			case "kdialog":
				got = NewKDialog("/usr/bin/kdialog", source{}).args(ctx, c.msg, c.actions)
			case "zenity":
				got = NewZenity("/usr/bin/zenity", source{}).args(ctx, c.msg, c.actions)
			default:
				t.Fatalf("adaptador desconocido: %q", c.adapter)
			}

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("args =\n  %q\nquiero\n  %q", got, c.want)
			}
		})
	}
}

func TestNewPicksTheAdapter(t *testing.T) {
	cases := []struct {
		tool    string
		want    any
		wantErr bool
	}{
		{tool: "/usr/bin/kdialog", want: &KDialog{}},
		{tool: "zenity", want: &Zenity{}},
		{tool: "/usr/local/bin/zenity", want: &Zenity{}},
		{tool: "", wantErr: true},
		{tool: "/usr/bin/notify-send", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			got, err := New(c.tool, source{})
			if c.wantErr {
				if err == nil {
					t.Fatalf("New(%q) = %T, quiero error", c.tool, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q) = %v, quiero nil", c.tool, err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(c.want) {
				t.Errorf("New(%q) = %T, quiero %T", c.tool, got, c.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	actions := []ntfy.Action{view("Open portal", "https://home.example/"), view("Turn down", "https://api.example/")}

	cases := []struct {
		name      string
		tag       string
		want      string
		wantErr   bool
		cancelled bool
	}{
		{name: "first action", tag: "1", want: "Open portal"},
		{name: "second action", tag: "2", want: "Turn down"},
		{name: "the way out", tag: "0", wantErr: true, cancelled: true},
		{name: "out of range", tag: "3", wantErr: true},
		{name: "not a number", tag: "open", wantErr: true},
		{name: "empty", tag: "", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolve(c.tag, actions)
			if c.wantErr {
				if err == nil {
					t.Fatalf("resolve(%q) = %q, quiero error", c.tag, got)
				}
				if cancelled := errors.Is(err, ErrCancelled); cancelled != c.cancelled {
					t.Errorf("resolve(%q) = %v, ¿ErrCancelled? %v", c.tag, err, cancelled)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve(%q) = %v, quiero nil", c.tag, err)
			}
			if got != c.want {
				t.Errorf("resolve(%q) = %q, quiero %q", c.tag, got, c.want)
			}
		})
	}
}

// TestAnswerOfButtons covers the exit codes of --yesnocancel: 0 is the first
// action, 1 the second and 2 the way out. Reading 1 as a cancel, which is what
// the single-button dialogs do, would answer "turn down" with "no answer".
func TestAnswerOfButtons(t *testing.T) {
	actions := []ntfy.Action{view("Aprobar", "https://home.example/"), view("Rechazar", "https://api.example/")}

	cases := []struct {
		name      string
		err       error
		want      string
		cancelled bool
	}{
		{name: "yes", err: nil, want: "Aprobar"},
		{name: "no", err: exitCode(1), want: "Rechazar"},
		{name: "cancel", err: exitCode(2), cancelled: true},
		{name: "something else", err: exitCode(7), want: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := answerOfButtons(context.Background(), c.err, actions)
			if c.cancelled {
				if !errors.Is(err, ErrCancelled) {
					t.Fatalf("answerOfButtons = %v, quiero ErrCancelled", err)
				}
				return
			}
			if c.want == "" {
				if err == nil {
					t.Fatalf("answerOfButtons = %q, quiero error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("answerOfButtons = %v, quiero nil", err)
			}
			if got != c.want {
				t.Errorf("answerOfButtons = %q, quiero %q", got, c.want)
			}
		})
	}
}

func TestPendingRoundTrip(t *testing.T) {
	ctx := WithPending(context.Background(), 2, 3)
	if pos, total := Pending(ctx); pos != 2 || total != 3 {
		t.Errorf("Pending() = %d, %d, quiero 2, 3", pos, total)
	}
	if pos, total := Pending(context.Background()); pos != 0 || total != 0 {
		t.Errorf("Pending() sin marca = %d, %d, quiero 0, 0", pos, total)
	}
}

// TestCatalogCoversTheDialog keeps the new visible strings in the catalogs: a
// msgid missing from locales/*.po reaches the user in English.
func TestCatalogCoversTheDialog(t *testing.T) {
	cases := []struct {
		locale string
		want   []string
	}{
		{locale: "en", want: []string{"Decision", "Decision (1 of 2)", "Dismiss", "Select", "Choose an action", "Action"}},
		{locale: "es", want: []string{"Decisión", "Decisión (1 de 2)", "Descartar", "Elegir", "Elegí una acción", "Acción"}},
	}

	for _, c := range cases {
		t.Run(c.locale, func(t *testing.T) {
			tr := i18n.New(c.locale)
			got := []string{
				tr.T("Decision"),
				tr.T("Decision (%d of %d)", 1, 2),
				tr.T("Dismiss"),
				tr.T("Select"),
				tr.T("Choose an action"),
				tr.T("Action"),
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("catálogo %s = %q, quiero %q", c.locale, got, c.want)
			}
		})
	}
}
