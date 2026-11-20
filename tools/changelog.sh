#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# El changelog se arma con fragmentos: cada cambio visible deja un archivo en
# .changes/unreleased/ y al publicar se juntan en una sección de versión.
#
#   tools/changelog.sh prepare v0.1.0    arma la sección, borra los fragmentos usados
#   tools/changelog.sh check             verifica el estado (para CI)
#   tools/changelog.sh list              muestra los fragmentos pendientes
#   tools/changelog.sh notes v0.2.0       imprime la sección de esa versión (cuerpo de la release)
#
# Formato de un fragmento: el nombre empieza con el tipo y el contenido es la
# entrada, tal como va a leerse en el changelog.
#
#   .changes/unreleased/added-subscription.md
#   .changes/unreleased/fixed-dialog-timeout.md

set -euo pipefail

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRAGMENTOS="${RAIZ}/.changes/unreleased"
CHANGELOG="${RAIZ}/CHANGELOG.md"
REPO_URL="https://github.com/maurorosero/ntfy-desktop"

# Orden en que aparecen las secciones, y su nombre en el changelog.
TIPOS=("added" "changed" "deprecated" "removed" "fixed" "security")
TITULOS=("Added" "Changed" "Deprecated" "Removed" "Fixed" "Security")

morir() {
	echo "$*" >&2
	exit 1
}

fragmentos_de() {
	local tipo="$1"
	find "${FRAGMENTOS}" -maxdepth 1 -name "${tipo}-*.md" 2>/dev/null | sort
}

cmd_list() {
	local total=0
	for i in "${!TIPOS[@]}"; do
		local tipo="${TIPOS[$i]}"
		local archivos
		archivos="$(fragmentos_de "${tipo}")"
		[ -n "${archivos}" ] || continue
		echo "${TITULOS[$i]}:"
		while IFS= read -r f; do
			echo "  - $(basename "${f}")"
			total=$((total + 1))
		done <<<"${archivos}"
	done
	if [ "${total}" -eq 0 ]; then
		echo "sin fragmentos pendientes"
	fi
	echo ""
	echo "total: ${total}"
}

construir_seccion() {
	local version="$1" fecha="$2"

	echo "## [${version}] - ${fecha}"

	local hubo=0
	for i in "${!TIPOS[@]}"; do
		local tipo="${TIPOS[$i]}"
		local archivos
		archivos="$(fragmentos_de "${tipo}")"
		[ -n "${archivos}" ] || continue
		hubo=1
		echo ""
		echo "### ${TITULOS[$i]}"
		echo ""
		while IFS= read -r f; do
			while IFS= read -r linea; do
				[ -n "${linea}" ] || continue
				case "${linea}" in
				[Dd]ocs:*) continue ;;
				-) echo "- " ;;
				*) echo "- ${linea}" ;;
				esac
			done <"${f}"
		done <<<"${archivos}"
	done

	if [ "${hubo}" -eq 0 ]; then
		echo ""
		echo "### Changed"
		echo ""
		echo "- Mantenimiento interno, sin cambios visibles."
	fi
}

cmd_prepare() {
	local version="${1:-}"
	[ -n "${version}" ] || morir "falta la versión, por ejemplo: tools/changelog.sh prepare v0.1.0"
	version="${version#v}"
	local tag="v${version}"

	grep -q "^## \[Unreleased\]" "${CHANGELOG}" || morir "CHANGELOG.md no tiene una sección [Unreleased]"
	grep -qE "^## \[v?${version}\]" "${CHANGELOG}" && morir "la versión ${version} ya existe en CHANGELOG.md"

	local fecha
	fecha="$(date +%F)"
	local seccion
	seccion="$(construir_seccion "${version}" "${fecha}")"

	# Insertar antes de la primera sección de versión que sigue a [Unreleased].
	local inicio siguiente
	inicio="$(grep -n '^## \[Unreleased\]' "${CHANGELOG}" | head -1 | cut -d: -f1)"
	siguiente="$(awk -v s="${inicio}" 'NR > s && /^## \[/ { print NR; exit }' "${CHANGELOG}")"
	[ -n "${siguiente}" ] || morir "no encontré dónde insertar la sección"

	{
		head -n "$((siguiente - 1))" "${CHANGELOG}"
		echo ""
		echo "${seccion}" | sed '/^$/N;/^\n$/D'
		echo ""
		tail -n "+${siguiente}" "${CHANGELOG}"
	} >"${CHANGELOG}.nuevo"
	mv "${CHANGELOG}.nuevo" "${CHANGELOG}"

	# Enlaces del pie: [Unreleased] apunta a la versión nueva; se agrega el suyo.
	sed -i "s|^\[Unreleased\]: .*|[Unreleased]: ${REPO_URL}/compare/${tag}...HEAD|" "${CHANGELOG}"
	if ! grep -q "^\[${version}\]: " "${CHANGELOG}"; then
		sed -i "/^\[Unreleased\]: /i [${version}]: ${REPO_URL}/releases/tag/${tag}" "${CHANGELOG}"
	fi

	# La sección Unreleased vuelve a quedar vacía.
	awk 'BEGIN{u=0} /^## \[Unreleased\]/{u=1; print; getline; print ""; print "- Nothing yet."; print ""; next} u==1 && /^## \[/{u=0} u==1{next} {print}' \
		"${CHANGELOG}" >"${CHANGELOG}.tmp" && mv "${CHANGELOG}.tmp" "${CHANGELOG}"

	local usados=0
	for tipo in "${TIPOS[@]}"; do
		local archivos
		archivos="$(fragmentos_de "${tipo}")"
		[ -n "${archivos}" ] || continue
		while IFS= read -r f; do
			rm -f "${f}"
			usados=$((usados + 1))
		done <<<"${archivos}"
	done

	echo "sección ${version} armada con ${usados} fragmentos, fecha ${fecha}"
	echo "revisá el diff de CHANGELOG.md antes de mergear"
}

