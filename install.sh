#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Instala ntfy-desktop sin root, en ~/.local por defecto.
# No escribe configuración: eso lo hace el usuario en ~/.config/ntfy-desktop/.
#
#   ./install.sh                     instala binario, acceso de menú, iconos y unidad de usuario
#   ./install.sh --bin-only          instala sólo el binario, sin unidad ni menú
#   ./install.sh --prefix /usr/local  instala donde se indique (puede requerir root)
#   ./install.sh --uninstall         quita lo instalado
#   ./install.sh --dry-run           muestra qué haría, sin tocar nada
#
# La unidad se escribe con la ruta real del binario: no asume /usr/bin.

set -euo pipefail

PREFIJO="${HOME}/.local"
DESINSTALAR=0
SIMULAR=0
SOLO_BINARIO=0

uso() {
	sed -n '2,13p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix) PREFIJO="${2:?falta la ruta}"; shift 2 ;;
	--prefix=*) PREFIJO="${1#*=}"; shift ;;
	--bin-only) SOLO_BINARIO=1; shift ;;
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
# XDG_DATA_HOME (o /usr/local/share) es donde systemd --user busca unidades de
# usuario además de ~/.config/systemd/user: así el prefijo manda y no se asume
# ninguna ruta absoluta.
DESTINO_UNIDAD="${PREFIJO}/share/systemd/user/ntfy-desktop.service"
NOMBRE_UNIDAD="ntfy-desktop.service"

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

# hay_bus_usuario dice si esta sesión puede hablarle a systemd --user. Por SSH
# sin sesión gráfica no se puede, y ahí se imprime el comando en vez de fallar.
hay_bus_usuario() {
	systemctl --user show-environment >/dev/null 2>&1
}

if [ "${DESINSTALAR}" -eq 1 ]; then
	echo "desinstalando de ${PREFIJO}"
	if hay_bus_usuario; then
		ejecutar systemctl --user disable --now "${NOMBRE_UNIDAD}" >/dev/null 2>&1 || true
	fi
	ejecutar rm -f "${DESTINO_BIN}" "${DESTINO_UNIDAD}"
	if [ "${SOLO_BINARIO}" -eq 0 ]; then
		ejecutar rm -f "${DESTINO_DESKTOP}" "${DESTINO_SVG}"
		for tamano in 64 128 256 512; do
			ejecutar rm -f "${PREFIJO}/share/icons/hicolor/${tamano}x${tamano}/apps/ntfy-desktop.png"
		done
	fi
	if hay_bus_usuario; then
		ejecutar systemctl --user daemon-reload >/dev/null 2>&1 || true
	fi
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

if [ "${SOLO_BINARIO}" -eq 1 ]; then
	echo "sólo el binario: no se instaló el acceso de menú ni la unidad de usuario."
	echo "listo. Probá:  ${DESTINO_BIN}"
	exit 0
fi

ejecutar install -Dm644 "${RAIZ}/packaging/ntfy-desktop.desktop" "${DESTINO_DESKTOP}"
ejecutar install -Dm644 "${RAIZ}/assets/icon/ntfy-desktop.svg" "${DESTINO_SVG}"
for tamano in 64 128 256 512; do
	origen="${RAIZ}/assets/icon/ntfy-desktop-${tamano}.png"
	if [ -f "${origen}" ]; then
		ejecutar install -Dm644 "${origen}" \
			"${PREFIJO}/share/icons/hicolor/${tamano}x${tamano}/apps/ntfy-desktop.png"
	fi
done

# La unidad se copia con la ruta real del binario en ExecStart: el archivo del
# repositorio sirve para una instalación del sistema, no para ~/.local.
unidad_generada() {
	sed "s|^ExecStart=.*|ExecStart=${DESTINO_BIN} --daemon|" \
		"${RAIZ}/packaging/ntfy-desktop.service"
}

if [ "${SIMULAR}" -eq 1 ]; then
	echo "  [simulado] instalar ${DESTINO_UNIDAD} con ExecStart=${DESTINO_BIN} --daemon"
	echo "  [simulado] systemctl --user daemon-reload && systemctl --user enable --now ${NOMBRE_UNIDAD}"
else
	mkdir -p "$(dirname "${DESTINO_UNIDAD}")"
	unidad_generada >"${DESTINO_UNIDAD}"
	chmod 644 "${DESTINO_UNIDAD}"
fi

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

	# El cliente vive de la sesión gráfica: se arranca con ella y se para con ella.
	if hay_bus_usuario; then
		systemctl --user daemon-reload
		if systemctl --user enable --now "${NOMBRE_UNIDAD}"; then
			echo "unidad de usuario: habilitada y corriendo"
		else
			echo "aviso: la unidad quedó instalada pero no arrancó; mirá:" >&2
			echo "       journalctl --user -u ${NOMBRE_UNIDAD} -n 20" >&2
		fi
	else
		echo "la unidad quedó en ${DESTINO_UNIDAD}; para activarla desde tu sesión:"
		echo "       systemctl --user daemon-reload && systemctl --user enable --now ${NOMBRE_UNIDAD}"
	fi
fi

echo "listo. Probá:  ${DESTINO_BIN}"
echo "las traducciones van dentro del binario; no hay archivos de idioma que copiar."
