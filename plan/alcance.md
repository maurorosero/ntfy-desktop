# ntfy-desktop — alcance

**Producto**: un cliente de ntfy para escritorio Linux.

Se conecta a un servidor ntfy, muestra los mensajes como notificaciones nativas, permite
actuar sobre ellos y publicar mensajes. Producto aislado: no depende de ninguna instalación
concreta —servidor, topic y credenciales entran por configuración—.

**Plataforma inicial**: KDE Plasma y derivadas. GNOME y derivadas: evaluable, ver §4.

Estado: **borrador para revisión**. No hay código todavía.

---

## 1. Problema

ntfy tiene cliente web, aplicación móvil y CLI, pero **no tiene cliente de escritorio** para
Linux. Quien trabaja en el escritorio tiene que abrir un navegador para leer un mensaje, y no
puede actuar sobre él sin salir de donde está.

## 2. Qué hace

1. Se conecta a un servidor ntfy y se suscribe a uno o más topics.
2. Cada mensaje se muestra como **notificación del escritorio**.
3. Muestra el contenido: título, cuerpo, prioridad, etiquetas, adjuntos.
4. Si el mensaje trae **acciones**, abre un diálogo con esas opciones y devuelve la respuesta.
5. Permite **publicar** mensajes en un topic.
6. Guarda historial local: lo que llegó mientras no mirabas no se pierde.

## 3. Qué no hace

- No es un servidor ntfy ni lo administra.
- No reemplaza al cliente móvil ni al web: convive con ellos.
- No cifra contenido.
- No funciona sin sesión gráfica.

## 4. Plataformas

| Entorno | Estado | Cómo |
|---|---|---|
| KDE Plasma y derivadas (Kubuntu, KDE neon) | **alcance actual** | `kdialog` para diálogos, `notify-send` para avisos |
| GNOME y derivadas (Ubuntu, Fedora Workstation) | **capa lista, sin implementar** | `zenity` para diálogos; falta máquina de prueba |
| Otros (XFCE, Cinnamon, Sway) | fuera de alcance | — |

**Detección en ejecución, no un producto por librería.** El producto no enlaza Qt ni GTK:
delega el diálogo en la herramienta del escritorio.

```
KDE    → kdialog
GNOME  → zenity
otro   → el que exista (zenity suele estar en ambos)
```

**Qué significa "capa lista"**: el núcleo habla con una interfaz de tres operaciones
(`avisar`, `decidir`, `bandeja`) y nunca con `kdialog` directamente. Agregar GNOME es escribir
una implementación nueva de esa interfaz, sin tocar el núcleo.

## 4.1 Identidad

El cliente **no tiene cuentas propias**: toma el usuario registrado en la sesión del sistema
(`$USER`, `getuid()`). Ese usuario determina:

- dónde vive la configuración (`~/.config/ntfy-desktop/`),
- dónde vive el historial y el estado,
- qué identidad se informa cuando el cliente responde una petición.

No hay inicio de sesión en el cliente, ni usuarios que administrar dentro de él.

## 4.2 Autenticación contra ntfy

Dos modos, y sólo dos por ahora:

| Modo | Cuándo | Cómo |
|---|---|---|
| **Sin credenciales** | topic público | sólo la URL del servidor y el topic |
| **Usuario y contraseña** | topic protegido | HTTP Basic con el par configurado |

La configuración declara el modo: sin credenciales cuando no hay ni `user` ni `password`, HTTP
Basic cuando están las dos. Un token (bearer) queda como variante a evaluar si un servidor lo
exige.

Media pareja —usuario sin contraseña, o contraseña sin usuario— **no** es un modo: es un error de
configuración y se rechaza al cargar, con el nombre de la clave que falta. Suscribirse en
silencio sin credenciales a un topic protegido se vería como un topic vacío, no como un error.

## 4.3 Configuración

Un solo archivo, escrito por el usuario. El paquete nunca lo crea.

```
os.UserConfigDir()/ntfy-desktop/config.toml     # normalmente ~/.config/ntfy-desktop/config.toml
```

