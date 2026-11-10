# Despliegue en VPS Debian 11 (Hostinger)

Plataforma Grupo Sur — un binario Go (monolito modular) detrás de Caddy.

## 1. DNS

En el panel del dominio `gruposur.ar`, crear dos registros **A** apuntando a la
IP del VPS (la landing en `gruposur.ar` no se toca):

| Tipo | Nombre        | Valor          |
|------|---------------|----------------|
| A    | login         | IP_DEL_VPS     |
| A    | estadisticas  | IP_DEL_VPS     |

> Sin tildes en los subdominios.

## 2. Google OAuth

En https://console.cloud.google.com → APIs & Services → Credentials:

1. Crear **OAuth client ID** tipo *Web application*.
2. **Authorized redirect URI:** `https://login.gruposur.ar/auth/callback`
3. Guardar el *Client ID* y *Client Secret* para el `.env`.
4. En *OAuth consent screen*, agregar los dominios y publicar (o dejar en
   "testing" agregando los emails permitidos).

> El acceso real lo controla la app: solo entran los emails dados de alta y
> activos en la tabla `users` (admin inicial: `sebastian.chapartegui@gruposur.ar`).

## 3. Usuario del sistema y archivos

```bash
sudo useradd --system --home /opt/grupo-sur --shell /usr/sbin/nologin grupo-sur
sudo mkdir -p /opt/grupo-sur/{data,backups,deploy}
sudo chown -R grupo-sur:grupo-sur /opt/grupo-sur
```

Compilar el binario (en tu máquina) y subirlo:

```bash
make build-linux
scp bin/server-linux-amd64 usuario@IP_DEL_VPS:/tmp/server
scp deploy/backup.sh        usuario@IP_DEL_VPS:/tmp/backup.sh
ssh usuario@IP_DEL_VPS 'sudo mv /tmp/server /opt/grupo-sur/server && \
  sudo mv /tmp/backup.sh /opt/grupo-sur/deploy/backup.sh && \
  sudo chmod +x /opt/grupo-sur/server /opt/grupo-sur/deploy/backup.sh && \
  sudo chown grupo-sur:grupo-sur /opt/grupo-sur/server /opt/grupo-sur/deploy/backup.sh'
```

## 4. Configuración (`/opt/grupo-sur/.env`)

```env
ADDR=:8080
BASE_URL=https://estadisticas.gruposur.ar
LOGIN_URL=https://login.gruposur.ar
COOKIE_DOMAIN=.gruposur.ar
SECURE_COOKIE=1
SESSION_KEY=<generar: openssl rand -hex 32>
DB_PATH=/opt/grupo-sur/data/gruposur.db
ADMIN_EMAILS=sebastian.chapartegui@gruposur.ar
GOOGLE_CLIENT_ID=<...>
GOOGLE_CLIENT_SECRET=<...>
OAUTH_REDIRECT_URL=https://login.gruposur.ar/auth/callback
DEV_AUTH_BYPASS=0
```

```bash
sudo chown grupo-sur:grupo-sur /opt/grupo-sur/.env
sudo chmod 600 /opt/grupo-sur/.env
```

## 5. systemd

```bash
sudo cp deploy/grupo-sur.service /etc/systemd/system/grupo-sur.service
sudo systemctl daemon-reload
sudo systemctl enable --now grupo-sur
sudo systemctl status grupo-sur
curl -s http://127.0.0.1:8080/healthz   # -> ok
```

## 6. Caddy (reverse proxy + HTTPS)

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy

sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy obtiene certificados automáticamente. Verificar:
`https://estadisticas.gruposur.ar/visitas` y `https://login.gruposur.ar`.

## 7. Backups

```bash
sudo apt install -y sqlite3
echo '15 3 * * * grupo-sur DB_PATH=/opt/grupo-sur/data/gruposur.db /opt/grupo-sur/deploy/backup.sh >> /var/log/grupo-sur-backup.log 2>&1' | sudo tee /etc/cron.d/grupo-sur-backup
```

## 8. Actualizar la app

```bash
make build-linux
scp bin/server-linux-amd64 usuario@IP_DEL_VPS:/tmp/server
ssh usuario@IP_DEL_VPS 'sudo mv /tmp/server /opt/grupo-sur/server && \
  sudo chown grupo-sur:grupo-sur /opt/grupo-sur/server && \
  sudo systemctl restart grupo-sur'
```

## Flujo de uso

1. `https://login.gruposur.ar` → "Iniciar sesión con Google".
2. Tras autenticar, aterriza en el **hub de módulos** (tarjetas según acceso).
3. Tarjeta **Estadísticas** → `https://estadisticas.gruposur.ar/estadisticas`
   (secciones) → **Visitas** (`/visitas`, dashboard).
4. Importar datos: `…/visitas/importar` (subir el `.zip` del CTMS).
5. Admin de usuarios: `…/admin/usuarios` (alta/baja y asignación de módulos).
