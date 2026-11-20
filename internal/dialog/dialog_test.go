// SPDX-License-Identifier: GPL-3.0-or-later

package dialog

import (
	"context"
	"errors"
	"fmt"
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
			name:    "kdialog with two actions",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "Unauthorized access", Message: "Movement in the yard"},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
			},
			want: []string{
				"--title", "Decision",
				"--menu", "<b>Unauthorized access</b><br/>Movement in the yard",
				"--default", "1",
				"1", "Open portal",
				"2", "Turn down",
				"0", "Dismiss",
			},
		},
		{
			name:    "kdialog says how many are waiting",
			adapter: "kdialog",
			pos:     1,
			total:   2,
			msg:     ntfy.Message{Title: "Unauthorized access"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision (1 of 2)",
				"--menu", "<b>Unauthorized access</b>",
				"--default", "1",
				"1", "Open portal",
				"0", "Dismiss",
			},
		},
		{
			name:    "kdialog escapes the sender's markup",
			adapter: "kdialog",
			msg:     ntfy.Message{Title: "<i>bold</i>", Message: "a & b\nsecond line"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--menu", "<b>&lt;i&gt;bold&lt;/i&gt;</b><br/>a &amp; b<br/>second line",
				"--default", "1",
				"1", "Open portal",
				"0", "Dismiss",
			},
		},
		{
			name:    "kdialog without text asks for an action",
			adapter: "kdialog",
			actions: []ntfy.Action{view("", "https://home.example/")},
			want: []string{
				"--title", "Decision",
				"--menu", "Choose an action",
				"--default", "1",
				"1", "view", // no label: the kind of action is all there is
				"0", "Dismiss",
			},
		},
		{
			name:    "zenity with two actions",
			adapter: "zenity",
			msg:     ntfy.Message{Title: "Unauthorized access", Message: "Movement in the yard"},
			actions: []ntfy.Action{
				view("Open portal", "https://home.example/"),
				view("Turn down", "https://api.example/"),
			},
			want: []string{
				"--list", "--radiolist",
				"--title", "Decision",
				"--text", "<b>Unauthorized access</b>\nMovement in the yard",
				"--column", "", "--column", "", "--column", "Action",
				"--hide-header", "--hide-column=2", "--print-column=2",
				"--ok-label", "Select", "--cancel-label", "Dismiss",
				"TRUE", "1", "Open portal",
				"FALSE", "2", "Turn down",
				"FALSE", "0", "Dismiss",
			},
		},
		{
			name:    "zenity says how many are waiting",
			adapter: "zenity",
			pos:     2,
			total:   2,
			msg:     ntfy.Message{Title: "Denied request"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--list", "--radiolist",
				"--title", "Decision (2 of 2)",
				"--text", "<b>Denied request</b>",
				"--column", "", "--column", "", "--column", "Action",
				"--hide-header", "--hide-column=2", "--print-column=2",
				"--ok-label", "Select", "--cancel-label", "Dismiss",
				"TRUE", "1", "Open portal",
				"FALSE", "0", "Dismiss",
			},
		},
		{
			name:    "zenity escapes the sender's markup",
			adapter: "zenity",
			msg:     ntfy.Message{Title: "<b>x</b>", Message: "a & b"},
			actions: []ntfy.Action{view("Open portal", "https://home.example/")},
			want: []string{
				"--list", "--radiolist",
				"--title", "Decision",
				"--text", "<b>&lt;b&gt;x&lt;/b&gt;</b>\na &amp; b",
				"--column", "", "--column", "", "--column", "Action",
				"--hide-header", "--hide-column=2", "--print-column=2",
				"--ok-label", "Select", "--cancel-label", "Dismiss",
				"TRUE", "1", "Open portal",
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
