# ntfy-desktop

Cliente de ntfy para escritorio Linux.

Estado: **esqueleto**. Todavía no habla con el servidor: detecta el entorno —usuario de sesión,
idioma y escritorio— y sirve de base para los hitos del plan.

## Compilar

El toolchain de Go vive dentro del repositorio, así que no hace falta tener Go instalado:

```sh
make bootstrap    # baja y verifica go1.27.4 en .toolchain/
make check        # fmt + vet + test + build
./bin/ntfy-desktop
```

## Uso

```sh
./bin/ntfy-desktop            # muestra el entorno detectado
./bin/ntfy-desktop -version   # sólo la versión
```

## Plan y alcance

Ver [`plan/alcance.md`](plan/alcance.md). Ahí están el alcance, los hitos, los criterios de
aceptación y las decisiones de diseño con su motivo.

## Licencia

GPL-3.0-or-later. Ver [`LICENSE`](LICENSE) y [`NOTICE`](NOTICE).
