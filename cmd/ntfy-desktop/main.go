// SPDX-License-Identifier: GPL-3.0-or-later

// ntfy-desktop es un cliente de ntfy para escritorio Linux.
// Esta es la etapa de esqueleto: detecta el entorno y no habla con el servidor todavía.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/maurorosero/ntfy-desktop/internal/platform"
	"github.com/maurorosero/ntfy-desktop/internal/version"
)

func main() {
	mostrarVersion := flag.Bool("version", false, "muestra la versión y termina")
	flag.Parse()

	if *mostrarVersion {
		fmt.Printf("ntfy-desktop %s\n", version.Version)
		return
	}

	entorno := platform.Detect()

	herramienta := entorno.DialogTool
	if herramienta == "" {
		herramienta = "(ninguna: sin diálogos)"
	}

	fmt.Printf("ntfy-desktop %s\n", version.Version)
	fmt.Printf("  usuario   %s\n", entorno.User)
	fmt.Printf("  idioma    %s\n", entorno.Locale)
	fmt.Printf("  escritorio %s\n", entorno.Desktop)
	fmt.Printf("  diálogos  %s\n", herramienta)

	os.Exit(0)
}
