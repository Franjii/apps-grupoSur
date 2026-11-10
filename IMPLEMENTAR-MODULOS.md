# Cómo integrar futuros módulos en Grupo Sur

Guía basada en el código de `apps-grupoSur` y en la integración de Compliance y Pizarra.
Última revisión: 10 de noviembre de 2026.

## 1. Qué resuelve el portal

El portal centraliza el inicio de sesión con Google, los usuarios, las sesiones,
los permisos por módulo y las tarjetas de acceso. Una aplicación externa puede
tener su propio proceso, base de datos, servicio y subdominio.

Agregar un módulo todavía requiere cambios de código y recompilar el portal.
Administración permite asignar módulos existentes a usuarios; no permite crear
un módulo nuevo indicando solamente su nombre y URL.

Hay dos registros que deben mantenerse sincronizados:

| Registro | Función | Ubicación |
|---|---|---|
| Catálogo de presentación | Nombre, descripción, URL, tarjeta y secciones | `internal/catalog/catalog.go` |
| Catálogo de permisos | Módulos asignables y relación con los usuarios | Tablas `modules` y `user_modules`; inicialización en `internal/db/db.go` |

Registrar solamente la tarjeta no habilita permisos. Insertar solamente una fila
en `modules` tampoco crea una tarjeta.

## 2. Definir el módulo antes de programar

Documentar estos datos, por ejemplo para un módulo ficticio llamado Agenda:

| Dato | Ejemplo |
|---|---|
| Clave estable | `AppEjemplo` |
| Nombre visible | AppEjemplo |
| URL de producción | `https://appejemplo.gruposur.ar` |
| Ruta inicial | `/` |
| Variable del portal | `AGENDA_BASE_URL` |
| Servicio | `appejemplo.service` |
| Puerto interno | Uno verificado como libre en el VPS |
| Base de datos | Propia del módulo, separada de la del login |
| Permisos internos | Definir si hay proyectos, propietarios, lectores o editores |

Elegir entre una sección dentro del portal y una aplicación externa. Una sección
interna usa las rutas y middlewares del portal. Una aplicación externa necesita
integrar el SSO en su backend y desplegarse por separado.

No cambiar la clave del módulo después de asignarla a usuarios sin una migración:
la clave vincula catálogo, permisos y comprobaciones del backend.

## 3. Cambios necesarios en apps-grupoSur

### Configuración

En [internal/config/config.go](internal/config/config.go):

- Agregar el campo de URL del módulo a `Config`.
- Leer su variable de entorno, con un valor local adecuado para pruebas.
- Configurar explícitamente la URL HTTPS real al publicar. Un valor predeterminado
  `localhost` puede hacer que la tarjeta publicada abra la PC del usuario.

En [cmd/server/main.go](cmd/server/main.go), pasar la configuración al catálogo.
Actualizar la firma de `catalog.Build` y todos sus usos y pruebas de forma coherente.
Actualmente Build recibe las URLs de Estadísticas, Inventario, Compliance y una
URL opcional de Pizarra; no acepta automáticamente aplicaciones adicionales.

### Tarjeta y catálogo

En [internal/catalog/catalog.go](internal/catalog/catalog.go), agregar una entrada
con `Key`, `Name`, `Desc`, `BaseURL` y `Landing`. `Sections` es opcional.

Ejemplo conceptual dentro de `Build`, después de incorporar su parámetro de URL:

```go
{Key: "agenda", Name: "Agenda", Desc: "Organización de actividades.",
 BaseURL: agendaBaseURL, Landing: "/"},
```

La plantilla [internal/web/templates/hub.html](internal/web/templates/hub.html)
recorre el catálogo permitido al usuario. No requiere una tarjeta HTML escrita a
mano por cada módulo nuevo.

### Registro de permisos

En `Seed`, en [internal/db/db.go](internal/db/db.go), incorporar el módulo al INSERT
existente de `modules`, conservando los anteriores y su comportamiento idempotente:

```sql
INSERT INTO modules(key, name) VALUES ('agenda', 'Agenda')
ON CONFLICT(key) DO NOTHING;
```

