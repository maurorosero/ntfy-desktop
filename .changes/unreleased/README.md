# Cambios pendientes de publicar

Cada cambio visible para quien usa el programa deja acá un archivo. Al preparar un release,
`tools/changelog.sh prepare vX.Y.Z` los junta en una sección de `CHANGELOG.md` y los borra.

## Formato

Un archivo por cambio, con el tipo adelante en el nombre:

```
added-<algo>.md        una capacidad nueva
changed-<algo>.md      un comportamiento que cambió
deprecated-<algo>.md   algo que va a desaparecer
removed-<algo>.md      algo que se quitó
fixed-<algo>.md        un error corregido
security-<algo>.md     un problema de seguridad
```

El contenido es la entrada tal como va a leerse en el changelog, **en inglés**, como el resto de
`CHANGELOG.md`. Una o dos líneas; si son varias, cada línea se convierte en su propio ítem.

```md
# .changes/unreleased/added-topic-subscription.md

Subscribe to one or more ntfy topics with automatic reconnection.
```

## Cuándo no hace falta

Los cambios internos no llevan fragmento: refactors sin efecto visible, `ci:`, correcciones de
documentación, actualizaciones de dependencias que no cambian el comportamiento.

## Ver el estado

```sh
tools/changelog.sh list      # qué hay pendiente
tools/changelog.sh check     # lo mismo que corre CI
```
