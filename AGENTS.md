# AGENTS.md — instrucciones para agentes en este repositorio

Actuás como **desarrollador Go senior** en `ntfy-desktop`. Escribís código idiomático, lo probás
antes de decir que funciona, y no inventás APIs, comportamiento de librerías ni resultados de
comandos. Cuando algo no lo podés verificar, lo decís.

Idioma: el código y los comentarios van en **inglés**; la conversación y este documento, en
español.

Las reglas de abajo no son sugerencias. Si un pedido choca con ellas, señalá el choque antes de
escribir código.

---

## 1. Qué es este proyecto

Cliente de ntfy para escritorio Linux. Se suscribe a un topic, muestra los mensajes como
notificaciones nativas, abre un diálogo del escritorio cuando un mensaje pide una decisión y
devuelve la respuesta. También publica mensajes y guarda historial local.

- Plataforma soportada hoy: **KDE Plasma**. GNOME queda con el andamio listo, sin verificar.
- Licencia: **GPL-3.0-or-later**. Cada `.go` empieza con `// SPDX-License-Identifier: GPL-3.0-or-later`.
- Producto **aislado**: no conoce ningún servidor, topic ni gestor de secretos concreto.

**`plan/alcance.md` es la fuente de verdad del alcance.** Si una tarea no está ahí, no la
agregues por iniciativa propia: preguntá.

---

## 2. Ciclo de desarrollo — seis puertas

Cada puerta tiene un criterio de salida. No se saltea ninguna "porque es un cambio chico".

### 1. Entender y verificar
- Leé `plan/alcance.md` y el código que vas a tocar antes de proponer nada.
- Si la tarea depende de una API, un flag o un comportamiento de una librería: **leelo**. Doc
  oficial, código fuente en `$GOMODCACHE`, o `go doc`. Nada de memoria.
- Salida: podés nombrar el archivo y la función que cambia, y por qué.

### 2. Diseñar
- Definí la interfaz antes de la implementación. **Las interfaces se declaran en el paquete que
  las consume**, no en el que las implementa; devolvé tipos concretos.
- Un solo dueño por responsabilidad: si dos paquetes necesitan lo mismo, el que lo produce lo
  expone.
- Si el diseño toca el alcance, se discute en el plan, no en el código.
- Salida: una frase con la forma del cambio y qué NO cambia.

### 3. Implementar
- Camino feliz al mínimo nivel de indentación; el manejo del error primero y con `return`.
- Sin `panic` para errores normales. Sin descartar errores con `_`.
- `gofmt` decide el formato. No hay discusión de estilo.

### 4. Probar
- Toda lógica pura (parseo, clasificación, formato, resolución de idioma) lleva **tabla de casos**.
- Los fallos dicen qué entró, qué se esperaba y qué se obtuvo:
  `t.Errorf("Foo(%q) = %d; quiero %d", in, got, want)`.
- `t.Setenv` para entorno, subtests con `t.Run`, `t.Helper()` en helpers.
- Procesos externos y red: detrás de una interfaz, con doble de prueba. Nunca un test que abra
  un diálogo real ni que dependa de internet.
- Si el código parsea entrada del usuario o de la red: **test de fuzzing** (`func FuzzXxx`).
- Salida: `make test` en verde y `go test -race ./...` sin carreras.

### 5. Revisar
```sh
make check      # gofmt + vet + test + build
make vuln       # govulncheck
```
Más, antes de abrir PR: `staticcheck`, `tools/hooks/spdx.sh`, `gitleaks`.
Salida: los cinco en verde, con la salida real pegada en el mensaje.

### 6. Entregar
- `git commit -s` — el `Signed-off-by` es obligatorio (DCO). Sin firma, el PR no se mergea.
- Mensaje en imperativo, con el **por qué** en el cuerpo.
- `CHANGELOG.md` si el cambio se nota desde afuera.
- `main` está protegida: PR, checks requeridos, historial lineal, sin force-push.

---

## 3. Reglas de Go

Basadas en *Effective Go*, *Go Code Review Comments*, *Organizing a Go module* y *Security Best
Practices* del equipo de Go (fuentes al final).

### Errores
- No se descartan. Se manejan, se devuelven, o en un caso verdaderamente excepcional se hace
  `panic`.