Este ejemplo explica la operación; integrarla en el código de inicialización, no
usarla como una instrucción aislada para modificar producción manualmente.
`DO NOTHING` no actualiza nombres de registros existentes: un cambio de nombre
requiere una actualización explícita.

La administración obtiene las opciones desde la tabla `modules`. Los usuarios
comunes necesitan asignación explícita. `HasModule` permite acceso a módulos a
los administradores; no confundir esa regla con permisos sobre todos los datos
de cada aplicación.

### Rutas internas, si corresponde

Para una sección que vive dentro del portal, registrar sus rutas en
[internal/handlers/handlers.go](internal/handlers/handlers.go) y protegerlas con
`RequireModule("agenda", handler)`. Proteger tanto las pantallas como las
operaciones que consultan o modifican datos.

## 4. Integración SSO de una aplicación externa

El navegador recibe la cookie `gs_session` del login. En producción, el SSO entre
subdominios usa `COOKIE_DOMAIN=.gruposur.ar`, `SECURE_COOKIE=true` y ruta `/`.
La cookie es HttpOnly: el JavaScript de la aplicación no debe intentar leerla.

El backend de la aplicación debe:

1. Leer `gs_session` de la petición del navegador.
2. Consultar por HTTPS `https://login.gruposur.ar/auth/introspect`.
3. Enviar el secreto compartido en `X-SSO-Secret` y la sesión en
   `X-Session-Token`, exclusivamente entre servidores.
4. Validar la respuesta y exigir su propia clave de módulo en `modules`, o la
   condición `is_admin` conforme a la política del portal.
5. Aplicar además sus permisos internos sobre cada recurso solicitado.

Respuesta de referencia del portal:

```json
{
  "email": "usuario@gruposur.ar",
  "name": "Nombre del usuario",
  "is_admin": false,
  "modules": ["agenda"]
}
```

El endpoint existente también admite un token por query, por compatibilidad.
Las nuevas integraciones deben usar el encabezado para evitar tokens en URLs,
logs y registros de proxies. No enviar secretos al frontend ni incluirlos en
respuestas, repositorios o paquetes de publicación.

Distinguir credenciales:

| Variable | Uso |
|---|---|
| `SESSION_KEY` | Configuración del login; no sustituye al secreto SSO |
| `SSO_SHARED_SECRET` | Autenticar la comunicación entre las aplicaciones y el portal |
| Credenciales OAuth de Google | Autorización con Google según el flujo correspondiente |
| Clave de cifrado del módulo | Proteger sus propios tokens guardados, si los utiliza |

La nueva aplicación debe usar el secreto SSO efectivo del portal, que puede estar
en un archivo de entorno o en systemd. No generar un reemplazo para el login:
las aplicaciones actuales dependen de ese valor. La rotación requiere coordinación.

### Fallos y revocación

- Sin sesión o con sesión vencida: pedir autenticación; en API usar un estado
  explícito, no devolver HTML de login como si fuese una respuesta JSON válida.
- Sin permiso de módulo o recurso: denegar con 403.
- Portal inaccesible: denegar temporalmente; no habilitar un usuario de desarrollo.
- Usar tiempos de espera acotados y no seguir redirecciones arbitrarias al
  consultar el portal.
- Volver a validar el acceso en las operaciones protegidas. Si se incorpora caché,
  definir cuánto tarda en aplicarse una revocación; no conservar permisos indefinidamente.
- No cachear sesiones ni respuestas privadas en proxies o service workers.
- Proteger las escrituras frente a CSRF; compartir dominio de cookie no reemplaza
  la autorización. Publicar bajo `gruposur.ar` solamente aplicaciones de confianza.

## 5. Usuarios del módulo y permisos propios

El acceso a un módulo no implica pertenecer a todos sus proyectos. Pizarra, por
ejemplo, verifica pertenencia al proyecto además de la habilitación del módulo.
Un administrador del portal no recibe automáticamente todos los proyectos de Pizarra.

