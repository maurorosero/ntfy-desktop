# ntfy-desktop — lectura en voz alta de los avisos (TTS)

Estado: **diseño, sin implementar**. Escrito el 2026-11-22 sobre `devs` (0.3.5).
Este documento es la fuente para abrir los issues del §13; no describe código que exista.

## 1. Problema

Un aviso visual exige mirar la pantalla. Cuando el mensaje importa —una falla de backup, un
servidor caído, una decisión que espera respuesta— el aviso se pierde si el usuario está en otra
ventana, con la pantalla bloqueada o lejos del equipo. El teléfono ya resuelve esto: la app de HA
puede leer el mensaje en voz alta con `tts_text`. El escritorio no tiene equivalente.

## 2. Qué se busca

Que el cliente pueda **leer en voz alta** el aviso que llega, con tres controles:

- **Global**: una clave de configuración decide si se lee nada, solo lo crítico o todo.
- **Por mensaje**: un tag reservado permite que el emisor pida la lectura de un mensaje puntual.
- **Sin dependencias nuevas**: se usa un sintetizador que ya esté en el sistema, ejecutado como
  proceso externo, igual que `kdialog`, `zenity` y `xdg-open`.

## 3. Qué no entra

- **No es un asistente de voz**: no escucha, no interpreta, no responde.
- **No elige la voz por idioma del mensaje**: usa la que diga la configuración.
- **No reemplaza la notificación**: el aviso visual sigue existiendo; la voz se suma.
- **No lee adjuntos, ni historial, ni la bandeja.**
- **No hay control de velocidad ni de tono** en la primera versión.

## 4. Hechos verificados que condicionan el diseño

Comprobados en la máquina de referencia (`mrlaptop`, Plasma 6.6.7, Wayland) el 2026-11-22:

- **`spd-say 0.12.1` está instalado** en `/usr/bin/spd-say` y lista voces en español, con
  variantes por nombre (`Spanish (Spain)+Andrea`, `+Antonio`, …). No hay `espeak-ng`, `piper`,
  `festival` ni `say`.
- **El servicio `speech-dispatcher` de usuario está `inactive`** y `spd-say` arranca el daemon
  por demanda: la primera lectura tarda más que las siguientes.
- **El programa ya ejecuta binarios externos** con `exec.CommandContext` en cinco lugares
  (`internal/dialog/kdialog.go`, `internal/dialog/zenity.go`, `internal/dialog/prompt.go`,
  `internal/action/action.go`, `internal/notify/open.go`). Un TTS es el mismo patrón.
- **`internal/notify/compose.go` no envía ningún hint de sonido.** El sonido lo decide el
  servidor de notificaciones del escritorio según la urgencia. La voz es otra capa, no un hint.
- **`ntfy.Message` ya trae `Tags []string` y `Priority int`** (`internal/ntfy/message.go`), que
  son los dos campos que hacen falta para decidir.
- **La configuración es un único `config.toml` con cinco claves** (`server`, `topic`, `user`,
  `password`, `log-level`) y **cualquier clave desconocida es error** con número de línea
  (`internal/config/config.go`). Agregar claves obliga a tocar parser, validación, tests y doc.
- **`internal/history` deduplica por `id`** y conserva 500 mensajes por topic en
  `$XDG_CACHE_HOME/ntfy-desktop/history` (`DefaultLimit`). Al reconectar, el cliente re-lee lo
  que llegó mientras estaba apagado y el store descarta lo repetido: el TTS tiene que usar esa
  misma señal, o lee en voz alta los backups del día anterior al abrir sesión.

## 5. Decisión de arquitectura

Paquete nuevo `internal/speak`, con el mismo criterio que el resto del proyecto:

```
internal/speak/
    speak.go        contrato Speaker + selección de texto + truncado
    command.go      implementación por proceso externo (spd-say)
    speak_test.go   pruebas sin audio, con un ejecutable de mentira
```

Cumple los tres criterios de §8.1 del alcance: cambia por un motivo distinto (el sintetizador
cambia cuando cambia el sistema de voz), se puede probar solo (con un doble de `exec`) y prevé
más de una implementación (un `nop` para cuando no hay sintetizador).

