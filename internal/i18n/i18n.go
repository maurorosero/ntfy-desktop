// SPDX-License-Identifier: GPL-3.0-or-later

// Package i18n resuelve las traducciones del producto a partir de los catálogos
// gettext embebidos. El inglés es el idioma fuente: si no hay catálogo para el
// idioma pedido, se devuelve el texto original.
package i18n

import (
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/maurorosero/ntfy-desktop/locales"
)

const dominio = "ntfy-desktop"

// Translator traduce cadenas para un idioma ya resuelto.
type Translator struct {
	locale *gotext.Locale
	lang   string
}

// New construye un traductor para el locale pedido, con respaldo
// <idioma>_<pais> -> <idioma> -> inglés.
func New(locale string) *Translator {
	for _, candidato := range candidates(locale) {
		l := gotext.NewLocaleFS(candidato, locales.FS)
		if l == nil {
			continue
		}
		l.AddDomain(dominio)
		if _, ok := l.Domains[dominio]; ok {
			base := strings.SplitN(gotext.SimplifiedLocale(candidato), "_", 2)[0]
			return &Translator{locale: l, lang: base}
		}
	}
	return &Translator{lang: "en"}
}

// Language devuelve el idioma efectivamente usado.
func (t *Translator) Language() string {
	return t.lang
}

// T traduce una cadena. Sin catálogo, devuelve la cadena original.
func (t *Translator) T(msgid string, vars ...any) string {
	if t.locale == nil {
		if len(vars) == 0 {
			return msgid
		}
		return gotext.FormatString(msgid, vars...)
	}
	return t.locale.GetD(dominio, msgid, vars...)
}

// candidates arma la cadena de respaldo, sin repetidos:
// es_PA.UTF-8 -> [es_PA, es, en]
func candidates(locale string) []string {
	raw := strings.TrimSpace(locale)
	if raw == "" || raw == "C" || raw == "POSIX" {
		return []string{"en"}
	}

	// Fuera codificación y modificador: es_PA.UTF-8 -> es_PA, es@euro -> es
	norm := raw
	if i := strings.IndexByte(norm, '.'); i > 0 {
		norm = norm[:i]
	}
	if i := strings.IndexByte(norm, '@'); i > 0 {
		norm = norm[:i]
	}

	out := make([]string, 0, 3)
	agregar := func(v string) {
		if v == "" {
			return
		}
		for _, x := range out {
			if x == v {
				return
			}
		}
		out = append(out, v)
	}

	agregar(norm)
	if i := strings.IndexByte(norm, '_'); i > 0 {
		agregar(norm[:i])
	}
	agregar("en")
	return out
}
