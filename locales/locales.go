// SPDX-License-Identifier: GPL-3.0-or-later

// Package locales expone los catálogos de traducción embebidos en el binario.
// Estructura gettext estándar: <idioma>/LC_MESSAGES/ntfy-desktop.po
package locales

import "embed"

// FS contiene los catálogos. Se embeben para que el binario sea un solo archivo
// y no dependa de archivos en disco.
//
//go:embed en es
var FS embed.FS