`os.UserConfigDir()` respeta `XDG_CONFIG_HOME`, así que la ruta no se escribe a mano. El archivo
es TOML, y de TOML se acepta el subconjunto que hace falta: comentarios, líneas en blanco y una
asignación `clave = "valor"` por línea, con los escapes `\"`, `\\`, `\n`, `\t` y `\r`. Lo que no
entra en ese subconjunto se rechaza con el número de línea: una clave mal escrita no se ignora en
silencio.

```toml
server    = "https://ntfy.example.org"   # obligatoria, http o https, sin query ni fragmento
topic     = "alerts"                     # obligatoria: 1 a 64 de [A-Za-z0-9_-]
user      = "andrea"                     # opcional, junto con password
password  = "…"                          # opcional, junto con user
log-level = "info"                       # opcional: debug, info, warn o error
```

Sin `user` ni `password` el cliente se suscribe sin credenciales. Las secciones `[tabla]` todavía
no se aceptan: hoy la configuración es de una sola suscripción y las claves van sueltas.

## 5. Funcionalidades de un cliente ntfy

Inventario según la documentación de ntfy (`docs.ntfy.sh/subscribe/api`, `docs.ntfy.sh/publish`).
La columna Estado define qué entra en cada hito.

### 5.1 Suscripción

| Función | Estado |
|---|---|
| Stream JSON por HTTP (`/topic/json`) | **núcleo** |
| Sin credenciales (topic público) | **núcleo** |
| Usuario y contraseña (HTTP Basic) | **núcleo** |
| Identidad tomada del usuario de la sesión | **núcleo** |
| Reconexión con espera creciente | **núcleo** |
| Recuperar lo perdido (`since=<id>`) | **núcleo** |
| Varios topics a la vez | siguiente |
| Filtros del servidor: `id`, `message`, `title`, `priority`, `tags` | siguiente |
| Poll y último mensaje (`poll=1`, `since=latest`) | siguiente |
| SSE y WebSocket | después |
| Mensajes programados (`scheduled=1`) | después |

### 5.2 Mensajes

| Función | Estado |
|---|---|
| Título, cuerpo, prioridad, etiquetas, hora, topic | **núcleo** |
| Abrir el enlace del mensaje (`click`) | **núcleo** |
| Historial local y estado leído/no leído | **núcleo**: `internal/history` y el comando `history`; falta que el daemon lo alimente |
| Markdown en el cuerpo | siguiente |
| Adjuntos: mostrar, descargar, abrir | siguiente |
| Icono propio del mensaje (`icon`) | después |
| Secuencia (`X-Sequence-ID`): actualizar o borrar un aviso | después |
| Búsqueda en el historial | después |

### 5.3 Acciones

| Función | Estado |
|---|---|
| Mostrar las acciones que trae el mensaje | **núcleo** |
| Acción `view`: abrir una URL | **núcleo** |
| Acción `http`: llamar una URL (aprobar/rechazar) | **núcleo** |
| Caducidad de una petición de decisión | **núcleo** |
| Responder publicando en un topic de respuesta | evaluable (§11) |
| Acción `broadcast`: reenviar a otros dispositivos | después |
| `inline-reply`: responder texto desde el aviso | evaluable (no funciona en Plasma) |

### 5.4 Publicación

| Función | Estado |
|---|---|
| Enviar mensaje: cuerpo y título | **núcleo** |
| Prioridad, etiquetas, enlace, markdown | **núcleo** |
| Adjunto por URL (`attach`) | siguiente |
| Adjunto por archivo (subida) | después |
| Entrega programada (`X-Delay`) | después |
| Acciones incluidas en el mensaje publicado | después |

### 5.5 El cliente

| Función | Estado |
|---|---|
| Configuración: servidor, topic, credenciales | **núcleo** |
| Registro de diagnóstico con nivel configurable (`log-level`, `--log-level`) | **núcleo** |
| Arranque con la sesión (servicio de usuario) y cierre limpio con `SIGTERM` | **núcleo** |
| Ajustes por topic: silenciar, prioridad mínima | siguiente |
| Varios perfiles de servidor | después |
| Bandeja del sistema | después (requiere D-Bus) |

## 6. Requisitos no funcionales

