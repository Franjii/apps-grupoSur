# Plataforma Grupo Sur — Especificación

> Documento de requisitos. Las secciones marcadas con ⚠️ son decisiones por
> defecto propuestas que conviene confirmar o ajustar.

## 1. Objetivo

Construir una plataforma web modular para Grupo Sur. La primera sección es un
**Dashboard interactivo de Visitas** basado en los datos exportados del sistema
de gestión de estudios clínicos (CTMS).

### Jerarquía: Módulo → Sección
- **Módulo** = unidad de primer nivel, asignable por usuario y mapeada a un
  subdominio. Hoy existe **Estadísticas**; a futuro se agregarán otros (ej.
  Finanzas).
- **Sección** = parte interna de un módulo. **Visitas** es una sección del módulo
  **Estadísticas** (no un módulo en sí).
- Al iniciar sesión el usuario ve un **hub con tarjetas de los módulos
  asignados**; al entrar a un módulo ve **tarjetas de sus secciones**.
  Navegación: `Login → Hub (módulos) → Módulo (secciones) → Sección`.

## 2. Arquitectura de dominios

- `login.gruposur.ar` — Acceso (login) y **hub de módulos** tras autenticar.
- `estadisticas.gruposur.ar` — Módulo **Estadísticas**. `/estadisticas` lista sus
  secciones; `/visitas` es la sección de Visitas (dashboard).
- Futuro: cada módulo nuevo se monta como un subdominio adicional bajo
  `gruposur.ar`, sirviendo desde el mismo binario.

> Nota: el dominio raíz `gruposur.ar` lo usa la Landing Page de la compañía, por
> eso se trabaja con subdominios.
> Importante: los subdominios DNS **no llevan tildes** → se usa
> `estadisticas` (sin tilde).

⚠️ **Enfoque de despliegue:** Monolito modular — un único binario Go sirve el
login y las secciones, distinguiendo por host/ruta. Se puede separar en
servicios independientes más adelante si el crecimiento lo justifica.

## 3. Autenticación y autorización

- **Login con Google (OAuth 2.0).** No se gestionan contraseñas propias.
- **Sesión compartida entre subdominios:** cookie de sesión sobre el dominio
  padre `.gruposur.ar`, de modo que el usuario hace login una vez y accede a
  todas las secciones permitidas (SSO simple propio).
- **Control de acceso por módulo (RBAC):**
  - Rol **Admin**: puede dar de alta / inhabilitar usuarios y **asignar módulos**
    a cada usuario.
  - Un usuario solo ve y entra a los **módulos** que tiene asignados (ej.: quien
    solo tiene "Estadísticas" no puede ver un futuro "Finanzas"). El acceso a las
    secciones se hereda del módulo.
- El primer usuario admin (semilla inicial) es
  **sebastian.chapartegui@gruposur.ar**.
- Consideración: los datos provienen de estudios clínicos → tratar el acceso y
  almacenamiento con confidencialidad.

## 4. Ingesta de datos

- **Subida manual recurrente por web:** pantalla protegida (solo usuarios con
  permiso) donde se sube el `.zip` exportado del CTMS.
- **El `.zip` puede contener varios archivos Excel:** cuando la exportación es
  grande, el CTMS la parte en múltiples archivos dentro del mismo `.zip`. El
  importador debe **recorrer y procesar todos los Excel** del `.zip` como un
  único lote.
- El export contiene **todas las visitas realizadas** (histórico de ~3 años a la
  fecha) y **crece con el tiempo**. Cada importación trae el dataset completo o
  ampliado.
- El sistema debe **evitar duplicados** al re-importar (ver §6: clave única de
  visita/procedimiento), ya que reimportar el histórico completo es el flujo
  normal.
- ⚠️ Se conserva histórico: cada importación agrega o actualiza registros, no
  borra los previos.
- Pendiente: analizar `Reporte de Visitas.zip` para validar nombres de columnas,
  tipos de dato y calidad antes de fijar el esquema.

## 5. Dashboard de Visitas

### Filtros (combinables)
- **Estudio** — `Study Protocol Assignment`
- **Visita** — `Visit Name`
- **Procedimiento** — `Procedure`
- **Profesional** — `Coordinator`
- **Rango de fecha** — `Procedure Date`

⚠️ **Semántica de filtros:** OR dentro de un mismo filtro (varias selecciones del
mismo tipo), AND entre filtros distintos. Los filtros son aditivos: a un filtro
se le puede sumar otro por encima.

> **Definición de "visita realizada":** una ocurrencia única
> `(MRN + Study Protocol Assignment + Visit Name + Procedure Date)` que tiene al
> menos un procedimiento realizado (es decir, con `Procedure Date`). Las filas
> planificadas/no realizadas (sin fecha) **se descartan** y no aparecen en el
> dashboard.

### Componente Hero (respeta los filtros activos)
- Cantidad de **Visitas realizadas** →
  `COUNT(DISTINCT MRN, Study, Visit Name, Procedure Date)` sobre las filas
  filtradas.