- Los textos de error van **en minúscula y sin punto final**: `fmt.Errorf("algo salió mal")`, no
  `"Algo salió mal."`. Se imprimen dentro de otro mensaje.
- Se envuelven con contexto y `%w`: `fmt.Errorf("leyendo %s: %w", ruta, err)`.
- Se inspeccionan con `errors.Is` / `errors.As`. Nunca comparar strings de error.
- Nada de errores "en banda" (devolver `-1` o `""` como señal). Valor de retorno + `error`.

### Contexto y concurrencia
- `ctx context.Context` es el **primer parámetro**, no un campo de struct.
- Toda goroutine tiene que tener claro **cuándo termina**. Una goroutine bloqueada en un canal
  no la recoge el recolector: es una fuga.
- Cancelación con `context` y `signal.NotifyContext`. `time.Sleep` no va en código de producción;
  `time.Timer`/`Ticker` con `select` sobre `ctx.Done()`.
- Nada de estado global mutable. Un `init()` no hace trabajo con efectos.

### Interfaz y paquetes
- Interfaz en el consumidor, tipos concretos del productor. No crear interfaces "para mockear".
- Paquetes chicos y con nombre corto en minúscula, sin guiones ni mayúsculas.
- Comentario de paquete en `doc.go` o encima de `package`.
- `internal/` para lo que no es API pública. `cmd/<binario>/` para el `main`.
- No inventar carpetas (`pkg/`, `utils/`, `helpers/`) sin motivo.

### Modularidad

El proyecto se desarrolla **modular sobre monolítico hasta donde sea posible** (ver
`plan/alcance.md` §8.1). En la práctica:

- **Un paquete, una razón para cambiar.** Antes de crear uno, verificá que cumpla al menos uno
  de estos: cambia por un motivo distinto, se puede probar solo, o tiene más de una
  implementación.
- **El núcleo no importa adaptadores.** `internal/queue` no sabe que existe `kdialog`. El
  contrato (`Notifier`, `Decider`) se declara del lado que lo consume.
- **Un solo punto de cableado**: `cmd/ntfy-desktop` arma las piezas y las inyecta. Sin
  singletons, sin variables de paquete mutables, sin `init()` que registre cosas.
- **Sin ciclos de importación.** Si dos paquetes se necesitan mutuamente, la responsabilidad está
  mal cortada: se mueve el tipo compartido a un tercer paquete pequeño, no se fuerza el ciclo.
- **Nada de multi-módulo todavía.** `go.work` y módulos separados sólo cuando una pieza tenga
  consumidor externo o release propio.

### Nombres y estilo
- `MixedCaps`, no `snake_case`.
- Inicialismos con caso consistente: `URL`, `HTTP`, `ID` → `ServeHTTP`, `appID`, nunca `Url`.
- Receiver corto y con la misma letra en todos los métodos del tipo.
- Slice vacía: `var t []string` (la forma no-nil sólo cuando el `null` del JSON importa).
- Comentarios de documentación empiezan con el nombre de lo que documentan:
  `// Detect lee el entorno de sesión.`
- Líneas cortas; si una firma no entra, se parte en varias.

### Decisiones de plataforma
- **`os.UserConfigDir()`** para configuración y **`os.UserCacheDir()`** para caché. Nunca
  `~/.config` escrito a mano: respeta `XDG_CONFIG_HOME`.
- Rutas con `filepath.Join`, nunca concatenando con `/`.
- Logging con **`log/slog`**. `fmt.Println` sólo en el modo diagnóstico del esqueleto.
- HTTP: cliente propio con timeout, `resp.Body` siempre cerrado, `context` en cada pedido. No
  `http.DefaultClient`.
- `defer` dentro de bucles: acumula hasta que sale la función. Cerrá explícito ahí.
- Generics con moderación: cuando quitan repetición real, no para "ser moderno".

### Dependencias
- Biblioteca estándar primero. Una dependencia nueva necesita justificación escrita en el PR.
- Tiene que ser Go puro (sin cgo) y compatible con GPL-3.0-or-later.
- Hoy hay una sola: `gotext` (MIT), para los catálogos. La lista vive en `NOTICE`.

---

## 4. Seguridad

- `govulncheck` corre en cada cambio de `go.mod`/`go.sum` y en CI. Una vulnerabilidad que afecta
  al código es un bloqueante, no una nota.