| # | Requisito |
|---|---|
| N1 | Un mensaje aparece como aviso en menos de 5 segundos |
| N2 | Nunca hay dos diálogos de decisión abiertos a la vez |
| N3 | Cerrar un diálogo sin responder no descarta la petición |
| N4 | Las credenciales no viven en el código: configuración o entorno |
| N5 | El producto no conoce ninguna instalación concreta |
| N6 | Sin dependencias más allá de las herramientas del escritorio (hasta la bandeja) |
| N8 | La identidad es la del usuario de la sesión; el cliente no administra cuentas |
| N7 | Sin conexión: reintenta y recupera al volver |

## 7. Hechos verificados que condicionan el diseño

Probados en el escritorio de referencia (Kubuntu, Plasma 6.6.7, Wayland):

- **La notificación del sistema no dibuja botones.** Las acciones se envían y el servidor
  anuncia la capacidad `actions`, pero no se renderizan. Por eso la decisión no va por ahí.
- **`expire_timeout = 0` cierra la notificación al instante**: el popup usa ese valor como
  intervalo de su temporizador. Los avisos llevan timeout explícito, nunca 0.
- **`kdialog` dibuja botones nativos y no expira**: quedó abierto 40 s hasta que lo mató un
  timeout externo. Es la superficie de decisión.
- **Dos `kdialog` a la vez se apilan**: la segunda tapa a la primera. De ahí N2.
- **Con un panel del sistema abierto, Plasma retiene los avisos.**

## 8. Arquitectura propuesta

```
        ntfy (HTTP)
            │  stream JSON
      ┌─────▼─────┐
      │   núcleo  │  suscripción · cola · historial · publicación
      └─────┬─────┘
            │  interfaz de adaptador
      ┌─────▼─────────────┐
      │ avisar · decidir  │
      └─────┬─────────────┘
       KDE  │  GNOME
    kdialog │  zenity
 notify-send│ notify-send
```

El núcleo no sabe qué escritorio hay. El adaptador se elige una vez al arrancar y expone tres
operaciones: `avisar`, `decidir`, `bandeja`.

### 8.1 Modularidad

El desarrollo es **modular sobre monolítico hasta donde sea posible**: cada pieza con una
responsabilidad, un contrato explícito y la posibilidad de probarse sola. Lo que no se hace es
partir por partir.

```
cmd/ntfy-desktop/     composición: flags, cableado, arranque. Sin lógica de producto.
internal/ntfy/        protocolo: stream JSON, reconexión, since, autenticación, publicar
internal/queue/       cola de peticiones y su estado
internal/action/      ejecución de la acción elegida: `view` y `http`
internal/history/     almacenamiento local de mensajes
internal/config/      carga y validación de la configuración
internal/notify/      contrato Notifier + implementación por D-Bus
internal/dialog/      contrato Decider + adaptadores kdialog / zenity
internal/i18n/        catálogos embebidos
internal/platform/    detección de usuario, idioma y escritorio
internal/version/     versión inyectada en el build
```

Criterios que justifican un paquete nuevo — tiene que cumplir al menos uno:

1. **Cambia por un motivo distinto** que el resto (el protocolo cambia cuando cambia ntfy; el
   adaptador cambia cuando cambia un escritorio).
2. **Se puede probar solo**, sin abrir procesos ni red.
3. **Tiene más de una implementación** o se prevé que la tenga (KDE/GNOME; notificación real vs
   doble de prueba).

Reglas de dependencia:

- **El núcleo no importa adaptadores.** `internal/queue` no sabe que existe `kdialog`.
- **Las interfaces se declaran en el paquete que las consume**; el adaptador devuelve tipos
  concretos y no impone una interfaz.
- **Un solo punto de cableado**: `cmd/ntfy-desktop` arma las piezas. Nada de singletons ni
  estado global.
- **Sin ciclos.** Si dos paquetes se necesitan mutuamente, la responsabilidad está mal cortada.

Lo que **no** se hace todavía: separar en varios módulos de Go (`go.work`). Un módulo por pieza
sólo se justifica cuando una de ellas tenga consumidor externo o ciclo de release propio; hoy
sería costo sin beneficio.

## 9. Lenguaje: Go

Decidido. Motivos, en orden:

1. **Distribución**: binario único, sin intérprete ni runtime que instalar.
2. **Dependencias mínimas**: `net/http` y `encoding/json` cubren el transporte; `kdialog`,
   `zenity` y `notify-send` se ejecutan por `os/exec`. La única dependencia externa es
   `gotext` (MIT, Go puro) para los catálogos de idioma. Sin cgo.
