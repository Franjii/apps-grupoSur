# Plan de implementación — Plataforma Grupo Sur

Stack: **Go + HTMX + SQLite**, binario único (monolito modular) en VPS Debian 11
(Hostinger), detrás de Caddy. Ver requisitos en `instrucciones.md`.

## 0. Decisiones técnicas

- **Router:** `net/http` (Go 1.22+, con patrones de ruta) + middlewares propios.
  Sin framework pesado.
- **SQLite:** `modernc.org/sqlite` (driver **puro Go, sin CGO**) → permite
  compilar un binario estático y cruzado para Debian sin toolchain de C.
- **Excel:** `github.com/xuri/excelize/v2` con **lector en streaming** (`Rows`)
  para no cargar 130k filas en memoria.
- **OAuth:** `golang.org/x/oauth2` + endpoint de Google. Validación de `hd`/email
  contra usuarios habilitados.
- **Plantillas:** `html/template` + **HTMX** (fragmentos parciales para filtros y
  gráfico sin recargar la página).
- **Gráfico:** render del lado servidor. Opción A: SVG generado en template
  (sin JS). Opción B: Chart.js. → Por defecto **SVG server-side** (coherente con
  filosofía HTMX, cero dependencias JS).
- **Sesión:** cookie firmada con token opaco; registro en tabla `sessions`;
  `Domain=.gruposur.ar`, `Secure`, `HttpOnly`, `SameSite=Lax`.

## 1. Estructura del proyecto

```
apps-grupoSur/
├── cmd/server/main.go              # arranque, wiring, http.Server
├── internal/
│   ├── config/        config.go    # carga de env (.env / variables)
│   ├── db/            db.go         # apertura SQLite, pragmas, migraciones embebidas
│   │                  queries.go    # consultas (visitas, usuarios)
│   ├── auth/          oauth.go      # flujo Google OAuth
│   │                  session.go    # crear/validar/expirar sesiones
│   │                  middleware.go # RequireAuth, RequireModule, RequireAdmin
│   ├── importer/      importer.go   # zip → xlsx (streaming) → dedup → insert
│   │                  parse.go      # detección de header, fecha serial→ISO
│   ├── visits/        handlers.go   # dashboard, endpoints HTMX de filtros/gráfico
│   │                  service.go    # lógica de métricas y agregaciones
│   └── web/           render.go     # helpers de templates
├── web/
│   ├── templates/     *.html        # layout, login, dashboard, parciales, admin
│   └── static/        css, htmx.min.js
├── migrations/        00x_*.sql     # esquema (embebido con go:embed)
├── deploy/            Caddyfile, grupo-sur.service, README-deploy.md
├── go.mod
└── instrucciones.md / PLAN.md
```

## 2. Fases

### Fase 1 — Scaffold y base de datos
- `go mod init`, dependencias, config por env
  (`GOOGLE_CLIENT_ID/SECRET`, `BASE_DOMAIN`, `DB_PATH`, `SESSION_KEY`, etc.).
- Migraciones SQLite (embebidas) con las tablas de §6 de `instrucciones.md`:
  `visit_procedures` (+ índices y UNIQUE), `import_batches`, `users`, `modules`,
  `user_modules`, `sessions`.
- Seed: módulo `estadisticas` (con sección Visitas definida en código); admin
  inicial `sebastian.chapartegui@gruposur.ar`.
- Servidor HTTP mínimo con healthcheck.

### Fase 2 — Autenticación y autorización
- Flujo Google OAuth en `login.gruposur.ar` (login → callback → match contra
  `users` activos → crear sesión → cookie en `.gruposur.ar`).
- Middlewares: `RequireAuth`, `RequireModule("estadisticas")`, `RequireAdmin`.
- Pantalla **admin de usuarios**: alta/baja (activar/inhabilitar) y asignación de
  módulos. Solo `is_admin`.
- Logout y expiración de sesión.

### Fase 3 — Importador
- Pantalla protegida de **subida de `.zip`** (sección Visitas, módulo estadisticas).
- Descomprimir en memoria → por cada `.xlsx`: localizar fila `MRN`, leer en
  streaming, convertir `Procedure Date` serial→ISO, **descartar filas sin fecha**
  (planificadas).
- Insert con dedup por clave natural (`INSERT ... ON CONFLICT DO NOTHING`).
- Registrar `import_batches` (totales/insertados/omitidos) y mostrar resumen.

### Fase 4 — Dashboard de Visitas (`estadisticas.gruposur.ar/visitas`)
- Filtros combinables: Estudio, Visita, Procedimiento, Profesional, Rango de
  fecha. Semántica **OR dentro / AND entre**. Cada cambio dispara HTMX que
  re-renderiza hero + gráfico.
- **Hero:** visitas realizadas, tipos de visita, estudios (fórmulas de §5).
- **Gráfico de barras** Estudio+Visita (`Protocolo - Visita`), orden desc,
  altura = `COUNT(DISTINCT MRN, Procedure Date)`.
- Opciones de filtro pobladas dinámicamente desde la BD.

### Fase 5 — Despliegue
- Build estático: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build`.
- **Caddy** como reverse proxy con HTTPS automático para `login.gruposur.ar` y
  `estadisticas.gruposur.ar` → binario local.
- **systemd** unit (`grupo-sur.service`): arranque automático, restart on-failure.
- **DNS** en el proveedor: registros A de ambos subdominios → IP del VPS.
- **Backup** diario del archivo SQLite (cron + copia con timestamp).
- Doc de actualización: build → scp → restart service.

## 3. Orden sugerido de trabajo

1. Fase 1 (scaffold + esquema) — base para todo.
2. Fase 3 (importador) — permite cargar datos reales y validar el modelo pronto.
3. Fase 4 (dashboard) — el valor visible.
4. Fase 2 (auth) — se puede desarrollar en paralelo; necesaria antes de exponer.
5. Fase 5 (despliegue).

> Nota: durante desarrollo se puede usar un bypass de auth local para no depender
> de OAuth hasta la Fase 2.
