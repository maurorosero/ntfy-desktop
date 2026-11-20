#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Arma el expediente de un release para que un agente escriba el changelog y
# actualice la documentación. No escribe nada en el repo: deja el expediente en
# .changes/release-<version>.md (ignorado por git) y lo imprime por pantalla.
#
#   tools/release-prep.sh v0.2.0
#
# Lo que recolecta: fragmentos pendientes, commits desde el último tag, archivos
# tocados y los documentos que suelen necesitar actualización.

set -euo pipefail

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${RAIZ}"

version="${1:-}"
[ -n "${version}" ] || {
	echo "falta la versión, por ejemplo: tools/release-prep.sh v0.2.0" >&2
	exit 1
}
version="${version#v}"
salida="${RAIZ}/.changes/release-${version}.md"

ultimo_tag="$(git describe --tags --abbrev=0 2>/dev/null || echo "")"
if [ -n "${ultimo_tag}" ]; then
	rango="${ultimo_tag}..HEAD"
	base="${ultimo_tag}"
else
	rango="HEAD"
	base="(sin tags)"
fi

{
	echo "# Expediente de release ${version}"
	echo ""
	echo "Generado: $(date -Iseconds)"
	echo "Base: ${base}"
	echo "Commits: $(git rev-list --count ${rango})"
	echo ""
	echo "## 1. Fragmentos pendientes"
	echo ""
	hay=0
	for tipo in added changed deprecated removed fixed security; do
		archivos="$(find .changes/unreleased -maxdepth 1 -name "${tipo}-*.md" 2>/dev/null | sort)"
		[ -n "${archivos}" ] || continue
		hay=1
		echo "### ${tipo}"
		echo ""
		while IFS= read -r f; do
			echo "- **$(basename "${f}")**"
			while IFS= read -r linea; do
				[ -n "${linea}" ] && echo "  ${linea}"
			done <"${f}"
		done <<<"${archivos}"
		echo ""
	done
	[ "${hay}" -eq 1 ] || echo "(ninguno: todos los cambios fueron internos)"
	echo "## 2. Commits desde ${base}"
	echo ""
	git log --no-merges --format='- %h %s' ${rango} 2>/dev/null | head -40
	echo ""
	echo "## 3. Archivos tocados"
	echo ""
	git diff --stat "${rango}" 2>/dev/null | tail -20
	echo ""
	echo "## 4. Documentos que suelen necesitar actualización"
	echo ""
	echo "| Documento | Revisar si… |"
	echo "|---|---|"
	echo "| \`README.md\` | cambió lo que el programa hace, cómo se instala o cómo se configura |"
	echo "| \`AGENTS.md\` | cambió el proceso de desarrollo, el flujo de ramas o una regla |"
	echo "| \`CONTRIBUTING.md\` | cambió el proceso de contribución |"
	echo "| \`plan/alcance.md\` | cambió el alcance, un requisito o una decisión de diseño |"
	echo "| \`NOTICE\` | se agregó o quitó una dependencia |"
	echo ""
	echo "## 5. Qué se espera de vos"
	echo ""
	echo "1. Escribir la sección de \`CHANGELOG.md\` en prosa, desde el punto de vista de quien usa"
	echo "   el programa: qué puede hacer ahora que antes no, o qué dejó de romperse. Sin jerga"
	echo "   interna, sin números de commit, sin inventar nada que no esté en los fragmentos o en"
	echo "   el diff. Una línea por cambio; dos como máximo si hace falta contexto."
	echo "2. Actualizar sólo los documentos del punto 4 que de verdad cambiaron."
	echo "3. \`tools/changelog.sh prepare v${version}\` para ensamblar la sección."
	echo "4. \`make check\`, commit firmado."
} >"${salida}"

cat "${salida}"
echo ""
echo "---"
echo "expediente guardado en ${salida} (ignorado por git)"
