<!-- SPDX-License-Identifier: GPL-3.0-or-later -->

# ntfy-desktop — configuración asistida

**Extensión de `plan/alcance.md`.** Este documento describe una funcionalidad nueva: crear, editar y
verificar la configuración desde el propio programa, en texto y en el escritorio. Está escrito para
trocearse después en issues detallados.

Estado: **propuesta para revisión**. No hay código.

---

## 1. Problema

Hoy la configuración es un archivo que se escribe a mano, en una ruta fija, sin ninguna ayuda del
programa:

- Para saber si el servidor existe, si es ntfy, si la contraseña sirve o si el topic se puede leer,
  hay que arrancar el daemon y leer el journal. El error aparece **al usar** la configuración, no al
  escribirla.
- Agregar o quitar un topic es editar a mano una línea separada por comas, con un formato que se
  valida recién al cargar (`topic = "casa,,x"` falla después, no al escribir).
- No hay forma de ver la configuración efectiva sin abrir el archivo, y `Config.String()` —que no
  imprime la contraseña— no está expuesto por ningún comando.
- Cambiar de servidor o de credenciales no tiene ninguna verificación previa: si la contraseña está
  mal, el síntoma es un 401 en el journal del daemon.

## 2. Qué agrega

Un subcomando `config` con dos frentes sobre el mismo motor:

| Frente | Cómo | Cuándo |
|---|---|---|
| Texto | `ntfy-desktop config`, pregunta por entrada estándar | servidores, SSH, sesión sin escritorio |
| Gráfico | `ntfy-desktop config --gui`, diálogos de `kdialog` | sesión Plasma |

### 2.1 Operaciones

| Operación | Comando | Qué hace |
|---|---|---|
| Ver | `config --show` | imprime la configuración efectiva (sin contraseña) y la ruta |
| Verificar | `config --check` | valida archivo, servidor, credenciales y cada topic; no escribe nada |
| Crear | `config` (sin archivo) | asistente completo y, al final, escritura con confirmación |
| Cambiar una clave | `config --set server=https://…` | valida el valor y reescribe solo esa clave |
| Agregar topic | `config topic --add avisos` | valida el nombre y la lista resultante |
| Quitar topic | `config topic --remove avisos` | valida que quede al menos uno |
| Asistente gráfico | `config --gui` | las mismas operaciones, un paso por diálogo |

Reglas comunes:

- **Nada se escribe sin confirmar.** El asistente muestra el resultado y pide confirmación; los
  comandos con `--set`/`--add`/`--remove` escriben solo si todas las validaciones pasan.
- **Un archivo inválido nunca se reemplaza.** Se escribe en un temporal, se valida el temporal con el
  cargador real, y solo entonces se renombra sobre el original (atómico), con `0600` y dejando un
  `.bak` de la versión anterior.
- **Se reescribe la línea, no el archivo.** Al cambiar una clave o la lista de topics se sustituye esa
  línea, se conserva el comentario que va detrás del valor y las demás líneas quedan intactas.
- **La contraseña nunca viaja por argumentos.** Se lee de la entrada estándar, de la variable que
  indique el propio comando o de `kdialog --password`; nunca como `--password=…`, que quedaría en
  `ps` y en el historial.

### 2.2 Validaciones, en el orden en que se hacen

| Paso | Cómo se comprueba | Qué se acepta |
|---|---|---|
| 1. Forma de la URL | cargador de `internal/config` | `http`/`https`, sin query ni fragmento |
| 2. ¿Es un ntfy? | `GET {server}/v1/health` | 200 con `healthy` en el JSON |
| 3. Credenciales | `GET {server}/v1/account` con Basic | 200 → válidas; 401 → rechazadas; 200 con `role=anonymous` → el servidor no pide credenciales |
| 4. Cada topic | nombre válido + lista resultante | 1 a 64 de `[A-Za-z0-9_-]`, sin duplicados ni vacíos |
| 5. Permiso de lectura | abrir `{server}/{topic}/json`, cortar en el primer evento | `open` → puede leer; 401/403 → no |
| 6. Permiso de escritura (opcional, solo si se publica) | no se prueba: publicar en un topic real deja un mensaje | se informa el rol y los límites de `/v1/account` |

Cada paso tiene timeout propio y corto (3 s): un servidor que no contesta es un error del paso, no
una espera del programa.

## 3. Hechos verificados (2026-11-20, contra ntfy.sh)

Probado con `curl` desde esta máquina, no leído de la documentación:

