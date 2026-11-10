# Desplegar el módulo Inventario en el VPS

`inventory-webapp` es una app Go aparte que corre como **binario + servicio
systemd**, igual que la plataforma, en su propio subdominio
`inventario.gruposur.ar` detrás de Caddy. El login es **SSO**: confía en la
sesión de `login.gruposur.ar` (cookie `gs_session` sobre `.gruposur.ar`), así que
**debe servirse por HTTPS**.

> Diferencia clave con la plataforma: este binario **no es 100% autónomo**. Lee
> `./static` (assets/PWA) y `./inventory.db` **relativos al WorkingDirectory**.
> Por eso el servicio fija `WorkingDirectory=/opt/inventario` y hay que copiar la
> carpeta `static/` y la base junto al binario. Las plantillas (templ) sí van
> compiladas dentro del binario.

Puertos: la plataforma usa `:8080`; **Inventario usa `:4040`** (fijo en el
código, no por env).

---

## 0. Requisito previo: actualizar la plataforma

El SSO necesita el endpoint `/auth/introspect` que agrega esta integración.
Redesplegá primero la plataforma con el binario nuevo y sumá dos variables a
`/opt/grupo-sur/.env`:

```env
INVENTARIO_BASE_URL=https://inventario.gruposur.ar
SSO_SHARED_SECRET=<generar: openssl rand -hex 32>
```

```bash
# En tu máquina, dentro de apps-grupoSur:
make build-linux
scp bin/server-linux-amd64 usuario@IP_DEL_VPS:/tmp/server
ssh usuario@IP_DEL_VPS 'sudo mv /tmp/server /opt/grupo-sur/server && \
  sudo chown grupo-sur:grupo-sur /opt/grupo-sur/server && \
  sudo systemctl restart grupo-sur'
```

Guardá el `SSO_SHARED_SECRET`: va **idéntico** en el `.env` de Inventario (paso 4).

---

## 1. DNS

En el panel de `gruposur.ar`, agregar un registro **A**:

| Tipo | Nombre     | Valor      |
|------|------------|------------|
| A    | inventario | IP_DEL_VPS |

---

## 2. Usuario del sistema y carpetas

```bash
sudo useradd --system --home /opt/inventario --shell /usr/sbin/nologin inventario
sudo mkdir -p /opt/inventario
sudo chown -R inventario:inventario /opt/inventario
```

---

## 3. Compilar y subir (binario + static + base)

Compilar en tu máquina, dentro de `inventory-webapp/` (sin CGO → binario
estático, igual que la plataforma):

```bash
cd inventory-webapp
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
  -o bin/server-linux-amd64 ./cmd/server

# Binario + assets estáticos (obligatorio: se leen de ./static en runtime)
scp bin/server-linux-amd64        usuario@IP_DEL_VPS:/tmp/server
scp -r static                     usuario@IP_DEL_VPS:/tmp/static
scp deploy/inventario.service     usuario@IP_DEL_VPS:/tmp/inventario.service
```

Base de datos: subí tu `inventory.db` **existente** (con datos). En WAL conviene
copiar una versión consistente:

```bash
# En tu máquina, generar una copia limpia (requiere sqlite3):
sqlite3 inventory.db ".backup '/tmp/inventory.db'"
scp /tmp/inventory.db usuario@IP_DEL_VPS:/tmp/inventory.db
```

Colocar todo en `/opt/inventario` con el dueño correcto:

```bash
ssh usuario@IP_DEL_VPS '
  sudo mv /tmp/server        /opt/inventario/server && \
  sudo rm -rf /opt/inventario/static && sudo mv /tmp/static /opt/inventario/static && \
  sudo mv /tmp/inventory.db  /opt/inventario/inventory.db && \
  sudo chmod +x /opt/inventario/server && \
  sudo chown -R inventario:inventario /opt/inventario'
```

---

## 4. Configuración (`/opt/inventario/.env`)