Si una aplicación necesita seleccionar usuarios, implementar un directorio mínimo
para ese módulo. Usar como referencia
[internal/handlers/pizarra_users.go](internal/handlers/pizarra_users.go) y sus pruebas.

Ese endpoint exige secreto del servidor, sesión válida y permiso de Pizarra, y
expone solamente nombre y correo de cuentas activas habilitadas. No reutilizarlo
para otro módulo sin adaptar la comprobación y el filtro. Registrar la nueva ruta
en el portal y validar nuevamente el destinatario/integrante al guardar.

La pertenencia a un grupo de correo no otorga acceso a la aplicación a sus miembros.
Cada persona que inicia sesión necesita su propio usuario y permisos.

## 6. Prueba local antes de publicar

Usar bases, puertos y secretos locales independientes. No copiar sobre producción
una base de pruebas ni activar `DEV_AUTH_BYPASS` en un servicio público.

El login y las aplicaciones deben usar el mismo hostname local, por ejemplo
`localhost`, para compartir la cookie. En local: cookie sin dominio y sin Secure
si se usa HTTP. Los puertos de las aplicaciones deben ser distintos.

El bypass local del portal no reemplaza una prueba real de SSO con Google. Registrar
el callback local exacto en el cliente OAuth cuando se pruebe ese recorrido.

Comprobaciones mínimas:

- Usuario habilitado ve la tarjeta y accede.
- Usuario sin módulo no ve la tarjeta ni puede acceder pegando la URL.
- Usuario desactivado, sesión vencida y cierre de sesión deniegan acceso.
- Quitar permisos afecta a nuevas operaciones, incluso con una pestaña abierta.
- Un usuario no accede a datos de otro proyecto cambiando IDs en la petición.
- Una caída del login no concede acceso alternativo.
- Las otras tarjetas y aplicaciones siguen funcionando.
- Escrituras, fechas, historial y migraciones conservan los datos esperados.
- Si hay correo: vista previa, destinatario validado y protección contra duplicados.

Desde la raíz del portal, ejecutar los tests y el análisis:

```bash
go test ./...
go vet ./...
```

Revisar resultados y pruebas omitidas: por ejemplo, la prueba del importador
puede omitirse si no existe su ZIP de referencia. Ejecutar también las pruebas
propias del nuevo módulo. Un build exitoso no demuestra compatibilidad con la
base ni con la configuración publicada.

## 7. Publicación en el VPS

Datos observados el 10/11/2026; volver a verificarlos antes de cada despliegue:

| Componente | Ubicación/configuración observada |
|---|---|
| Sistema | Debian 11, x86_64: paquete Linux amd64 |
| Proxy | Nginx; no usar las plantillas Caddy sin adaptar al servidor real |
| Servicio del portal | `grupo-sur.service` |
| Binario del portal | `/opt/grupo-sur/server` |
| Directorio del portal | `/opt/grupo-sur` |
| Entorno del portal | `/opt/grupo-sur/.env`, además de posibles variables de systemd |
| Base del portal | `/opt/grupo-sur/data/gruposur.db` |
| Puerto del portal | 8181 |
| Pizarra | Servicio `pizarra`, escucha en `127.0.0.1:8083` |
| Puerto 8082 | Ocupado por Mimir; no reutilizar |

### Preparación

1. Identificar versiones, arquitectura, puertos libres, rutas, permisos y
   configuración efectiva. No asumir que la copia local del portal coincide con
   producción: una actualización del portal también afecta Estadísticas.
2. Definir base vacía o migración. Cada módulo conserva su base separada.
3. Preparar el binario, checksum y referencia del código fuente. Excluir `.env`,
   claves, bases, copias de seguridad y datos reales del paquete.
4. Configurar la URL de producción del módulo en el portal.
5. Crear un registro DNS específico hacia el VPS correcto. Un comodín puede
   apuntar a otro servidor. No cambiar los registros del login para agregar un módulo.
6. Preparar un sitio Nginx independiente, HTTPS, proceso en loopback y servicio
   con usuario propio sin privilegios. Validar con `nginx -t` antes de recargar.
