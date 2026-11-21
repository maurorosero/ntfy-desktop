<!-- SPDX-License-Identifier: GPL-3.0-or-later -->

# ntfy-desktop — brief de la interfaz de configuración

**Para quien implemente el frente gráfico (#42).** Este documento describe cómo se comporta el
asistente y cómo debe verse. La referencia visual es `plan/configuracion-ui.png`, un **mockup**, no una
captura de algo que exista: los textos de la imagen son el contenido de referencia, no cadenas para
copiar al código (van por catálogo).

El comportamiento y el reparto en pasos están en `plan/configuracion.md` §2, §5 y §8. Acá está solo lo
que hace falta para dibujar y para no romper nada.

---

## 1. Los cuatro principios

1. **Un paso por diálogo.** Sin formularios gigantes ni una ventana que pida todo junto: cada paso es un
   diálogo de `kdialog`, con su texto y sus botones. Es lo que el producto ya hace con las decisiones.
2. **Nada se escribe sin confirmar.** El último paso muestra el archivo tal como quedará y pide
   confirmación explícita.
3. **Cancelar no deja rastro.** `kdialog` devuelve **2** al cancelar y **1** al «no». En cualquier paso,
   esos dos códigos terminan el asistente sin tocar el archivo (el hash antes y después es igual).
4. **La contraseña nunca se ve.** Ni en el resumen, ni en el log, ni en `ps`: se pide con
   `kdialog --password` o por entrada estándar, nunca como argumento.

## 2. Las cinco pantallas

La imagen muestra cuatro de las cinco (la 2, credenciales, se omite por ser la más simple).

### Paso 1 — Servidor
- **Pide:** la URL base del servidor ntfy.
- **Muestra:** campo con el valor actual si existe, y una ayuda: «http o https, sin parámetros ni
  fragmento. Se acepta barra final.»
- **Valida al continuar:** forma de la URL y `GET /v1/health`. Si responde, una línea verde:
  «Responde y se identifica como ntfy · 38 ms».
- **Si falla:** el diálogo no se cierra. `kdialog --error` con el motivo («no responde», «responde pero
  no parece ntfy»), y se vuelve al campo. Tres intentos y se ofrece cancelar.
- **Botones:** Cancelar · Continuar.

### Paso 2 — Credenciales
- **Pide:** si el servidor usa usuario y contraseña (`--yesno`). Si dice que sí: usuario (`--inputbox`)
  y contraseña (`--password`).
- **Valida:** `GET /v1/account` con Basic. Tres resultados posibles: válidas (muestra usuario y rol),
  rechazadas (401 → vuelve a pedir), o el servidor no pide credenciales (`role=anonymous` → avisa y
  permite seguir sin ellas).
- **Botones:** Atrás · Cancelar · Continuar.

### Paso 3 — Topics
- **Muestra:** la lista actual como `--checklist`, con los configurados marcados.
- **Permite:** marcar/desmarcar y agregar uno nuevo desde un campo.
- **Valida en el momento:** 1 a 64 caracteres de `[A-Za-z0-9_-]`; sin duplicados; **no se puede quedar
  sin ninguno** (quitar el último es error, no una lista vacía).
- **Botones:** Atrás · Cancelar · Continuar.

### Paso 4 — Verificación
- **No pide nada:** muestra el resultado de los cinco controles con su estado.
- **Estados:** `ok` en verde, `no verificado` en ámbar, `falla` en rojo.
- **Regla:** un `no verificado` (servidor sin `/v1/account`, un 404 en esa ruta, red ausente) **no
  bloquea**; una `falla` (401, 403, servidor que no es ntfy) sí bloquea el paso.
- **Botones:** Cancelar · Continuar (este último deshabilitado si hay una falla).

### Paso 5 — Resumen y escritura
- **Muestra:** el archivo final en monoespaciado, con la contraseña como `••••••••` y el comentario de
  cada línea conservado. Debajo, la ruta, `0600` y el `.bak`.
- **Advierte:** si algún control quedó «no verificado», una línea ámbar arriba del resumen.
- **Botones:** Atrás · Cancelar · **Escribir**.

## 3. Qué se ve cuando algo sale mal

| Situación | Qué muestra el asistente | ¿Deja seguir? |
|---|---|---|
| URL con query o fragmento | error en el paso 1, con la parte que sobra | no |
| Servidor que no responde | «no responde en 3 s» y el paso marcado como no verificado | sí, con advertencia |
| Servidor que responde y no es ntfy | «responde, pero no parece un servidor ntfy» + el cuerpo recibido | no |
| 401 en `/v1/account` | «credenciales rechazadas», vuelve a pedirlas | no hasta que sean válidas o se elija anónimo |
| 403 al abrir el topic | «sin permiso de lectura en `x`», con el topic nombrado | no |
| Archivo previo inválido | no se puede editar: se ofrece ver el error y crear uno nuevo | — |

## 4. Cómo se implementa sin abrir diálogos en los tests

- `internal/setup` declara la interfaz que consume, por ejemplo `Prompter` con una operación por paso
  (`AskServer`, `AskCredentials`, `AskTopics`, `ShowProgress`, `Confirm`).
- Los adaptadores (`kdialog`, `zenity`, `text`) la implementan en `internal/dialog` o junto a ella, como
  ya hace `Decider`.
- Los tests usan un `Prompter` de prueba con respuestas fijas: **ningún test abre `kdialog`**.
- Los códigos de salida se traducen en el adaptador, no en el motor: el motor recibe «confirmado»,
  «cancelado» o «error», nunca un `2`.

## 5. Trampas conocidas de las herramientas

- **`kdialog`:** 0 = sí, 1 = no, 2 = cancelar. `--checklist` con `--separate-output` para leer una lista
  sin separadores raros.
- **`zenity`:** devuelve **1 tanto en `--extra-button` como en cancelar** (compatibilidad con 3.x). Hay
  que pasarle `ZENITY_EXTRA` para distinguirlos; si no, un cancelar se confunde con «usar el valor
  sugerido».
- **Sin `kdialog` ni `zenity`:** el frente gráfico no falla; cae al modo texto y lo dice en una línea.
- **Nunca pasar la contraseña por argv.** Ni `--password=…`, ni un `echo` en un `sh -c`.

## 6. Lo que no se hace

- No es un KCM de KDE ni usa Qt/GTK: un panel del sistema exigiría Qt/C++ y choca con
  `plan/alcance.md` §10.
- No hay formulario único con todos los campos.
- No se guarda nada sin la confirmación del paso 5.
- No se muestra la contraseña, ni siquiera enmascarada con su longitud real.
- No se administra el servidor (usuarios, ACL): eso es `ntfy user` / `ntfy access` allá.

## 7. Criterios de aceptación de la interfaz

- El flujo completo en Plasma deja un `config.toml` válido, con `0600` y `.bak`.
- Cancelar en los cinco pasos deja el archivo con el mismo hash (se prueba con el código 2 simulado y,
  manualmente, una vez con `kdialog` real).
- El paso 5 nunca muestra la contraseña.
- Con el servidor caído, el resumen advierte y «Escribir» sigue disponible.
- Sin `kdialog` ni `zenity`, `--gui` arranca el modo texto.
- Todas las cadenas salen del catálogo: `locales/ntfy-desktop.pot` y `locales/es/…` actualizados.
