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

La configuración declara el modo; si falta el par, se asume el primero. Un token (bearer) queda
como variante a evaluar si un servidor lo exige.

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
| Historial local y estado leído/no leído | siguiente |
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
| Enviar mensaje: cuerpo y título | siguiente |
| Prioridad, etiquetas, enlace, markdown | siguiente |
| Adjunto por URL (`attach`) | siguiente |
| Adjunto por archivo (subida) | después |
| Entrega programada (`X-Delay`) | después |
| Acciones incluidas en el mensaje publicado | después |

### 5.5 El cliente

| Función | Estado |
|---|---|
| Configuración: servidor, topic, credenciales | **núcleo** |
| Registro de diagnóstico | **núcleo** |
| Arranque con la sesión (servicio de usuario) | siguiente |
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

## 9. Lenguaje: Go

Decidido. Motivos, en orden:

1. **Distribución**: binario único, sin intérprete ni runtime que instalar.
2. **Sin dependencias**: `net/http` y `encoding/json` cubren el transporte; `kdialog`,
   `zenity` y `notify-send` se ejecutan por `os/exec`. Sin cgo.
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
| Decisión: **`kdialog`** en Plasma, `zenity` en GNOME | único mecanismo que dibuja botones y persiste |
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

---

## Nota: uso interno

En nuestra instalación apuntamos a nuestro propio servidor y tomamos las credenciales de
nuestro gestor de secretos. Eso **es uso, no diseño**: no aparece en el código, ni en la
configuración de ejemplo, ni en las pruebas del producto.