# Fragmentos que piden documentación: los que **introduce este cambio** y que
# afectan lo que el usuario ve. Los fragmentos viejos, ya integrados, no cuentan.
# Un fragmento se exime con una línea "Docs: none".
fragmentos_con_docs() {
	local base="$1"
	if [ -z "${base}" ] || ! git rev-parse --verify --quiet "${base}" >/dev/null; then
		echo 0
		return
	fi
	local n=0
	while IFS= read -r f; do
		[ -n "${f}" ] || continue
		case "${f}" in
		.changes/unreleased/*-*.md) ;;
		*) continue ;;
		esac
		grep -qiE '^[Dd]ocs:[[:space:]]*(none|n/?a)' "${RAIZ}/${f}" 2>/dev/null && continue
		n=$((n + 1))
	done < <(git diff --name-only --diff-filter=AM "${base}...HEAD" -- .changes/unreleased/ 2>/dev/null)
	echo "${n}"
}

cmd_check() {
	local base="${1:-}"
	local fallo=0
	# HOTFIX=1 sólo lo pone CI cuando el PR lleva la etiqueta hotfix: se avisa y
	# no se bloquea. Sirve para un fix prioritario; lo que se saltee queda a la vista.
	local hotfix="${HOTFIX:-0}"

	# 1. La sección [Unreleased] tiene que existir.
	grep -q "^## \[Unreleased\]" "${CHANGELOG}" || {
		echo "CHANGELOG.md no tiene [Unreleased]"
		fallo=1
	}

	# 2. ¿Hay fragmentos pendientes?
	local pendientes=0
	for tipo in "${TIPOS[@]}"; do
		local n
		n="$(fragmentos_de "${tipo}" | grep -c . || true)"
		pendientes=$((pendientes + n))
	done

	# 3. Si hay cambios visibles, la documentación tiene que venir en el mismo cambio.
	local exigen_docs
	exigen_docs="$(fragmentos_con_docs "${base}")"
	if [ "${exigen_docs}" -gt 0 ] && [ -n "${base}" ] && git rev-parse --verify --quiet "${base}" >/dev/null; then
		local docs_tocados
		docs_tocados="$(git diff --name-only "${base}...HEAD" -- \
			README.md AGENTS.md CONTRIBUTING.md plan/ 2>/dev/null | grep -c . || true)"
		if [ "${docs_tocados}" -eq 0 ]; then
			echo "hay ${exigen_docs} cambio(s) visible(s) y ningún documento cambió"
			echo "actualizá README.md, plan/ o AGENTS.md — o marcá el fragmento con 'Docs: none' si de verdad no aplica"
			[ "${hotfix}" = "1" ] && echo "HOTFIX: se deja pasar, y queda registrado" || fallo=1
		else
			echo "documentación actualizada junto al cambio: OK (${docs_tocados} archivo/s)"
		fi
	fi

	# 4. Si el PR va a main, el CHANGELOG tiene que haber cambiado.
	local destino="${base##*/}"
	if [ "${destino}" = "main" ] && [ -n "${base}" ] && git rev-parse --verify --quiet "${base}" >/dev/null; then
		if git diff --quiet "${base}...HEAD" -- CHANGELOG.md; then
			echo "el PR va a main y CHANGELOG.md no cambió"
			echo "corré: tools/changelog.sh prepare vX.Y.Z"
			[ "${hotfix}" = "1" ] && echo "HOTFIX: se deja pasar, y queda registrado" || fallo=1
		else
			echo "CHANGELOG.md cambió respecto de ${base}: OK"
		fi
	fi

	if [ "${hotfix}" = "1" ]; then
		echo ""
		echo "ATENCIÓN: gate saltado por hotfix. El pendiente queda a la vista en el changelog."
	fi

	if [ "${fallo}" -ne 0 ]; then
		exit 1
	fi

	if [ "${pendientes}" -eq 0 ]; then
		echo "sin fragmentos pendientes (los cambios internos no llevan)"
	else
		echo "fragmentos pendientes: ${pendientes}"
	fi
}

# cmd_notes imprime la sección de una versión, sin el encabezado ##, para que el
# workflow de entrega la use como cuerpo de la release: lo que se lee en la
# release es exactamente lo que dice el changelog.
cmd_notes() {
	local version="${1:-}"
	[ -n "${version}" ] || morir "falta la versión, por ejemplo: tools/changelog.sh notes v0.2.0"
	version="${version#v}"

	grep -qE "^## \\[${version}\\]" "${CHANGELOG}" ||
		morir "CHANGELOG.md no tiene una sección para ${version}"

	awk -v v="${version}" '
		$0 ~ "^## \\[" v "\\]" { dentro = 1; next }
		dentro && /^## \[/ { exit }
		dentro { print }
	' "${CHANGELOG}" | sed -e '1{/^$/d}' -e '${/^$/d}'
}

case "${1:-}" in
prepare) shift; cmd_prepare "$@" ;;
check) shift; cmd_check "$@" ;;
list) cmd_list ;;
notes) shift; cmd_notes "$@" ;;
-h | --help | "")
	sed -n '2,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
	;;
*)
	morir "orden desconocida: ${1} (usá prepare, check o list)"
	;;
esac
