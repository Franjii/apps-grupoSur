# Plataforma Grupo Sur

Plataforma web modular para Grupo Sur Investigación Clínica. Primera sección:
**Dashboard de Visitas** a partir de los exports del CTMS (RealTime-CTMS).

- **Stack:** Go + HTMX + SQLite (binario único, sin CGO).
- **Arquitectura:** monolito modular con jerarquía **Módulo → Sección**.
  `login.gruposur.ar` (acceso + hub de módulos) y `estadisticas.gruposur.ar`
  (módulo **Estadísticas**, con la sección **Visitas** en `/visitas`). Navegación:
  `Login → Hub (módulos) → Módulo (secciones) → Sección`. Diseñado para sumar
  módulos futuros (ej. Finanzas) agregando una entrada en `internal/catalog`.
- Requisitos y modelo de datos: [`instrucciones.md`](instrucciones.md).
- Plan de implementación: [`PLAN.md`](PLAN.md).
- Despliegue: [`deploy/README-deploy.md`](deploy/README-deploy.md).

## Desarrollo local

```bash
make run        # http://localhost:8080  (DEV_AUTH_BYPASS: sesión como admin)
make test       # tests (incluye import del zip real si está presente)
```

Con el server corriendo:
- `/` — hub con tarjetas de los módulos asignados.
- `/estadisticas` — módulo Estadísticas (tarjetas de secciones).
- `/visitas` — sección Visitas: dashboard (hero + gráfico + filtros aditivos).
- `/visitas/importar` — subir el `.zip` del CTMS.
- `/admin/usuarios` — alta/baja de usuarios y asignación de módulos.

## Actualizar la build en producción

La app corre en el VPS como servicio systemd (`grupo-sur`, binario en
`/opt/grupo-sur/server`), expuesta por un túnel **ngrok** al puerto `8080`.
Actualizar = recompilar el binario, subirlo y reiniciar el servicio. No hace
falta reiniciar el VPS ni ngrok (el túnel se reconecta solo al puerto 8080).

```bash
# 1. Compilar el binario estático para el VPS (Debian, amd64)
make build-linux            # genera bin/server-linux-amd64

# 2. Subirlo al servidor
scp bin/server-linux-amd64 root@2.24.67.95:/tmp/server

# 3. Reemplazar el binario y reiniciar el servicio
ssh root@2.24.67.95 'mv /tmp/server /opt/grupo-sur/server && \
  chown grupo-sur:grupo-sur /opt/grupo-sur/server && \
  systemctl restart grupo-sur'

# 4. Verificar que levantó bien
ssh root@2.24.67.95 'systemctl status grupo-sur --no-pager && curl -s http://127.0.0.1:8080/healthz'
# -> "active (running)" y "ok"
```

> Conviene correr `make test` antes del paso 1. Para no ingresar la contraseña
> de root en cada deploy: `ssh-copy-id root@2.24.67.95` (copia tu clave SSH).
> Detalles de la instalación inicial: [`deploy/README-deploy.md`](deploy/README-deploy.md).

## Estructura

```
cmd/server         arranque
internal/config    configuración por env
internal/db        SQLite, esquema embebido, seed
internal/catalog   registro de módulos y secciones (hub/navegación)
internal/auth      usuarios, sesiones, Google OAuth, RBAC por módulo
internal/importer  zip → xlsx (streaming) → dedup → insert
internal/visits    consultas del dashboard + gráfico SVG
internal/web       plantillas HTML + assets (HTMX)
internal/handlers  rutas HTTP
deploy             Caddyfile, systemd, backup, guía de despliegue
```

## Modelo de datos (resumen)

- `visit_procedures`: un procedimiento **realizado** por fila. Clave natural de
  dedup `(mrn, study, visit_name, procedure, procedure_date)`.
- Visita realizada = `DISTINCT(mrn, study, visit_name, procedure_date)`.
- Las filas planificadas (sin fecha) se descartan al importar.
- `users` / `modules` / `user_modules` / `sessions`: auth y RBAC.
