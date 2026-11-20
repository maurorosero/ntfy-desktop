<!--
  Plantilla de pull request. Mantené el PR chico y con un solo tema.
-->

## What this changes

<!-- Una o dos frases. Qué hace el cambio, no cómo. -->

## Why

<!-- El problema que resuelve. Si hay un issue, enlazalo: Closes #123 -->

## How it was tested

<!--
  No alcanza con "compila". Contá qué corriste:
  - make check
  - ./bin/ntfy-desktop en KDE / GNOME / sin escritorio
  - qué viste en pantalla
-->

## Checklist

- [ ] Los commits están firmados (`git commit -s`)
- [ ] `make check` pasa en local
- [ ] Agregué pruebas para la lógica nueva
- [ ] No agregué dependencias (o expliqué por qué en el issue)
- [ ] No hay cgo, ni Qt, ni GTK
- [ ] Las cadenas visibles van por catálogo, no escritas en el código
- [ ] Actualicé `CHANGELOG.md` si el cambio se nota desde afuera

## Notas para quien revisa

<!-- Decisiones que conviene mirar con lupa, dudas, cosas que dejaste afuera a propósito. -->
