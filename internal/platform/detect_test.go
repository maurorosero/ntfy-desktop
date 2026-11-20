// SPDX-License-Identifier: GPL-3.0-or-later

package platform

import "testing"

func TestClassify(t *testing.T) {
	casos := []struct {
		entrada string
		quiero  Desktop
	}{
		{"KDE", KDE},
		{"kde", KDE},
		{"ubuntu:GNOME", GNOME},
		{"GNOME", GNOME},
		{"XFCE", Other},
		{"", Other},
		{"sway", Other},
	}
	for _, c := range casos {
		if got := Classify(c.entrada); got != c.quiero {
			t.Errorf("Classify(%q) = %q, quiero %q", c.entrada, got, c.quiero)
		}
	}
}

func TestNormalizeLocale(t *testing.T) {
	casos := map[string]string{
		"es_PA.UTF-8": "es_PA",
		"es_PA":       "es_PA",
		"en_US.utf8":  "en_US",
		"es@euro":     "es",
		"":            "",
		"  en_GB  ":   "en_GB",
	}
	for entrada, quiero := range casos {
		if got := NormalizeLocale(entrada); got != quiero {
			t.Errorf("NormalizeLocale(%q) = %q, quiero %q", entrada, got, quiero)
		}
	}
}

func TestLocaleNeutroCaeAIngles(t *testing.T) {
	for _, neutro := range []string{"C", "POSIX"} {
		t.Setenv("LC_ALL", neutro)
		t.Setenv("LC_MESSAGES", neutro)
		t.Setenv("LANG", neutro)
		if got := detectLocale(); got != "en" {
			t.Errorf("con %q se esperaba en, se obtuvo %q", neutro, got)
		}
	}
}

func TestLocaleConValor(t *testing.T) {
	t.Setenv("LC_ALL", "es_PA.UTF-8")
	if got := detectLocale(); got != "es_PA" {
		t.Errorf("se esperaba es_PA, se obtuvo %q", got)
	}
}
