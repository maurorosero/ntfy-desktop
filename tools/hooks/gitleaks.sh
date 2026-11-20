#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Corre gitleaks sobre lo preparado. Si no está instalado, avisa y no bloquea:
# en integración continua siempre corre.
set -euo pipefail
if ! command -v gitleaks >/dev/null 2>&1; then
	echo "gitleaks no instalado en local, se omite (corre en CI)"
	exit 0
fi
exec gitleaks protect --staged --redact
