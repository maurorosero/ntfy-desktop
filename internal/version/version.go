// SPDX-License-Identifier: GPL-3.0-or-later

// Package version expone la versión del producto, inyectada en el build.
package version

// Version se sobrescribe en tiempo de enlace:
//
//	go build -ldflags "-X github.com/maurorosero/ntfy-desktop/internal/version.Version=1.2.3"
var Version = "dev"