| Comprobación | Resultado real |
|---|---|
| `GET /v1/health` | `200` · `{"healthy":true,"role":"active"}` |
| `HEAD /v1/health` | `404` — **hay que usar GET**, no HEAD |
| `GET /v1/account` sin credenciales | `200` · `{"username":"*","role":"anonymous","limits":{…}}` |
| `GET /v1/account` con usuario y contraseña falsos | `401` · `{"code":40101,"http":401,"error":"unauthorized"}` |
| `GET /{topic}/json?poll=1` con credenciales falsas | `401` · mismo cuerpo |
| `GET /{topic}/json` (stream normal) | primer evento `{"event":"open"}` |
| `GET /{topic}/json?poll=1` | **no** envía `open`: el poll no sirve para probar permisos |

Dos consecuencias de diseño:

- **`/v1/health` es la firma de ntfy.** Un servidor que responde 200 con otro cuerpo no es ntfy, y el
  asistente lo dice con esas palabras en vez de seguir y fallar después.
- **La prueba de permisos es un stream que se corta.** Se abre la suscripción, se espera el `open` y
  se cierra. No consume el cache ni deja mensajes.

## 4. Lo que no se puede validar, y hay que decirlo

| Caso | Por qué no se puede | Qué hace el asistente |
|---|---|---|
| Que el topic «exista» | en ntfy un topic no se crea aparte: existe al publicar. Uno vacío responde `200` igual | valida el **permiso**, no la existencia |
| Que el usuario pueda publicar | probarlo deja un mensaje real en el topic | muestra el rol de `/v1/account` y avisa |
| Que el ACL del servidor permita el topic | el cliente no ve el ACL; solo ve el 401/403 | traduce el código a «sin permiso de lectura» |
| Servidores sin `/v1/account` | es una ruta de ntfy moderno; una instancia vieja puede no tenerla | si da `404`, el paso se marca como «no verificable» y no bloquea |

## 5. Estilo KDE, sin Qt ni GTK

`plan/alcance.md` §10 decide **sin Qt ni GTK**: el producto no enlaza el toolkit, delega en
herramientas del escritorio. Un panel de configuración de KDE (KCM) queda **fuera de alcance** por
esa decisión: exigiría Qt/C++ y una compilación distinta.

Lo que sí encaja es un asistente con `kdialog`, que ya es la superficie de decisión del producto:

```
config --gui
  1. kdialog --inputbox   "URL del servidor"
  2. kdialog --yesno      "¿Usa usuario y contraseña?"     → si: --inputbox + --password
  3. kdialog --checklist  topics existentes (marcar/quitar) + --inputbox para agregar
  4. barra de progreso    verificando servidor, credenciales y topics
  5. kdialog --textbox    resumen de lo que se va a escribir (sin contraseña)
  6. kdialog --yesno      "¿Escribo el archivo?"           → escritura atómica
```

Detalles que hacen la diferencia:

- **Un paso por diálogo**, no un formulario gigante: es lo que el producto ya hace con las decisiones.
- **El resumen final muestra la contraseña como `••••`** y el archivo tal como quedará.
- **Si el usuario cancela en cualquier paso, no se escribe nada.**
- **`--gui` sin `kdialog` (o en GNOME)** cae a `zenity` con los mismos pasos; si no hay ninguno de los
  dos, cae al modo texto y lo dice. No falla.

La referencia visual es `plan/configuracion-ui.png` (mockup de cuatro de las cinco pantallas) y el
brief para quien implemente el frente gráfico está en `plan/configuracion-ui.md`.

## 6. Piezas nuevas

| Pieza | Responsabilidad | Por qué un paquete |
|---|---|---|
| `internal/setup` | las operaciones (leer, cambiar, validar, escribir) sin saber de UI | se prueba sin abrir diálogos ni red |
| `internal/config` (extensión) | `Render`/`Write` atómico y `Set`/`AddTopic`/`RemoveTopic` sobre el archivo | ya es el dueño del formato |
| `internal/ntfy` (extensión) | `Probe`: health, account y permiso de lectura | ya es el dueño del protocolo |
| `cmd/ntfy-desktop` | subcomando `config`, cableado de los dos frentes | punto único de composición |

`internal/setup` declara las interfaces que consume (`Prober`, `Prompter`) y los adaptadores
(`text`, `kdialog`, `zenity`) las implementan: misma regla que `Notifier`/`Decider`.

## 7. Decisiones que este documento propone

| Decisión | Motivo |
|---|---|
| Un motor, dos frentes (texto y gráfico) | la lógica se prueba una vez; el escritorio solo dibuja |
| Validar contra el servidor **antes** de escribir | el error aparece donde se comete |
| Escritura atómica con `.bak` | un fallo a mitad no deja una configuración a medias |
| Contraseña por stdin o `--password` de kdialog, nunca por argv | no queda en `ps` ni en el historial |
| `/v1/health` como prueba de «es ntfy» | es la única ruta que responde sin credenciales |
| La prueba de permisos es un stream cortado en `open` | no consume cache ni publica nada |
| KCM descartado | choca con «sin Qt ni GTK» (§10 del alcance) |

