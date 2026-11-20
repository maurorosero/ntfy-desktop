// SPDX-License-Identifier: GPL-3.0-or-later

// ntfy-desktop es un cliente de ntfy para escritorio Linux.
// Esta es la etapa de esqueleto: detecta el entorno y no habla con el servidor todavía.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/maurorosero/ntfy-desktop/internal/i18n"
	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/version"
)

func main() {
	entorno := platform.Detect()
	tr := i18n.New(entorno.Locale)

	mostrarVersion := flag.Bool("version", false, tr.T("prints the version and exits"))
	flag.Parse()

	if *mostrarVersion {
		fmt.Printf("ntfy-desktop %s\n", version.Version)
		return
	}

	herramienta := entorno.DialogTool
	if herramienta == "" {
		herramienta = tr.T("no dialog tool found")
	}

	fmt.Printf("ntfy-desktop %s\n", version.Version)
	fmt.Printf("  %-10s %s\n", tr.T("user"), entorno.User)
	fmt.Printf("  %-10s %s\n", tr.T("locale"), entorno.Locale)
	fmt.Printf("  %-10s %s\n", tr.T("desktop"), entorno.Desktop)
	fmt.Printf("  %-10s %s\n", tr.T("dialogs"), herramienta)

	os.Exit(0)
}