3. **Arranque rápido**: el cliente vive en la sesión y debe estar listo al iniciar.
4. **El precio, dicho de frente**: la bandeja del sistema va por D-Bus (StatusNotifierItem) y
   ahí sí hará falta una librería. Es el único punto donde Go deja de ser cero-dependencias, y
   está en el hito que agrega la bandeja, no antes.

## 10. Decisiones tomadas

| Decisión | Motivo |
|---|---|
| Transporte: **ntfy**, URL del servidor por configuración | protocolo abierto; cualquier instancia |
| Topic y credenciales: **por configuración** | el producto no conoce ninguna instalación |
| Aviso: **`notify-send`** con timeout explícito | no roba el foco; cero dependencias |
| Decisión: **`kdialog` con botones** en Plasma, **`zenity`** con botones en GNOME | dibuja botones reales, persiste y permite etiquetas propias traducidas en los dos |
| **Idiomas: inglés (fuente) y español**, gettext `.po` | formato que el ecosistema ya usa; el resto entra por aporte |
| **Cola de a una petición** | N2 |
| **Sin Qt ni GTK**: adaptador por `exec`, detección en ejecución | una sola compilación, nativo en cada escritorio |
| **Identidad = usuario de la sesión** | el cliente no administra cuentas |
| **Autenticación**: sin credenciales o usuario/contraseña | los dos casos que existen hoy |
| **Lenguaje: Go** | binario único, sin dependencias hasta la bandeja |
| Repositorio: **`ntfy-desktop`**, producto aislado | se sostiene solo |

## 11. Preguntas abiertas

1. **Cómo vuelve la respuesta**: publicar en un topic de respuesta, o llamar la URL de la
   acción `http`. El primero no exige exponer nada; el segundo es el estándar de ntfy.
2. **Qué pasa cuando el emisor espera y nadie contesta**: ¿vence? ¿se reintenta?
3. **Verificación en GNOME**: falta una máquina para probar `zenity` y las notificaciones.
4. **Adjuntos**: ¿descargar y abrir, o abrir la URL en el navegador?
5. **Historial local**: ¿cuántos mensajes y dónde se guardan?

## 12. Hitos

Sin fechas. Cada hito se cierra cuando se puede probar.

1. **Lectura**: conecta, se suscribe, imprime los mensajes. Sin interfaz.
2. **Aviso**: notificación por mensaje, con prioridad y timeout correctos.
3. **Decisión**: acciones visibles, diálogo, respuesta publicada, cola de a una.
4. **Resiliencia**: reconexión con `since`, arranque con la sesión.
5. **Historial**: guardar lo recibido, leer, marcar.
6. **Publicación**: enviar mensajes desde el escritorio.
7. **Portabilidad**: adaptador de GNOME verificado en una máquina real.

## 13. Criterios de aceptación

- Un mensaje aparece como aviso en menos de 5 segundos.
- Un mensaje con acciones abre un diálogo con las opciones exactas que trae.
- Elegir una opción publica la respuesta correcta y cierra el diálogo.
- Con dos peticiones seguidas nunca hay dos diálogos abiertos.
- Apagar y encender la máquina no pierde las peticiones pendientes.
- El token nunca aparece en el código ni en los registros.

## 14. Riesgos

- **Dependencia de la sesión gráfica**: sin sesión no hay diálogo; los pendientes se acumulan.
- **El servidor ntfy es un punto único**: si cae, no hay avisos.
- **Los tokens dan acceso al topic completo**: uno filtrado permite publicar y leer.
- **Plasma puede cambiar** su comportamiento en notificaciones entre versiones.
- **GNOME sin verificar**: hasta probarlo, el soporte es una intención, no un hecho.

## 15. Licencia

**GPL-3.0-or-later.** Texto completo en `LICENSE`, y `SPDX-License-Identifier: GPL-3.0-or-later`
en la cabecera de cada archivo de código.

Es compatible con todas las dependencias previstas —`godbus` (BSD-2), `notify` (BSD-3),
`systray` (MIT), stdlib de Go (BSD-3)— y con `kdialog`/`zenity`, que se ejecutan como procesos
externos y no se enlazan.

Obligaciones que asume el proyecto:

- Quien distribuya el binario debe poder obtener el código, incluidas las modificaciones.
- Debe incluirse lo necesario para compilarlo: el script del toolchain con versión y checksum
  forma parte del cumplimiento, no es un extra.
- Los avisos de licencia de las dependencias van en `NOTICE`.

Contribuciones: **DCO** (`Signed-off-by` en cada commit), no CLA.

## 16. Repositorio y CI/CD

### Estructura

```
ntfy-desktop/
├── .toolchain/          toolchain de Go, gitignored (~250 MB)
├── tools/bootstrap-go.sh baja y verifica el toolchain (versión + sha256)
├── cmd/                 punto de entrada
├── internal/            núcleo y adaptadores
├── assets/              icono (SVG + PNG 64/128/256/512) y banner
├── packaging/           .desktop · unit de systemd · (nfpm cuando toque)
├── plan/                este documento
├── AGENTS.md            lineamientos para agentes: ciclo de desarrollo y reglas de Go
├── about.me             fuente del "About" del repositorio remoto (y sus topics)
├── description.me       fuente del campo Description del repositorio remoto
├── LICENSE · NOTICE
├── go.mod · go.sum
└── .gitignore · .gitattributes
```

### `.gitignore`

```
.toolchain/
bin/
dist/
coverage.out
*.test
.env
```

### Integración continua, dos niveles

| Nivel | Qué corre |
|---|---|
| **pre-commit** (local) | `gofmt -l` · `go vet` · `go test` · `govulncheck` · `gitleaks` |
| **Pull request** (GitHub) | build `linux/amd64` y `linux/arm64` · vet · staticcheck · test · govulncheck · gitleaks · avisos de licencia |
| **Tag** | binarios + `sha256` + `NOTICE`, release automática con goreleaser |
| **Semanal** | dependabot + `govulncheck` programado |

Nada llega al remoto si falla en local.

### Protección de `main`

```
PR obligatorio · checks requeridos · sin force-push · historial lineal
admins sujetos a la regla · aprobaciones requeridas: 0
```

Cero aprobaciones porque hay una sola identidad: el titular puede mergear su propio PR sin
segundo revisor, igual que en los otros repositorios públicos de la casa.

## 17. Idiomas (i18n)

El producto es traducible desde el primer día, no después.

**Formato: gettext (`.po`)** con `leonelquinteros/gotext` (MIT, activo, 514★), versionado en
`go.mod` y **embebido en el binario** (`//go:embed`): no hay archivos de idioma que instalar.

Los catálogos viven en `locales/<idioma>/LC_MESSAGES/ntfy-desktop.po`, con
`locales/ntfy-desktop.pot` como plantilla.

Es el formato que el ecosistema Linux ya usa: los traductores lo abren con Poedit o Weblate, las
distros lo integran, y es texto plano que se versiona y se revisa en un diff.

**Idioma fuente: inglés.** Las cadenas se escriben en inglés y se traducen; el español es la
primera traducción. Es la convención de los proyectos públicos y permite que cualquiera aporte
sin que el español sea el original.

**Idiomas soportados inicialmente: inglés y español.** El inglés es el catálogo fuente y el
español el primer `.po` traducido; los dos se mantienen completos y son requisito de release.
Cualquier otro idioma entra por aporte, sin comprometer el alcance inicial.

**Detección del idioma**: se toma del entorno de la sesión —`LANG`, `LC_ALL`, `LC_MESSAGES`— con
la misma lógica que la identidad. Cadena de respaldo: `es_PA` → `es` → `en`.

**Qué se traduce y qué no**

| Se traduce | No se traduce |
|---|---|
| Toda cadena de la interfaz | El contenido de los mensajes de ntfy |
| Etiquetas de los diálogos | Los nombres de los topics |
| Mensajes de error y registro | Los valores de configuración |
| Formatos de fecha y hora | — |

**Consecuencia en el diseño de diálogos**: los dos adaptadores renombran sus botones desde el
catálogo, así que las etiquetas son nuestras y del idioma del usuario: `kdialog --yes-label`,
`--no-label` y `--cancel-label`, y `zenity --ok-label`, `--extra-button` y `--cancel-label`.
Con dos acciones se piden con botones reales —un clic, no una lista más un botón—; con una sola,
un botón; con tres o más, la lista, porque ninguno de los dos tiene más botones.

