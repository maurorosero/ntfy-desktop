// SPDX-License-Identifier: GPL-3.0-or-later

// Package platform detecta el entorno del escritorio: usuario de sesión, idioma y
// qué herramienta de diálogo está disponible. Es el andamio de integración: el
// núcleo no conoce ningún escritorio, sólo consume el resultado de esta detección.
package platform

import (
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// Desktop identifica la familia de escritorio.
type Desktop string

const (
	KDE   Desktop = "kde"
	GNOME Desktop = "gnome"
	Other Desktop = "other"
)

// Env es el entorno detectado al arrancar.
type Env struct {
	User       string
	Locale     string
	Desktop    Desktop
	DialogTool string // ruta de kdialog o zenity; vacío si no hay ninguna
}

// Classify traduce el valor de XDG_CURRENT_DESKTOP a una familia de escritorio.
// Acepta valores compuestos como "ubuntu:GNOME" o "KDE".
func Classify(desktop string) Desktop {
	d := strings.ToUpper(desktop)
	switch {
	case strings.Contains(d, "KDE"):
		return KDE
	case strings.Contains(d, "GNOME"):
		return GNOME
	default:
		return Other
	}
}

// NormalizeLocale reduce un locale POSIX a su idioma, sin codificación:
// "es_PA.UTF-8" -> "es_PA", "es_PA" -> "es_PA".
func NormalizeLocale(raw string) string {
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '@'); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

// Detect lee el entorno de sesión. Nunca falla: si algo no está, deja el campo vacío.
func Detect() Env {
	e := Env{
		User:    detectUser(),
		Locale:  detectLocale(),
		Desktop: Classify(os.Getenv("XDG_CURRENT_DESKTOP")),
	}
	e.DialogTool = detectDialogTool(e.Desktop)
	return e
}

func detectUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("LOGNAME")
}

func detectLocale() string {
	for _, clave := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := NormalizeLocale(os.Getenv(clave)); v != "" && !esLocaleNeutro(v) {
			return v
		}
	}
	return "en"
}

// esLocaleNeutro reconoce los valores que POSIX usa cuando no hay idioma elegido.
func esLocaleNeutro(v string) bool {
	return v == "C" || v == "POSIX"
}

// detectDialogTool elige la herramienta de diálogo del escritorio y sólo cae al
// respaldo cuando la propia del entorno no existe.
func detectDialogTool(d Desktop) string {
	preferidos := map[Desktop][]string{
		KDE:   {"kdialog", "zenity"},
		GNOME: {"zenity", "kdialog"},
		Other: {"zenity", "kdialog"},
	}
	for _, nombre := range preferidos[d] {
		if ruta, err := exec.LookPath(nombre); err == nil {
			return ruta
		}
	}
	return ""
}