## 8. Decisiones resueltas (2026-11-20)

| Pregunta | Decisión | Consecuencia en el diseño |
|---|---|---|
| ¿Qué pasa con los comentarios al reescribir? | **Se preservan comentarios y orden**: se reescribe solo la línea que cambia | el escritor trabaja por línea, no por documento; si la línea no se puede mapear, se cae a canónico con aviso y `.bak` |
| ¿Perfiles múltiples? | **No**: un servidor y varios topics, como hoy | la configuración sigue siendo de una sola suscripción; `--config` y `[perfil]` quedan fuera de esta extensión |
| ¿Contraseña en el keyring? | **No por ahora**: sigue en el archivo con `0600` | `secret-tool` queda como fase posterior, no entra en los issues de esta extensión |
| ¿Se guarda sin poder verificar? | **Sí**, con advertencia explícita y el paso marcado | el asistente distingue «verificado» de «no verificado» y lo muestra en el resumen |
| ¿Códigos de salida de `--check`? | **Sí**: 0 ok, 2 archivo, 3 servidor, 4 credenciales, 5 permiso de topic | los códigos son parte del contrato del comando y se prueban en la tabla de casos |


## 9. Criterios de aceptación

- `config --check` sobre una configuración válida dice qué verificó y termina en 0, sin escribir nada.
- `config --check` devuelve 2 (archivo), 3 (servidor), 4 (credenciales) o 5 (permiso de topic) según
  qué falló, y el código se prueba en la tabla de casos.
- Con una contraseña incorrecta, el asistente **no** escribe el archivo y nombra el paso que falló.
- `config topic --add x` deja el archivo válido y conserva las demás claves; `--remove` se niega a
  dejar la lista vacía.
- Al cambiar una clave, los comentarios y el orden de las líneas no tocadas quedan idénticos
  (se comprueba comparando las líneas, no el archivo entero).
- El archivo escrito tiene `0600` y un `.bak` con la versión anterior.
- Interrumpir el asistente en cualquier paso no modifica el archivo (se comprueba con el hash antes y
  después).
- En Plasma, `config --gui` completa el flujo con diálogos nativos, y cancelar no deja rastro.
- Sin `kdialog` ni `zenity`, `--gui` cae a texto con un aviso, no con un error.
- La contraseña no aparece en ningún log, ni en `ps`, ni en el resumen final.

## 10. Riesgos

- **La validación depende de rutas de ntfy** (`/v1/health`, `/v1/account`). Una instancia vieja o un
  proxy que las intercepte dan falsos negativos: por eso el paso «no verificable» existe.
- **`kdialog` puede no estar** en una instalación mínima; el respaldo a texto es obligatorio.
- **Reescribir el archivo puede perder comentarios** del usuario si se elige el modo canónico.
- **Una validación con credenciales reales deja un intento fallido en el servidor** (si hay rate limit
  o fail2ban por IP, conviene un solo intento y un mensaje claro).

## 11. Hitos

Sin fechas. Cada uno se cierra cuando se puede probar.

1. **Motor**: `internal/setup` con lectura, cambios y escritura atómica; pruebas sin red.
2. **Verificación**: `Probe` contra un ntfy real; los cuatro estados (sano, credenciales, sin permiso,
   no verificable) con sus códigos de salida.
3. **Texto**: `config`, `--show`, `--check`, `topic --add/--remove`.
4. **Gráfico**: `config --gui` con `kdialog`, y el respaldo `zenity`/texto.
5. **Documentación**: `docs/configuration.md` y el README con el flujo nuevo.

## 12. Mapa a issues (borrador)

| Issue propuesto | Depende de | Alcance |
|---|---|---|
| `config: motor de cambios y escritura atómica` | — | `internal/config` + `internal/setup`: reescritura **por línea** que preserva comentarios, `.bak`, `0600`, pruebas de tabla |
| `config: verificar servidor, credenciales y permiso de lectura` | motor | `Probe` en `internal/ntfy`, códigos 0/2/3/4/5, timeouts de 3 s |
| `config: subcomando de texto` | motor + verificación | `config`, `--show`, `--check`, `topic --add/--remove` |
| `config: asistente gráfico con kdialog y respaldo` | texto | `--gui`, un paso por diálogo, fallback a zenity/texto |
| `docs: configuración asistida` | texto | `docs/configuration.md`, README, `plan/alcance.md` §4.3 |

Fuera de esta extensión, por §8: perfiles múltiples, `--config`, `[perfil]` y el keyring del escritorio.
