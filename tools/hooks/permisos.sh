#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Ningún archivo del repositorio puede ser escribible por grupo o por terceros.
# Sólo el dueño escribe. Falla con la lista de los que violan la regla.

set -euo pipefail

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${raiz}"

violan="$(find . -path ./.toolchain -prune -o -path ./.git -prune -o -perm /022 -print 2>/dev/null || true)"

if [ -n "${violan}" ]; then
	echo "escribibles por grupo o por terceros:"
	echo "${violan}"
	echo ""
	echo "corregí con:  chmod -R go-w ."
	exit 1
fi

echo "permisos: sólo el dueño escribe"
