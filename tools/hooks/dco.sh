#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Verifica el Developer Certificate of Origin: cada commit debe llevar
# Signed-off-by con el correo del autor.
#
#   tools/hooks/dco.sh origin/main

set -euo pipefail

base="${1:-origin/main}"
if ! git rev-parse --verify --quiet "${base}" >/dev/null; then
	echo "no existe la referencia ${base}" >&2
	exit 2
fi

fallo=0
revisados=0
omitidos=0

while read -r sha; do
	[ -n "${sha}" ] || continue
	revisados=$((revisados + 1))
	autor="$(git log -1 --format='%ae' "${sha}")"
	nombre="$(git log -1 --format='%an' "${sha}")"

	# Los bots no firman: Dependabot, Renovate y las acciones de GitHub no pueden
	# agregar Signed-off-by. Se omiten, se informan, y el resto del PR sigue sujeto a la regla.
	if [[ "${nombre}" =~ \[bot\]$ ]]; then
		omitidos=$((omitidos + 1))
		echo "omitido (bot): $(git log -1 --format='%h %s' "${sha}")  [${nombre}]"
		continue
	fi

	if ! git log -1 --format='%B' "${sha}" | grep -qiE "^Signed-off-by: .+<${autor}>[[:space:]]*$"; then
		echo "sin firma DCO: $(git log -1 --format='%h %s' "${sha}")  (autor ${autor})"
		fallo=1
	fi
done < <(git rev-list "${base}..HEAD")

if [ "${fallo}" -ne 0 ]; then
	echo ""
	echo "Firmá los commits con:  git commit -s   (o  git rebase --signoff)"
	exit 1
fi

echo "DCO: ${revisados} commits revisados, ${omitidos} omitidos por ser de bots"