Contrato, declarado en el paquete que lo consume:

```go
// Speaker lee en voz alta el texto que se le da.
type Speaker interface {
    Speak(ctx context.Context, text string) error
}
```

El cableado va en `cmd/ntfy-desktop`, como todo lo demás. `internal/notify` no conoce la
existencia de la voz: **la lectura se dispara en el mismo punto donde el mensaje se muestra**,
que es el daemon, no el notificador.

## 6. Configuración

Dos claves nuevas, ambas opcionales:

| Clave | Valores | Por defecto | Qué hace |
|---|---|---|---|
| `speak` | `off`, `critical`, `all` | `off` | Cuándo se lee, además del tag por mensaje |
| `speak-lang` | código de idioma (`es`, `en`, …) | vacío | Idioma de la voz; vacío usa el del sistema |

Reglas de validación, con el estilo actual (error con número de línea):

- `speak` fuera de los tres valores → error.
- `speak-lang` con caracteres fuera de `[a-z-]` → error. No se valida contra el catálogo del
  sintetizador: eso depende de la máquina y cambiaría entre versiones.
- Si `speak` no es `off` y `spd-say` no está en el `PATH`, **el programa arranca igual** con un
  `WARN` en el registro y la función desactivada. Mismo trato que `xdg-open`.

## 7. Gatillos

| Condición | `off` | `critical` | `all` |
|---|---|---|---|
| Mensaje con prioridad < 4, sin tag | no | no | **sí** |
| Mensaje con prioridad ≥ 4 | no | **sí** | **sí** |
| Mensaje con tag `tts`, cualquier prioridad | no | **sí** | **sí** |
| Eventos del stream (`open`, `keepalive`) | no | no | no |

El tag reservado es **`tts`** (en minúsculas, comparado sin distinguir mayúsculas). Se consume:
no se dibuja como etiqueta ni se convierte en emoji.

`off` apaga todo, **incluido el tag**. Un emisor no puede hacer hablar el equipo si el dueño lo
desactivó: la configuración manda sobre el mensaje.

## 8. Comportamiento

1. Llega un mensaje que cumple §7 y **es nuevo para el historial**.
2. Se arma el texto: `title` y `message`, separados por coma; si no hay título, solo el cuerpo.
   Se quita el markdown básico (`*`, `_`, `#`, enlaces en formato `[texto](url)` → `texto`).
3. Se trunca a **200 runas** cortando en el último espacio, con `…` si se cortó.
4. Se encola. **Una sola voz a la vez**, una goroutine con cola de 8; si la cola se llena, se
   descarta el mensaje nuevo y se registra `WARN` (mejor perder una lectura que encolar veinte).
5. El proceso corre con `exec.CommandContext` y un plazo de **15 s**; al vencer se mata.
6. El resultado se registra: `INFO` si se leyó, `WARN` si falló, nunca el texto completo si
   supera 80 caracteres.

`spd-say` se invoca como `<ruta> -w -l <lang> <texto>`, con `-w` para esperar el fin de la
locución y así respetar la cola. Sin `speak-lang`, se omite `-l`.

## 9. Seguridad

- **El texto va como argumento, nunca por `sh -c`.** Un mensaje con `; rm -rf /` se lee como
  texto y no ejecuta nada. Es la misma regla que ya aplica `internal/action`.
- **El binario se resuelve con `exec.LookPath` al arrancar**, una vez, y se guarda la ruta
  absoluta. No se acepta una ruta del mensaje ni de la red.
- **Límite de 200 runas** antes de tocar el proceso: acota el tiempo de voz y el riesgo de
  mensajes diseñados para saturar.
- **La cola es finita**: 8 pendientes y se descarta lo nuevo. Un emisor no puede encadenar
  lecturas indefinidas.
- **`off` gana sobre el tag**: el emisor no tiene la última palabra.

## 10. Casos borde

