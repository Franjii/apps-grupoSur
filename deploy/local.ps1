param(
    [ValidateSet('Start', 'Stop')][string]$Action = 'Start',
    [string]$InventoryRoot = 'C:\inventory-rebuild',
    [int]$InventoryPort = 8083
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$stateDir = Join-Path $root 'data\local-integration'
$stateFile = Join-Path $stateDir 'processes.json'
$platformExe = Join-Path $root 'bin\platform-local.exe'
$inventoryExe = Join-Path $root 'bin\inventory-local.exe'

if ($Action -eq 'Stop') {
    if (Test-Path -LiteralPath $stateFile) {
        $state = Get-Content -LiteralPath $stateFile -Raw | ConvertFrom-Json
        foreach ($entry in $state) {
            $process = Get-Process -Id $entry.Id -ErrorAction SilentlyContinue
            if ($process -and $process.Path -eq $entry.Path -and $process.StartTime.ToUniversalTime().Ticks.ToString() -eq $entry.StartTicks) {
                Stop-Process -Id $process.Id
            }
        }
        Remove-Item -LiteralPath $stateFile
    }
    Write-Output 'Local integration stopped. Databases retained.'
    exit
}

foreach ($port in @(8080,$InventoryPort)) {
    if ([Net.NetworkInformation.IPGlobalProperties]::GetIPGlobalProperties().GetActiveTcpListeners().Port -contains $port) {
        throw "Port $port is already in use. Stop the existing server before starting."
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $InventoryRoot 'go.mod'))) { throw "Inventory project not found: $InventoryRoot" }
New-Item -ItemType Directory -Force -Path $stateDir,(Join-Path $root 'bin') | Out-Null
$env:CGO_ENABLED = '0'
Push-Location $root
try { go build -o $platformExe ./cmd/server; if ($LASTEXITCODE -ne 0) { throw 'Platform build failed' } } finally { Pop-Location }
Push-Location $InventoryRoot
try { go build -o $inventoryExe ./cmd/inventory; if ($LASTEXITCODE -ne 0) { throw 'Inventory build failed' } } finally { Pop-Location }

$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes); $rng.Dispose()
$env:SSO_SHARED_SECRET = [Convert]::ToBase64String($bytes)
$env:ADDR = '127.0.0.1:8080'
$env:BASE_URL = 'http://localhost:8080'
$env:LOGIN_URL = 'http://localhost:8080'
$env:INVENTARIO_BASE_URL = "http://localhost:$InventoryPort"
$env:COOKIE_DOMAIN = ''
$env:SECURE_COOKIE = 'false'
$env:SESSION_KEY = $env:SSO_SHARED_SECRET
$env:DB_PATH = Join-Path $stateDir 'platform.db'
$env:DEV_AUTH_BYPASS = 'true'
$env:DEV_USER_EMAIL = 'local-test@example.invalid'
$env:ADMIN_EMAILS = $env:DEV_USER_EMAIL
$env:INVENTORY_AUTH_MODE = 'sso'
$env:PLATFORM_INTROSPECT_URL = 'http://127.0.0.1:8080/auth/introspect'
$env:PLATFORM_LOGIN_URL = 'http://localhost:8080/auth/login'
$env:PLATFORM_LOGOUT_URL = 'http://localhost:8080/logout'
$env:INVENTORY_SECURE_COOKIE = 'false'
$env:INVENTORY_DB = Join-Path $stateDir 'inventory.db'
$env:PORT = [string]$InventoryPort
$started = @()
try {
    $started += Start-Process -FilePath $platformExe -WorkingDirectory $root -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $stateDir 'platform.out.log') -RedirectStandardError (Join-Path $stateDir 'platform.err.log')
    $started += Start-Process -FilePath $inventoryExe -WorkingDirectory $InventoryRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $stateDir 'inventory.out.log') -RedirectStandardError (Join-Path $stateDir 'inventory.err.log')
    $ready = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        if (@($started | Where-Object HasExited).Count) { throw "Server stopped. See logs in $stateDir" }
        try {
            $health = Invoke-WebRequest 'http://127.0.0.1:8080/healthz' -UseBasicParsing -TimeoutSec 5
            $manifest = Invoke-WebRequest "http://127.0.0.1:$InventoryPort/manifest.webmanifest" -UseBasicParsing -TimeoutSec 5
            if ($health.StatusCode -eq 200 -and $manifest.StatusCode -eq 200) { $ready = $true; break }
        } catch { Start-Sleep -Milliseconds 200 }
    }
    if (-not $ready) { throw "Servers did not become ready. See logs in $stateDir" }
    $started | ForEach-Object { @{Id=$_.Id;Path=$_.Path;StartTicks=$_.StartTime.ToUniversalTime().Ticks.ToString()} } | ConvertTo-Json | Set-Content -LiteralPath $stateFile -Encoding UTF8
    Write-Output "Ready: http://localhost:8080/auth/login -> Inventory -> http://localhost:$InventoryPort"
    Write-Output "Local databases and logs: $stateDir"
} catch {
    $started | Where-Object { -not $_.HasExited } | Stop-Process
    throw
}
