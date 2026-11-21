#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Se ejecuta al instalar o actualizar el paquete, como root.
#
# Un paquete no puede reiniciar el servicio de la sesión gráfica de otra
# persona: el cliente sigue corriendo la versión anterior hasta que su dueño lo
# reinicia. Es la única cosa del paquete que no termina sola, así que se dice.

set -eu

if [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
	echo ""
	echo "ntfy-desktop: si el cliente ya estaba corriendo en la sesión de ${SUDO_USER},"
	echo "              sigue usando la versión anterior. Para tomar esta:"
	echo ""
	echo "                  systemctl --user restart ntfy-desktop"
	echo ""
fi

exit 0
