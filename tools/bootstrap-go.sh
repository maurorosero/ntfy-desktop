#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Instala el toolchain de Go dentro del repositorio (.toolchain/go), sin tocar el sistema.
# Verifica el sha256 oficial antes de descomprimir.

set -euo pipefail

GO_VERSION="go1.27.4"
SUMA_AMD64="7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840"
SUMA_ARM64="3392582988a273a87ab5043ee1b2c9c17a2c3c00580bf9326c47a2e0aaaeac92"

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DESTINO="${RAIZ}/.toolchain"
GO_BIN="${DESTINO}/go/bin/go"

case "$(uname -m)" in
	x86_64 | amd64) ARCO="amd64"; SUMA="${SUMA_AMD64}" ;;
	aarch64 | arm64) ARCO="arm64"; SUMA="${SUMA_ARM64}" ;;
	*)
		echo "Arquitectura no soportada por este script: $(uname -m)" >&2
		echo "Descargá el toolchain a mano desde https://go.dev/dl/" >&2
		exit 1
		;;
esac

if [ -x "${GO_BIN}" ]; then
	ACTUAL="$("${GO_BIN}" version | awk '{print $3}')"
	if [ "${ACTUAL}" = "${GO_VERSION}" ]; then
		echo "toolchain ya presente: ${ACTUAL}"
		exit 0
	fi
	echo "toolchain distinto (${ACTUAL}), se reemplaza por ${GO_VERSION}"
fi

ARCHIVO="${GO_VERSION}.linux-${ARCO}.tar.gz"
URL="https://go.dev/dl/${ARCHIVO}"

mkdir -p "${DESTINO}"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "descargando ${URL}"
if command -v curl >/dev/null 2>&1; then
	curl -fsSL --retry 3 -o "${TMP}/${ARCHIVO}" "${URL}"
else
	wget -q -O "${TMP}/${ARCHIVO}" "${URL}"
fi

echo "verificando sha256"
CALCULADA="$(sha256sum "${TMP}/${ARCHIVO}" | awk '{print $1}')"
if [ "${CALCULADA}" != "${SUMA}" ]; then
	echo "checksum no coincide" >&2
	echo "  esperado: ${SUMA}" >&2
	echo "  obtenido: ${CALCULADA}" >&2
	exit 1
fi

echo "descomprimiendo en ${DESTINO}/go"
rm -rf "${DESTINO}/go"
tar -C "${DESTINO}" -xzf "${TMP}/${ARCHIVO}"

echo "toolchain listo: $("${GO_BIN}" version)"
echo "usá:  export PATH=\"${DESTINO}/go/bin:\$PATH\"   (o 'make' que ya lo resuelve)"
