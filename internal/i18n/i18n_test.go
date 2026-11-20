// SPDX-License-Identifier: GPL-3.0-or-later

package i18n

import "testing"

func TestTraduceEspanol(t *testing.T) {
	tr := New("es_PA")
	if got := tr.Language(); got != "es" {
		t.Fatalf("Language() = %q, quiero es", got)
	}
	if got := tr.T("user"); got != "usuario" {
		t.Errorf("T(user) = %q, quiero usuario", got)
	}
	if got := tr.T("dialogs"); got != "diálogos" {
		t.Errorf("T(dialogs) = %q, quiero diálogos", got)
	}
}

func TestEspanolExacto(t *testing.T) {
	tr := New("es")
	if got := tr.T("desktop"); got != "escritorio" {
		t.Errorf("T(desktop) = %q, quiero escritorio", got)
	}
}

func TestInglesDevuelveLaFuente(t *testing.T) {
	tr := New("en")
	if got := tr.Language(); got != "en" {
		t.Fatalf("Language() = %q, quiero en", got)
	}
	if got := tr.T("user"); got != "user" {
		t.Errorf("T(user) = %q, quiero user", got)
	}
}

func TestIdiomaDesconocidoCaeAIngles(t *testing.T) {
	tr := New("fr_FR")
	if got := tr.Language(); got != "en" {
		t.Errorf("Language() = %q, quiero en", got)
	}
	if got := tr.T("locale"); got != "locale" {
		t.Errorf("T(locale) = %q, quiero locale", got)
	}
}

func TestSinCatalogoDevuelveLaCadena(t *testing.T) {
	tr := New("")
	if got := tr.T("cadena que no existe"); got != "cadena que no existe" {
		t.Errorf("se esperaba la cadena original, se obtuvo %q", got)
	}
}

func TestCandidates(t *testing.T) {
	casos := []struct {
		entrada string
		quiero  []string
	}{
		{"es_PA", []string{"es_PA", "es", "en"}},
		{"es", []string{"es", "en"}},
		{"en", []string{"en"}},
		{"C", []string{"en"}},
		{"", []string{"en"}},
		{"fr_FR.UTF-8", []string{"fr_FR", "fr", "en"}},
	}
	for _, c := range casos {
		got := candidates(c.entrada)
		if len(got) != len(c.quiero) {
			t.Errorf("candidates(%q) = %v, quiero %v", c.entrada, got, c.quiero)
			continue
		}
		for i := range got {
			if got[i] != c.quiero[i] {
				t.Errorf("candidates(%q) = %v, quiero %v", c.entrada, got, c.quiero)
				break
			}
		}
	}
}
