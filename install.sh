#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Instala ntfy-desktop sin root, en ~/.local por defecto.
# No escribe configuración: eso lo hace el usuario en ~/.config/ntfy-desktop/.
#
#   ./install.sh                     instala en ~/.local
#   ./install.sh --prefix /usr/local  instala donde se indique (puede requerir root)
#   ./install.sh --uninstall         quita lo instalado
#   ./install.sh --dry-run           muestra qué haría, sin tocar nada

set -euo pipefail

PREFIJO="${HOME}/.local"
DESINSTALAR=0
SIMULAR=0

uso() {
	sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix) PREFIJO="${2:?falta la ruta}"; shift 2 ;;
	--prefix=*) PREFIJO="${1#*=}"; shift ;;
	--uninstall) DESINSTALAR=1; shift ;;
	--dry-run) SIMULAR=1; shift ;;
	-h | --help) uso 0 ;;
	*) echo "opción desconocida: $1" >&2; uso 1 ;;
	esac
done

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARIO="${RAIZ}/bin/ntfy-desktop"
DESTINO_BIN="${PREFIJO}/bin/ntfy-desktop"
DESTINO_DESKTOP="${PREFIJO}/share/applications/ntfy-desktop.desktop"
DESTINO_SVG="${PREFIJO}/share/icons/hicolor/scalable/apps/ntfy-desktop.svg"

ejecutar() {
	if [ "${SIMULAR}" -eq 1 ]; then
		echo "  [simulado] $*"
	else
		"$@"
	fi
}

avisar_entorno() {
	local escritorio="${XDG_CURRENT_DESKTOP:-}"
	local herramienta=""
	case "${escritorio^^}" in
	*KDE*) herramienta="kdialog" ;;
	*GNOME*) herramienta="zenity" ;;
	esac
	if [ -n "${herramienta}" ] && ! command -v "${herramienta}" >/dev/null 2>&1; then
		echo "aviso: ${escritorio} detectado pero '${herramienta}' no está instalado." >&2
		echo "       sin él no se pueden mostrar diálogos de decisión." >&2
	fi
}

if [ "${DESINSTALAR}" -eq 1 ]; then
	echo "desinstalando de ${PREFIJO}"
	ejecutar rm -f "${DESTINO_BIN}" "${DESTINO_DESKTOP}" "${DESTINO_SVG}"
	for tamano in 64 128 256 512; do
		ejecutar rm -f "${PREFIJO}/share/icons/hicolor/${tamano}x${tamano}/apps/ntfy-desktop.png"
	done
	ejecutar rm -f "${PREFIJO}/lib/systemd/user/ntfy-desktop.service"
	echo "listo. La configuración en ~/.config/ntfy-desktop/ no se tocó."
	exit 0
fi

if [ ! -x "${BINARIO}" ]; then
	echo "no hay binario compilado en ${BINARIO}."
	echo "compilá primero:  make build"
	exit 1
fi

echo "instalando en ${PREFIJO}"
ejecutar install -Dm755 "${BINARIO}" "${DESTINO_BIN}"
ejecutar install -Dm644 "${RAIZ}/packaging/ntfy-desktop.desktop" "${DESTINO_DESKTOP}"
ejecutar install -Dm644 "${RAIZ}/assets/icon/ntfy-desktop.svg" "${DESTINO_SVG}"
for tamano in 64 128 256 512; do
	origen="${RAIZ}/assets/icon/ntfy-desktop-${tamano}.png"
	if [ -f "${origen}" ]; then
		ejecutar install -Dm644 "${origen}" \
			"${PREFIJO}/share/icons/hicolor/${tamano}x${tamano}/apps/ntfy-desktop.png"
	fi
done

if [ "${SIMULAR}" -eq 0 ]; then
	command -v update-desktop-database >/dev/null 2>&1 &&
		update-desktop-database "${PREFIJO}/share/applications" >/dev/null 2>&1 || true
	command -v gtk-update-icon-cache >/dev/null 2>&1 &&
		gtk-update-icon-cache -q "${PREFIJO}/share/icons/hicolor" >/dev/null 2>&1 || true
	avisar_entorno
	case ":${PATH}:" in
	*":${PREFIJO}/bin:"*) ;;
	*) echo "aviso: ${PREFIJO}/bin no está en PATH" ;;
	esac
fi

echo "listo. Probá:  ${DESTINO_BIN}"
echo "las traducciones van dentro del binario; no hay archivos de idioma que copiar."