| Caso | Comportamiento esperado |
|---|---|
| Mensaje de solo eventos (`keepalive`) | No se lee (`IsMessage()` falso) |
| Título vacío | Se lee solo el cuerpo |
| Cuerpo vacío y título presente | Se lee el título |
| Texto de 5 000 caracteres | Se leen 200 runas y `…` |
| Tag `tts` repetido en varios mensajes | Se lee cada uno, sujeto a la cola |
| El sintetizador tarda o no responde | Se mata a los 15 s, `WARN`, la cola sigue |
| Dos mensajes en el mismo segundo | Una voz, en orden de llegada |
| Reconexión tras horas apagado | **No se releen** los mensajes recuperados: ya estaban en el historial |
| `spd-say` desaparece del sistema | Las lecturas fallan con `WARN`; el programa sigue avisando |

## 11. Criterios de aceptación

- Con `speak = "off"`, no se lanza ningún proceso de voz en toda la sesión.
- Con `speak = "critical"`, un mensaje de prioridad 4 se lee y uno de prioridad 3 no.
- Un mensaje de prioridad 3 con tag `tts` se lee en modo `critical`.
- Con `speak = "off"`, un mensaje con tag `tts` **no** se lee.
- Dos mensajes seguidos producen una sola voz continua, en orden, sin solaparse.
- Un mensaje de 5 000 caracteres produce como máximo 200 runas de audio.
- Sin `spd-say` instalado, el programa arranca, avisa con `WARN` y sigue notificando.
- Un mensaje con metacaracteres de shell se lee como texto y no ejecuta nada.
- Tras reiniciar el cliente con historial, no se lee nada de lo ya recibido.
- `speak = "loud"` produce un error de configuración con el número de línea.

## 12. Pruebas

- **Unitarias** (`internal/speak`): selección de texto, truncado en palabra, decisión por modo
  y tag, cola serializada, timeout con un ejecutable de mentira que duerme.
- **Unitarias** (`internal/config`): las dos claves, valor inválido con línea, ausencia de ambas.
- **De integración** (`cmd/ntfy-desktop`): con un `Speaker` falso, el daemon llama a `Speak`
  exactamente una vez por mensaje nuevo y ninguna por los recuperados del historial.
- **Manual en `mrlaptop`**: `speak = "all"`, publicar un mensaje y confirmar que se oye; luego
  `off` y confirmar silencio. Evidencia en el issue.

## 13. Mapa de issues

Cada uno se puede cerrar solo y deja el programa funcionando:

| # | Issue | Toca | Cierra con |
|---|---|---|---|
| 1 | `internal/speak`: contrato, `spd-say` y pruebas | paquete nuevo | pruebas unitarias verdes |
| 2 | `internal/config`: `speak` y `speak-lang` | parser, validación, tests, doc | `make check` |
| 3 | Daemon: disparar la lectura solo en mensajes nuevos | `cmd/ntfy-desktop` | prueba de integración |
| 4 | Documentación y changelog | `docs/configuration.md`, `CHANGELOG`, catálogos | revisión de texto |
| 5 | Verificación manual en Plasma | máquina de referencia | evidencia en el issue |

Dependencias: 2 y 3 dependen de 1; 4 depende de 2; 5 va al final.

## 14. Riesgos

- **Hablar en un momento inoportuno**: `spd-say` no respeta «No molestar». Hasta que se resuelva
  (§15), la mitigación es `speak = "critical"` y apagarlo a mano.
- **Voz robótica**: `spd-say` con `espeak` suena mecánico. `piper` daría otra calidad, pero suma
  una dependencia y modelos que descargar.
- **Primera lectura lenta**: el daemon de voz arranca por demanda; el primer aviso puede tardar
  un par de segundos.
- **Ruido por emisor**: un topic compartido permite que cualquiera con permiso de escritura haga
  hablar el equipo. El tag acota el alcance, no lo elimina.

## 15. Lo que queda fuera, y por qué

- **Respetar «No molestar»**: requiere leer el estado de inhibición del escritorio por D-Bus.
  Es posible, pero agrega una dependencia de plataforma a una función que hoy es opcional. Se
  reevalúa si el ruido aparece en la práctica.
- **`piper` u otro sintetizador**: la primera versión usa lo que el sistema ya tenga.
- **Elegir la voz por nombre** (`speak-voice`): la variante por nombre es específica de cada
  instalación; queda para cuando `speak-lang` no alcance.
- **Leer el historial a pedido** (comando `speak-last`): es otra función, no esta.