7. Verificar renovación de certificado y recarga posterior de Nginx. La existencia
   de `certbot.timer` no demuestra por sí sola que el certificado renovado se cargue.

Las plantillas y estáticos del portal se embeben en el binario mediante
`internal/web/render.go`: sus cambios requieren recompilar. No se corrigen en
producción editando un HTML al lado del ejecutable.

Ejemplo de build del portal para el VPS observado, desde su repositorio:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=readonly \
  -buildvcs=false -trimpath -o bin/server-nuevo ./cmd/server
sha256sum bin/server-nuevo
```

El portal actual no implementa `-version`: ejecutar `server -version` no es una
prueba inocua, porque puede abrir la base y arrancar el servidor. Inspeccionar
el artefacto sin ejecutarlo y arrancarlo solo con configuración controlada.

### Respaldo y activación

- Respaldar binario, configuración (incluidas las unidades y overrides relevantes)
  y base antes del cambio. Mantener secretos fuera del paquete y respaldos privados.
- Para SQLite activa, usar la API de backup o un mecanismo consistente. No copiar
  solamente el `.db` mientras existe actividad WAL.
- Preparar el ejecutable nuevo antes de la pausa. Al detener el servicio, hacer
  un respaldo final consistente del directorio de datos, incluidos WAL/SHM si existen.
- Sustituir el binario conservando base, claves, entorno y permisos. La instalación
  de un módulo no debe reemplazar usuarios o sesiones por los de desarrollo.
- Reiniciar el portal solo cuando cambie su binario o entorno. Sus actualizaciones
  interrumpen brevemente el login, Estadísticas y las validaciones SSO de otras apps.
- Actualizar una aplicación externa después no requiere reiniciar el portal si
  su catálogo, permisos y URL no cambiaron.

Verificar después del arranque: estado estable, logs sin errores, puerto esperado,
respuesta HTTPS, login real, tarjeta, permisos negativos y operaciones persistentes.
`active` inmediatamente después de iniciar no basta: un proceso puede fallar y
entrar en un ciclo de reinicios. Un 401 en `curl` sin cookie tampoco demuestra que
el recorrido autenticado funcione; un 502 requiere revisar backend y proxy.

### Recuperación

Conservar un procedimiento para volver al ejecutable anterior. Si el nuevo código
modificó el esquema o los datos, comprobar compatibilidad antes de volver atrás.
No restaurar automáticamente una base: se pierden cambios posteriores al respaldo.

Los efectos externos, como correos ya enviados, no se deshacen al restaurar SQLite.
Reconciliar registros de envío antes de reintentar operaciones. Probar la recuperación
con datos de prueba y definir respaldos periódicos además del respaldo de despliegue.

## 8. Integraciones opcionales

**Google/Gmail:** el SSO no concede permisos para enviar correo. Cada integración
necesita sus scopes, consentimiento y almacenamiento seguro de tokens. Agregar los
callbacks exactos requeridos por la nueva app sin borrar los del login ni los de
otras aplicaciones. No rotar la clave de cifrado de tokens en cada actualización.

**PWA:** servir manifiesto, iconos y service worker por HTTPS; probar en Android e
iPhone. Instalar una PWA no concede permisos, no implementa automáticamente trabajo
sin conexión y no agrega notificaciones push. No cachear datos privados por defecto.

## 9. Lista de cierre

- [ ] Clave del módulo consistente entre catálogo, base y backend.
- [ ] URL de producción configurada y tarjeta visible según permisos.
- [ ] Sesiones y secretos conservados; no se publicó configuración local.
- [ ] SSO y permisos sobre recursos comprobados en el backend.
- [ ] Pruebas positivas y negativas completas, con evidencia de versión y resultado.
- [ ] DNS, HTTPS, proxy, puerto y servicio verificados.
- [ ] Respaldo consistente y recuperación documentada.
- [ ] Renovación TLS y respaldos periódicos comprobados.
- [ ] Otras aplicaciones y Estadísticas verificadas después de actualizar el portal.
- [ ] Operador, fecha, versión, checksum y cambios de esquema registrados.