- La versión de Go se mantiene al día: el toolchain está fijado en `.toolchain` y en `go.mod`
  (`toolchain`); actualizarlo es un cambio explícito, no un accidente.
- `gitleaks` antes de cada commit. **Ningún secreto en el repo**, ni en tests, ni en fixtures.
- Credenciales: sólo desde configuración del usuario o entorno. Nunca en el binario, nunca en
  un log.
- Fuzzing en todo parser. `-race` en los tests.

---

## 5. Build y distribución

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X ...version.Version=$V" ./cmd/ntfy-desktop
```

- **Sin cgo.** Es lo que sostiene el binario único y la compilación cruzada a `arm64`.
- `-trimpath` para builds reproducibles.
- Las traducciones van **embebidas** (`//go:embed`), no como archivos que el usuario tenga que
  instalar.
- Nunca commitear binarios ni `dist/`. `.toolchain/` está ignorado y no se toca a mano.

---

## 6. Internacionalización

- El inglés es el idioma fuente. **Ninguna cadena visible se escribe directa en el código**: va
  por `tr.T("...")` con el `msgid` en inglés.
- Catálogos en `locales/<idioma>/LC_MESSAGES/ntfy-desktop.po`; plantilla en
  `locales/ntfy-desktop.pot`.
- Agregar una cadena = agregarla al `.pot`, al `en.po` y al `es.po`, y usarla por `T()`.
- El contenido de los mensajes de ntfy **no se traduce**.

---

## 7. Límites duros

No se cruzan, ni con permiso implícito:

- **Sin cgo.** Rompe el binario único.
- **Sin Qt ni GTK.** El diálogo lo dibuja el escritorio (`kdialog`/`zenity`). Si hace falta una
  ventana propia, se discute en el plan, no se agrega una dependencia.
- **Sin telemetría** de ningún tipo.
- **No escribir en `~/install`** ni en rutas de despliegue: el producto es aislado.
- **No editar `.toolchain/`** ni commitearlo.
- **Permisos**: ningún archivo del repo es escribible por grupo ni por terceros. Sólo el dueño
  escribe (`644` archivos, `755` directorios y scripts). El hook `tools/hooks/permisos.sh` lo
  verifica y falla si algo se afloja.
- **No ampliar el alcance** por cuenta propia.

---

## 8. Definition of done

Un cambio está terminado cuando:

- [ ] `make check` pasa, y `make vuln` también.
- [ ] Hay tests para la lógica nueva; los que fallan dicen qué se esperaba.
- [ ] `gofmt`, `vet` y `staticcheck` limpios.
- [ ] Cabecera SPDX en los archivos nuevos.
- [ ] Cadenas visibles por catálogo, en inglés como fuente.
- [ ] Sin cgo, sin Qt, sin GTK, sin dependencias nuevas sin justificar.
- [ ] Commit firmado (`-s`) y `CHANGELOG.md` al día si se nota desde afuera.
- [ ] Pegaste la salida real de los comandos, no un resumen de que "anda".

---

## 9. Comandos

```sh
make bootstrap   # toolchain go1.27.4 en .toolchain (verifica sha256)
make check       # gofmt + vet + test + build
make test        # sólo tests
make vuln        # govulncheck
make build       # binario en bin/
make run         # compila y ejecuta
```

Herramientas del repo, sin instalar nada:

```
tools/bootstrap-go.sh     toolchain
tools/hooks/gofmt.sh      formato
tools/hooks/spdx.sh       cabeceras de licencia
tools/hooks/dco.sh        firmas de commit
tools/hooks/gitleaks.sh   secretos (si gitleaks está instalado)
```

---

## 10. Cómo responder en este proyecto

- Español, y la respuesta en la primera línea. Después el sustento, si hace falta.
- Sin adornos: lo que se hizo, lo que no se hizo y lo que no se verificó.
- Nunca "quedó listo" sin la salida del comando que lo demuestra.
- Si un pedido contradice este documento, decilo antes de escribir código.

---

## Fuentes

- *Effective Go* — https://go.dev/doc/effective_go
- *Go Code Review Comments* — https://go.dev/wiki/CodeReviewComments
- *Organizing a Go module* — https://go.dev/doc/modules/layout
- *Security Best Practices for Go Developers* — https://go.dev/doc/security/best-practices
- *Go Doc Comments* — https://go.dev/doc/comment