- Cantidad de **tipos de visita distintos** → `COUNT(DISTINCT Visit Name)`.
- Cantidad de **estudios distintos** → `COUNT(DISTINCT Study Protocol Assignment)`.

### Gráfico
- Gráfico de **barras de visitas realizadas**, ordenado de mayor a menor cantidad.
- **Dimensión:** combinación de **Estudio (`Study Protocol Assignment`) + Visita
  (`Visit Name`)**. Cada barra representa un par estudio-visita.
  Ejemplo de etiqueta: `Protocolo 1 - Visita 1`.
- **Altura de la barra:** cantidad de visitas realizadas distintas
  (`COUNT(DISTINCT MRN, Procedure Date)`) de ese par estudio-visita.

## 6. Modelo de datos (definitivo, basado en análisis del ZIP)

### Estructura real del export del CTMS (RealTime-CTMS)
- El `.zip` contiene **uno o varios** archivos `.xlsx` (la exportación se parte
  cuando es grande).
- **Cada** `.xlsx` tiene su propia fila de encabezado
  `MRN | Study Protocol Assignment | Visit Name | Procedure | Coordinator | Procedure Date`.
  El **primer** archivo además incluye ~9 filas de metadata arriba (título,
  fecha de reporte, criterios). → El importador debe **localizar la fila de
  encabezado dinámicamente** (la fila cuya primera celda es `MRN`), no por
  posición fija.
- **Grano:** una fila = **un procedimiento** dentro de una visita de un paciente.
- `Procedure Date` viene como **número serial de Excel** (base 1899-12-30) →
  convertir a fecha ISO (`YYYY-MM-DD`).
- Las filas **realizadas** tienen `Procedure Date` (y casi siempre `Coordinator`);
  las **planificadas** vienen sin fecha → se descartan al importar.
- `Coordinator` puede faltar en algunas filas realizadas → permitir NULL.

### Tablas (SQLite)

**Dominio Visitas** — se importan solo procedimientos realizados (con fecha):
- `visit_procedures` (id, mrn, study, visit_name, procedure, coordinator NULL,
  procedure_date, import_batch_id).
  - **Clave natural única (dedup):**
    `(mrn, study, visit_name, procedure, procedure_date)` — verificado: 0
    duplicados en los datos actuales. Reimportar el histórico completo no genera
    duplicados.
  - Índices en `study`, `visit_name`, `procedure`, `coordinator`,
    `procedure_date` (columnas de filtro).
- `import_batches` (id, filename, imported_at, imported_by, rows_total,
  rows_inserted, rows_skipped).

**Dominio Auth/Módulos:**
- `users` (id, email único, name, is_admin, active, created_at).
- `modules` (key, name) — catálogo de **módulos** para RBAC (`estadisticas`,
  futuro `finanzas`, ...). Las **secciones** de cada módulo (ej. Visitas) se
  definen en código (`internal/catalog`), no en la base.
- `user_modules` (user_id, module_key) — módulos asignados a cada usuario.
- `sessions` (id/token, user_id, created_at, expires_at).

### Perfil de los datos actuales (referencia)
- ~130.624 filas en el export; **23.254 procedimientos realizados** →
  **4.787 visitas realizadas** distintas.
- 13 estudios, 448 tipos de visita, 236 procedimientos, 45 coordinadores,
  1.762 pacientes. Rango: **2023-06-07 → 2026-06-26**.

## 7. Stack y despliegue

- **Lenguaje/UI:** Go (Golang) + HTMX.
- **Base de datos:** SQLite.
- **Servidor:** VPS Debian 11 en Hostinger, ejecutando el **binario** compilado.
- ⚠️ **Reverse proxy:** Caddy (HTTPS/Let's Encrypt automático para los
  subdominios).
- ⚠️ El binario corre como **servicio systemd** (arranque automático, reinicio).
- ⚠️ **Backups:** copia diaria del archivo SQLite.

## 8. Requisitos no funcionales

- **Volumen variable y creciente:** el dataset es el histórico completo de
  visitas (~3 años a la fecha) y crece de forma continua. El diseño de ingesta y
  consultas debe asumir un dataset que se amplía con cada importación. SQLite es
  adecuado para este volumen con índices apropiados en las columnas de filtro.
- HTTPS obligatorio en todos los subdominios.
- Estrategia de actualización del binario documentada (build → copiar → reiniciar
  servicio).

## 9. Pendientes antes de codificar

1. ✅ Esquema definido a partir del análisis del ZIP (ver §6).
2. Confirmar los puntos por defecto restantes marcados con ⚠️ (despliegue
   monolito modular, proxy Caddy, backups, semántica OR-dentro/AND-entre de los
   filtros).
3. Edge case menor a confirmar: ~6.720 filas tienen coordinador pero **sin**
   fecha. Por defecto se tratan como **no realizadas** (se descartan, porque
   "realizada" se define por tener `Procedure Date`). Confirmar si alguna debería
   considerarse realizada.
