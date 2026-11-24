# Integración de Transporte

Aplicación autónoma: `/mnt/c/transporte-webapp` en desarrollo, servicio y base
independientes al desplegar. El portal solo centraliza Google OAuth, sesiones,
permisos y la tarjeta de acceso.

- Clave del módulo: `transporte`.
- Nombre: Transporte.
- Descripción: Gestión de los traslados de los pacientes de los ensayos clínicos hacia y desde las sedes.
- Variable del portal: `TRANSPORTE_BASE_URL`.
- Valor local predeterminado: `http://localhost:4050`.
- Valor de producción que se deberá configurar: `https://transporte.gruposur.ar`.

El catálogo de permisos se agrega al iniciar el portal mediante `Seed`, de forma
idempotente. Los usuarios comunes requieren asignación desde Administración;
los administradores acceden según la política existente. La tarjeta se filtra
por permisos y Transporte vuelve a validarlos en su backend, además de exigir
administrador para su sección Admin. No se crean cuentas de prueba con este cambio.

Transporte valida `gs_session` con `/auth/introspect`, enviando `X-Session-Token`
y `X-SSO-Secret`. Debe usar exactamente el `SSO_SHARED_SECRET` efectivo del portal,
sin rotarlo. No copiar un `.env` local sobre producción.

## Estado y verificaciones pendientes

Cambio preparado el 19/11/2026. La ejecución de las pruebas nuevas del portal
está pendiente. Los tests y `go vet` de Transporte, incluidos los ajustes SSO,
fueron informados como correctos por el operador.

Antes del deploy: probar Google OAuth real, tarjeta, acceso directo, usuario
común, administrador, usuario sin permiso, revocación, cierre de sesión y caída
del portal. Usar bases locales separadas, mismo hostname y puertos distintos;
verificar puertos en WSL y Windows y conservar callbacks OAuth existentes.

Infraestructura acordada: VPS `2.24.67.95`, portal `/opt/grupo-sur`, Nginx y
Certbot. No aplicar las instrucciones antiguas de Caddy ni ngrok; deberán
corregirse antes del despliegue después de inspeccionar la configuración efectiva.
No se ha ejecutado ningún despliegue ni modificado el entorno de producción.
