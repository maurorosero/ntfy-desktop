// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
)

// fakeToolDir puts a shell script named tool in a directory of its own and
// leaves it as the only PATH entry, so the program finds that script and can
// never reach a dialog of the machine. The script uses shell builtins only:
// with a PATH of one directory there is no cat or grep to call.
func fakeToolDir(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing the fake dialog: %v", err)
	}
	t.Setenv("PATH", dir)
	return dir
}

// TestConfigGUIFallsBackToText is the decision of the brief: without kdialog
// and without zenity the graphical wizard does not fail, it asks in the
// terminal and says so in one line.
func TestConfigGUIFallsBackToText(t *testing.T) {
	dir := configDir(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	srv := deadServer(t)
	withStdin(t, srv+"\nn\ncasa\ny\n")

	code, out, errOut := runConfigCommand(t, "config", "--gui")
	if code != exitOK {
		t.Fatalf("config --gui = %d; want 0. stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "text mode") {
		t.Errorf("stderr = %q; want the line that says it fell back to text", errOut)
	}
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	if file := readFile(t, path); !strings.Contains(file, `topic = "casa"`) {
		t.Errorf("file = %q; want the topic the interview asked for. stdout: %s", file, out)
	}
}

// TestConfigGUIWithoutADesktopSession covers the other reason to fall back: the
// tool is there but there is no screen to draw it on.
func TestConfigGUIWithoutADesktopSession(t *testing.T) {
	dir := configDir(t)
	fakeToolDir(t, "kdialog", "exit 0")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	srv := deadServer(t)
	withStdin(t, srv+"\nn\ncasa\ny\n")

	code, _, errOut := runConfigCommand(t, "config", "--gui")
	if code != exitOK {
		t.Fatalf("config --gui = %d; want 0. stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "no desktop session") {
		t.Errorf("stderr = %q; want the line about the missing session", errOut)
	}
	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	if file := readFile(t, path); !strings.Contains(file, `topic = "casa"`) {
		t.Errorf("file = %q; want the topic the interview asked for", file)
	}
}

// TestConfigGUIWithKDialogDrivesTheWholeInterview runs --gui against a fake
// kdialog that answers every step: the flow has to reach the write, the file
// has to keep the secret, and the argument log has to never see it.
func TestConfigGUIWithKDialogDrivesTheWholeInterview(t *testing.T) {
	dir := configDir(t)
	log := filepath.Join(t.TempDir(), "argv")
	count := filepath.Join(t.TempDir(), "count")
	t.Setenv("NTFY_DIALOG_LOG", log)
	t.Setenv("NTFY_DIALOG_COUNT", count)
	t.Setenv("NTFY_ANSWER_PASSWORD", "malpass-9z")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")

	srv := newNtfyServer(t, serverCodes{body: `{"username":"andrea","role":"user"}`})
	t.Setenv("NTFY_ANSWER_SERVER", srv)

	fakeToolDir(t, "kdialog", `log="$NTFY_DIALOG_LOG"
count="$NTFY_DIALOG_COUNT"
printf '%s\n' "$*" >> "$log"
n=0
if [ -f "$count" ]; then read -r n < "$count"; fi
case " $* " in
*" --inputbox "*)
	n=$((n+1)); printf '%s\n' "$n" > "$count"
	case "$n" in
	1) printf '%s\n' "$NTFY_ANSWER_SERVER";;
	2) printf '%s\n' "andrea";;
	3) printf '%s\n' "casa";;
	*) printf '\n';;
	esac
	exit 0;;
*" --password "*)
	printf '%s\n' "$NTFY_ANSWER_PASSWORD"
	exit 0;;
*" --textbox "*)
	prev=""
	for a in "$@"; do
		if [ "$prev" = "--textbox" ]; then
			while IFS= read -r line; do printf '%s\n' "$line" >> "$log"; done < "$a"
		fi
		prev="$a"
	done
	exit 0;;
esac
exit 0`)

	code, out, errOut := runConfigCommand(t, "config", "--gui")
	if code != exitOK {
		t.Fatalf("config --gui = %d; want 0. stdout: %s stderr: %s", code, out, errOut)
	}

	path := filepath.Join(dir, "ntfy-desktop", "config.toml")
	file := readFile(t, path)
	for _, want := range []string{
		`server = "` + srv + `"`,
		`topic = "casa"`,
		`user = "andrea"`,
		`password = "malpass-9z"`,
	} {
		if !strings.Contains(file, want) {
			t.Errorf("file = %q; want %q in it", file, want)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("reading the permissions of %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode of the file = %o; want 600", mode)
	}

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading the dialog log: %v", err)
	}
	argvLog := string(data)
	if strings.Contains(argvLog, "malpass-9z") {
		t.Errorf("the dialog log exposes the password:\n%s", argvLog)
	}
	if !strings.Contains(argvLog, maskedPassword) {
		t.Errorf("the dialog log has no masked password; want %q in the summary", maskedPassword)
	}
	if !strings.Contains(argvLog, "--password") {
		t.Errorf("the dialog log has no --password; the secret was asked some other way")
	}
	if !strings.Contains(argvLog, "5 of 5") {
		t.Errorf("the dialog log has no title of the last step:\n%s", argvLog)
	}
}

// TestConfigGUICancelLeavesTheFileAlone is the acceptance criterion of the
// issue: a cancel in the first dialog —code 2 of kdialog— writes nothing.
func TestConfigGUICancelLeavesTheFileAlone(t *testing.T) {
	dir := configDir(t)
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")
	fakeToolDir(t, "kdialog", "exit 2")

	path := writeConfig(t, dir, "# la de casa\nserver = \"https://ntfy.invalid\"\ntopic = \"casa\"\n")
	before := digest(t, path)

	code, _, _ := runConfigCommand(t, "config", "--gui")
	if code != exitNothing {
		t.Fatalf("config --gui cancelled = %d; want %d", code, exitNothing)
	}
	if after := digest(t, path); after != before {
		t.Errorf("the file changed: %s -> %s", before, after)
	}
}

// TestCatalogCoversTheGUICommand keeps the strings the command adds in the
// catalogues: the fallback line is the one that tells a person why there is no
// window.
func TestCatalogCoversTheGUICommand(t *testing.T) {
	cases := []struct {
		locale string
		msgid  string
		want   string
	}{
		{
			locale: "es",
			msgid:  "no kdialog or zenity in the PATH: the interview continues in text mode",
			want:   "no hay kdialog ni zenity en el PATH: la entrevista sigue en modo texto",
		},
		{
			locale: "es",
			msgid:  "there is no desktop session: the interview continues in text mode",
			want:   "no hay sesión de escritorio: la entrevista sigue en modo texto",
		},
		{
			locale: "es",
			msgid:  "config takes one of --gui, --show, --check or --set",
			want:   "config acepta una de --gui, --show, --check o --set",
		},
		{locale: "es", msgid: "Server", want: "Servidor"},
		{locale: "es", msgid: "Summary", want: "Resumen y escribir"},
		{
			locale: "es",
			msgid:  "it is written with 0600 permissions; the previous version, if any, is kept as a .bak next to it",
			want:   "se escribe con permisos 0600; la versión anterior, si existe, queda como .bak al lado",
		},
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