```env
# URL pública (HTTPS => cookies Secure y recepción de gs_session)
APP_URL=https://inventario.gruposur.ar

# SSO con la plataforma (login.gruposur.ar)
PLATFORM_INTROSPECT_URL=https://login.gruposur.ar/auth/introspect
PLATFORM_LOGIN_URL=https://login.gruposur.ar/
PLATFORM_LOGOUT_URL=https://login.gruposur.ar/logout
SSO_REQUIRED_MODULE=inventario
SSO_SHARED_SECRET=<EL MISMO valor que pusiste en /opt/grupo-sur/.env>

# Email / alertas de vencimiento (SMTP)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=soporte@gruposur.ar
SMTP_PASSWORD=<app password de Gmail>
SMTP_FROM=soporte@gruposur.ar
```

```bash
sudo chown inventario:inventario /opt/inventario/.env
sudo chmod 600 /opt/inventario/.env
```

> Con `SSO_SHARED_SECRET` + `PLATFORM_INTROSPECT_URL` seteados, el login local
> usuario+contraseña queda **deshabilitado** y todo pasa por el SSO. Si dejás
> `SSO_SHARED_SECRET` vacío, la app vuelve al login local (útil como break-glass).

---

## 5. systemd

```bash
sudo cp /tmp/inventario.service /etc/systemd/system/inventario.service
sudo systemctl daemon-reload
sudo systemctl enable --now inventario
sudo systemctl status inventario --no-pager

# Liveness (no hay /healthz; con SSO, /login responde 303 hacia la plataforma):
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:4040/login   # -> 303
```

---

## 6. Caddy (agregar el subdominio)

Editar `/etc/caddy/Caddyfile` y sumar el bloque de inventario (proxy al `:4040`):

```caddyfile
login.gruposur.ar, estadisticas.gruposur.ar {
	encode gzip
	reverse_proxy 127.0.0.1:8080
}

inventario.gruposur.ar {
	encode gzip
	reverse_proxy 127.0.0.1:4040
}
```

```bash
sudo systemctl reload caddy
```

Caddy emite el certificado solo. Verificar: `https://inventario.gruposur.ar`
debe redirigir al login de la plataforma si no hay sesión.

---

## 7. Dar acceso al módulo

En `https://login.gruposur.ar` → `/admin/usuarios`, tildar **Inventario** a cada
usuario que deba entrar. Los admins de la plataforma lo tienen automáticamente y
se mapean al rol `admin` de inventario; el resto entra como `user`.

---

## 8. Probar el flujo end-to-end

1. Entrar a `https://login.gruposur.ar` y loguearse con Google.
2. En el hub aparece la tarjeta **Inventario** → click.
3. Debe abrir `https://inventario.gruposur.ar` **sin pedir otro login**.
4. Quitar el módulo a un usuario → al entrar debe recibir **403**.

---

## 9. Actualizar la app más adelante

```bash
cd inventory-webapp
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
  -o bin/server-linux-amd64 ./cmd/server
scp bin/server-linux-amd64 usuario@IP_DEL_VPS:/tmp/server
ssh usuario@IP_DEL_VPS 'sudo mv /tmp/server /opt/inventario/server && \
  sudo chown inventario:inventario /opt/inventario/server && \
  sudo systemctl restart inventario'
```

> Si cambiaste algo en `static/` (PWA, íconos, CSS), volvé a copiar la carpeta.
> El binario no la embebe.

---

## Alternativa: ngrok en vez de Caddy

Si en tu VPS exponés con **ngrok** en lugar de Caddy, necesitás que
`inventario.gruposur.ar` llegue al puerto `4040`. En ngrok esto requiere un
segundo endpoint/dominio reservado (plan pago) o un `ngrok.yml` con dos túneles
(`:8080` y `:4040`). El resto de la guía (systemd, `.env`, DNS, acceso) es igual;
sólo cambia cómo publicás el `:4040` hacia el subdominio HTTPS.
