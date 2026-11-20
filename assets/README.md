# SPDX-License-Identifier: GPL-3.0-or-later
#
# Los archivos de assets/ son parte del proyecto y caen bajo la misma licencia
# que el código: GPL-3.0-or-later.
#
# assets/icon/ntfy-desktop.svg   fuente vectorial del icono
# assets/icon/ntfy-desktop-N.png tamaños 64, 128, 256 y 512 para hicolor
# assets/banner.svg              cabecera del README (1280x640)
# assets/banner.png             versión rasterizada, para la previsualización social

# Paleta
#
#   verde profundo   #10403C
#   verde medio      #0F766E
#   casi blanco      #F0FDFA
#   turquesa claro   #5EEAD4

# Regenerar los PNG (requiere inkscape):
#
#   cd assets/icon
#   for s in 64 128 256 512; do
#     inkscape --export-type=png --export-filename=ntfy-desktop-$s.png \
#              -w $s -h $s ntfy-desktop.svg
#   done
#   cd .. && inkscape --export-type=png --export-filename=banner.png -w 1280 -h 640 banner.svg