Dos detalles que costaron verificar y están anotados en el código: `kdialog` devuelve `0` para el
sí, `1` para el no y `2` para cancelar, y **zenity devuelve `1` para su botón extra por
compatibilidad con 3.x — el mismo código que su cancelar**, así que hay que pasarle
`ZENITY_EXTRA` para poder distinguirlos.

**Control en integración continua**

- Las cadenas fuente se extraen y se comparan contra cada `.po`; si falta una traducción, avisa;
  si falta una cadena, **falla**.
- Los `.po` se compilan en el build; un `.po` roto rompe la construcción.
- Ninguna cadena visible se escribe directamente en el código.

Aportes de traducción: PR sobre el `.po` correspondiente, con DCO como el resto.

## 18. Empaquetado y distribución

Cuatro canales, del más simple al más dependiente de infraestructura.

| Canal | Para quién | Herramienta |
|---|---|---|
| **Script** (`install.sh`) sin root | cualquiera, ya | shell, sin dependencias |
| **`.deb`** | Debian, Ubuntu, Kubuntu | `nfpm` desde goreleaser |
| **`.rpm`** | Fedora, openSUSE | `nfpm` desde goreleaser |
| **`.pkg.tar.zst`** | Arch, Manjaro | `nfpm` desde goreleaser |
| **`.tar.gz`** con checksums | el resto | goreleaser (ya previsto) |

`nfpm` (MIT, 2.7k★, activo) construye los tres formatos desde un mismo archivo, sin cgo, y
goreleaser lo invoca con la sección `nfpms:`. Se configura una vez y sale en cada release.

Nota sobre `.pkg`: en Linux no existe ese formato. El equivalente en Arch es `.pkg.tar.zst`, y
es el que se cubre. macOS queda fuera del alcance del producto.

### Contenido de los paquetes

```
/usr/bin/ntfy-desktop
/usr/lib/systemd/user/ntfy-desktop.service
/usr/share/applications/ntfy-desktop.desktop
/usr/share/icons/hicolor/scalable/apps/ntfy-desktop.svg
/usr/share/locale/<idioma>/LC_MESSAGES/ntfy-desktop.mo
/usr/share/doc/ntfy-desktop/{LICENSE,NOTICE,README.md}
```

La configuración **no** la pone el paquete: vive en `~/.config/ntfy-desktop/` y la crea el
usuario. Un paquete nunca escribe en el home de nadie.

Los archivos de empaquetado viven en el repo, en `packaging/` (`.desktop`, `.service`, icono), y
los consume tanto `nfpm` como el script.

### Script de instalación

Para quien no quiere ni puede usar un paquete:

```sh
./install.sh                 # ~/.local, sin root
./install.sh --prefix /usr/local   # con root, si se prefiere
./install.sh --uninstall
```

Copia el binario, el `.desktop`, la unit de systemd de usuario y las traducciones, y verifica
que el escritorio tenga la herramienta de diálogo que corresponde. No toca nada fuera del
prefijo elegido.

### Lo que queda descartado, y por qué

**Flatpak y Snap no son viables con este diseño.** Un paquete sandbox no puede ejecutar
`kdialog` del sistema, y en Plasma el portal de notificaciones no está implementado — sólo
`notificationinhibition` en `xdg-desktop-portal-kde`. Sin diálogo del escritorio y sin portal,
no hay forma de pedir una decisión. Se reevalúa sólo si KDE implementa el portal.

### Firma y verificación

- Cada release publica `checksums.txt` (ya previsto) y una firma de ese archivo.
- Los `.deb`/`.rpm` se pueden firmar con GPG más adelante, cuando exista un repositorio propio.
- Repositorio apt/dnf propio: **después**, no en el alcance inicial.

### Cumplimiento de la licencia

Cada paquete incluye `LICENSE` y `NOTICE`, y el código fuente queda disponible en el repositorio
público. El binario se puede reconstruir con el script del toolchain incluido.

---

## Nota: uso interno

En nuestra instalación apuntamos a nuestro propio servidor y tomamos las credenciales de
nuestro gestor de secretos. Eso **es uso, no diseño**: no aparece en el código, ni en la
configuración de ejemplo, ni en las pruebas del producto.
