#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Verifica que todo archivo .go empiece con la cabecera SPDX del proyecto.
set -euo pipefail
raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fallo=0
while IFS= read -r -d '' archivo; do
	if ! head -1 "${archivo}" | grep -q "SPDX-License-Identifier: GPL-3.0-or-later"; then
		echo "falta la cabecera SPDX: ${archivo#${raiz}/}"
		fallo=1
	fi
done < <(find "${raiz}" -name '*.go' -not -path '*/.toolchain/*' -print0)
exit "${fallo}"
