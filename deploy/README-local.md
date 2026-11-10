# Integración local en Windows

Los proyectos permanecen separados: la plataforma en `C:\apps-grupoSur` e Inventario en `C:\inventory-rebuild`.

Iniciar desde PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File C:\apps-grupoSur\deploy\local.ps1
```

Abrir http://localhost:8080/auth/login y seleccionar Inventario en el hub. Inventario usa http://localhost:8083. Usar siempre `localhost` en el navegador para compartir la cookie entre ambos puertos.

El usuario de prueba es `local-test@example.invalid`, administrador. La plataforma usa `DEV_AUTH_BYPASS` para crear la sesión sin Google; Inventario valida esa sesión mediante el endpoint SSO real de la plataforma. Este modo permite probar la integración, pero no valida OAuth de Google ni las cookies entre subdominios HTTPS del VPS. El bypass de la plataforma tampoco reproduce su control de acceso normal tras cerrar sesión.

Ambos servidores escuchan solo en `127.0.0.1`. Las bases nuevas, los logs y los identificadores de procesos quedan en `data/local-integration`. No se cargan archivos `.env` ni se modifican las bases anteriores. El puerto 8082 queda disponible para la instancia de inventario independiente que ya estaba en uso.

Detener los servidores iniciados por este script, conservando los datos:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File C:\apps-grupoSur\deploy\local.ps1 -Action Stop
```

El script recompila ambos proyectos al iniciar. Se puede indicar otra ubicación con `-InventoryRoot` u otro puerto de inventario con `-InventoryPort`. Si un puerto está ocupado, se detiene sin reemplazar procesos existentes.
