# ntfy-desktop — alcance

Cliente de escritorio para recibir avisos y pedidos de decisión por ntfy, y responderlos
desde el escritorio de Plasma sin abrir un navegador.

Estado: **borrador para revisión**. No hay código todavía.

---

## 1. Problema

Los pedidos de decisión (aprobar un restore, confirmar una acción, autorizar un gasto) llegan
por chat o por correo, y se pierden entre el resto. No hay forma de resolverlos desde el
escritorio, ni de saber que quedaron pendientes.

## 2. Qué hace

1. Se suscribe a un topic de ntfy y escucha el stream de mensajes.
2. Cada mensaje genera un **aviso** en el escritorio.
3. Si el mensaje trae acciones, además abre un **diálogo** con las opciones.
4. La opción elegida se devuelve al emisor.
5. Lo que no se contestó queda **pendiente y recuperable**.

## 3. Qué no hace

- No reemplaza a la app de ntfy del teléfono.
- No publica mensajes: sólo escucha y responde.
- No administra usuarios ni topics del servidor.
- No cifra ni descifra contenido.
- No funciona sin sesión gráfica.

## 4. Requisitos

| # | Requisito | Origen |
|---|---|---|
| R1 | Recibir mensajes sin abrir navegador | requisito del usuario |
| R2 | Aviso visible en el escritorio | requisito del usuario |
| R3 | Poder **decidir** desde el aviso | requisito del usuario |
| R4 | La petición no se pierde si estoy trabajando | requisito del usuario |
| R5 | Sólo una ventana de decisión a la vez | prueba: dos diálogos se apilan y la primera queda tapada |
| R6 | Reconectar y recuperar lo que llegó mientras estaba apagado | diseño |
| R7 | Las credenciales no viven en el código: se leen de configuración o del entorno | requisito de producto |

Todo lo que identifica a una instalación concreta —servidor, topic, credenciales, rutas—
entra por **configuración**, nunca en el código ni en la documentación del producto.

## 5. Hechos verificados que condicionan el diseño

Todo lo siguiente se probó en el escritorio de referencia (Kubuntu, Plasma 6.6.7, Wayland):

- **La notificación del sistema no dibuja botones.** Las acciones se envían y el servidor
  anuncia la capacidad `actions`, pero no se renderizan. Por eso la decisión no va por ahí.
- **`expire_timeout = 0` cierra la notificación al instante.** El popup usa el valor como
  intervalo de su temporizador. Las notificaciones llevan un timeout explícito, nunca 0.
- **`kdialog` sí dibuja botones nativos y no expira.** Quedó abierto 40 s hasta que lo mató
  un timeout externo. Es la superficie de decisión.
- **Dos `kdialog` a la vez se apilan**: la segunda tapa a la primera y la primera se pierde.
  De ahí R5.
- **Con un panel del sistema abierto, Plasma retiene los avisos.** No es un fallo a corregir,
  pero explica avisos que "no aparecen".

## 6. Decisiones tomadas

| Decisión | Motivo |
|---|---|
| Transporte: **ntfy**, con la URL del servidor por configuración | protocolo abierto; funciona contra cualquier instancia, propia o pública |
| Topic y credenciales: **por configuración** (archivo o entorno) | el producto no conoce ninguna instalación concreta |
| Aviso: **notificación del sistema** con timeout explícito | no roba el foco |
| Decisión: **`kdialog --yesnocancel`** | único mecanismo que dibuja botones y persiste |
| **Cola de a una petición** | R5 |
| Lenguaje: **Python, sólo biblioteca estándar** | sin dependencias que instalar |
| Repositorio: **`ntfy-desktop`**, producto aislado | se sostiene solo; no depende de nada de la casa |

## 7. Preguntas abiertas

1. **Cómo vuelve la respuesta.** Dos caminos posibles:
   - publicar en un topic de respuesta, o
   - llamar una URL que venga en la acción (`http`).
   El primero no exige exponer nada; el segundo es el estándar de ntfy.
2. **Qué pasa cuando el emisor espera y nadie contesta.** ¿Vence? ¿Se reintenta?
3. **Arranque**: ¿con la sesión gráfica, o como servicio de usuario siempre activo?
4. **Instalación**: script propio, paquete, o copia manual.
5. **Qué se ve cuando hay 3 pendientes**: ¿se abren en serie, o un solo diálogo que lista?

## 8. Hitos

Sin fechas. Cada hito se cierra cuando se puede probar.

1. **Lectura**: conecta al topic, imprime los mensajes que llegan. Sin interfaz.
2. **Aviso**: notificación por cada mensaje, con timeout correcto.
3. **Decisión**: diálogo con opciones, respuesta publicada.
4. **Cola**: una petición a la vez, con contador.
5. **Resiliencia**: reconexión, catch-up por `since`, arranque con la sesión.
6. **Instalación**: unit de systemd de usuario y `.desktop`.

## 9. Criterios de aceptación

- Un mensaje publicado en el topic aparece como aviso en menos de 5 segundos.
- Un mensaje con acciones abre un diálogo con las opciones exactas que trae.
- Elegir una opción publica la respuesta correcta y cierra el diálogo.
- Con dos peticiones seguidas, nunca hay dos diálogos abiertos.
- Apagar y encender la laptop no pierde las peticiones pendientes.
- El token nunca aparece en el código ni en los logs.

## 10. Riesgos

- **Dependencia de la sesión gráfica**: sin sesión no hay diálogo. Los pendientes se acumulan.
- **El servidor ntfy es un punto único**: si el servidor cae, no hay avisos.
- **Los tokens dan acceso al topic completo**: un token filtrado permite publicar y leer.
- **Plasma puede cambiar** su comportamiento en notificaciones entre versiones.

---

## Nota: uso interno

En nuestra instalación apuntamos a nuestro propio servidor y tomamos las credenciales de
nuestro gestor de secretos. Eso **es uso, no diseño**: no aparece en el código, ni en la
configuración de ejemplo, ni en las pruebas del producto.
