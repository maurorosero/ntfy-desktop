#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Falla si algún .go no está formateado.
set -euo pipefail
raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fuera="$("${raiz}/.toolchain/go/bin/gofmt" -l "${raiz}")"
if [ -n "${fuera}" ]; then
	echo "archivos sin formatear:"
	echo "${fuera}"
	exit 1
fi
