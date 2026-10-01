<#
.SYNOPSIS
  One-command HDMS staging setup on a Windows PC, reachable by every device on the LAN.

.DESCRIPTION
  Brings up the staging stack (docker-compose.staging.yml over deploy/production/compose.yaml)
  and makes it reachable at https://<this-pc>:9443 from other devices:

    1. Checks Docker Desktop is running; installs mkcert with winget if missing.
    2. Creates .env.staging from .env.staging.example with freshly generated secrets
       (an existing .env.staging is left untouched).
    3. Issues a certificate for localhost, this PC's LAN IP and its hostname, signed by
       the local mkcert CA, and exports the CA as certs\hdms-staging-rootCA.crt for testers.
    4. Opens inbound TCP 9443 in Windows Firewall (Private and Domain networks only).
    5. Builds and starts the stack, then waits for /v1/healthz over the LAN address.
    6. Optionally seeds pilot-scale synthetic data and bootstraps an admin account.

  Safe to re-run: it reissues the certificate (picking up a changed IP) and restarts Caddy.

.PARAMETER Seed
  Load the pilot-scale synthetic dataset (~800 staff, ~500 devices, 5 000 loans). Run once.

.PARAMETER AdminEmail
  Create an admin console account with this email. Prompts for the password and prints
  the TOTP secret to scan into an authenticator app.

.PARAMETER AdminName
  Full name for the admin account created by -AdminEmail.

.PARAMETER LanIp
  The IPv4 address testers use. Defaults to the address of the adapter that has a
  default gateway; pass it explicitly if the PC has several network adapters.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File deploy\staging-lan.ps1

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File deploy\staging-lan.ps1 -Seed -AdminEmail admin@staging.test
#>
[CmdletBinding()]
param(
    [switch]$Seed,
    [string]$AdminEmail,
    [string]$AdminName = 'Staging Admin',
    [string]$LanIp
)

$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 may default to TLS 1.0, which Caddy refuses.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$Port = 9443
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $Root

function Write-Step([string]$Message) { Write-Host "==> $Message" -ForegroundColor Cyan }

# Runs a native command and fails the script on a non-zero exit code
# ($ErrorActionPreference does not cover native executables in Windows PowerShell).
function Invoke-Native([string]$Exe, [string[]]$Arguments) {
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

function New-RandomBytes([int]$Count) {
    $bytes = New-Object byte[] $Count
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    $rng.Dispose()
    return , $bytes
}
function New-HexSecret([int]$Count) { ((New-RandomBytes $Count) | ForEach-Object { $_.ToString('x2') }) -join '' }
function New-Base64Secret([int]$Count) { [Convert]::ToBase64String((New-RandomBytes $Count)) }

$Compose = @('compose', '-f', 'deploy/production/compose.yaml', '-f', 'docker-compose.staging.yml', '--env-file', '.env.staging')

# --- 0. Administrator (needed for the firewall rule) ---------------------------------
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this from a PowerShell window opened with "Run as administrator".'
}

# --- 1. Prerequisites ------------------------------------------------------------------
Write-Step 'Checking Docker Desktop'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'Docker is not installed. Install Docker Desktop (https://www.docker.com/products/docker-desktop/) and re-run.'
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) { throw 'Docker Desktop is not running. Start it, wait until it says "Engine running", and re-run.' }

if (-not (Get-Command mkcert -ErrorAction SilentlyContinue)) {
    Write-Step 'Installing mkcert with winget'
    Invoke-Native 'winget' @('install', '--id', 'FiloSottile.mkcert', '--exact')
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (Get-Command mkcert -ErrorAction SilentlyContinue)) {
        throw 'mkcert was installed but is not on PATH yet. Open a new administrator PowerShell window and re-run.'
    }
}

# --- 2. LAN identity -------------------------------------------------------------------
if (-not $LanIp) {
    $LanIp = Get-NetIPConfiguration |
        Where-Object { $_.IPv4DefaultGateway -and $_.NetAdapter.Status -eq 'Up' } |
        Select-Object -First 1 -ExpandProperty IPv4Address |
        Select-Object -First 1 -ExpandProperty IPAddress
    if (-not $LanIp) { throw 'Could not detect a LAN IPv4 address. Pass it with -LanIp 192.168.x.y.' }
}
$HostName = $env:COMPUTERNAME.ToLower()
Write-Step "LAN address: $LanIp ($HostName)"

$profiles = Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' }
if ($profiles) {
    Write-Warning ("Network '{0}' is set to Public, where the firewall rule below does not apply. " -f ($profiles.Name -join ', ') +
        'Switch it to Private in Settings > Network & internet > (your network) > Network profile type.')
}

# --- 3. .env.staging -------------------------------------------------------------------
if (Test-Path .env.staging) {
    Write-Step '.env.staging exists, keeping it'
} else {
    Write-Step 'Creating .env.staging with fresh secrets'
    $dbPassword = New-HexSecret 16
    $envText = Get-Content .env.staging.example -Raw
    # Values are hex/base64, so none contains a regex replacement token ($).
    $replacements = [ordered]@{
        'POSTGRES_PASSWORD'       = $dbPassword
        'HDMS_DATABASE_URL'       = "postgres://hdms_staging:$dbPassword@localhost:5443/hdms_staging?sslmode=disable"
        'HDMS_TOKEN_PEPPER'       = New-HexSecret 32
        'HDMS_CREDENTIAL_ENC_KEY' = New-Base64Secret 32
        'HDMS_TOTP_ENC_KEY'       = New-Base64Secret 32
        'HDMS_BACKUP_ENC_KEY'     = New-Base64Secret 32
    }
    foreach ($key in $replacements.Keys) {
        $envText = $envText -replace "(?m)^$key=.*$", "$key=$($replacements[$key])"
    }
    # UTF-8 without BOM: a BOM would corrupt the first variable name for docker compose.
    [IO.File]::WriteAllText((Join-Path $Root '.env.staging'), $envText, (New-Object Text.UTF8Encoding $false))
}

# --- 4. TLS certificate and CA for testers ----------------------------------------------
Write-Step 'Installing the mkcert CA on this PC (confirm the Windows security prompt if shown)'
Invoke-Native 'mkcert' @('-install')

Write-Step "Issuing certificate for localhost, $LanIp, $HostName, $HostName.local"
New-Item -ItemType Directory -Force -Path certs | Out-Null
Invoke-Native 'mkcert' @('-cert-file', 'certs/localhost.pem', '-key-file', 'certs/localhost-key.pem',
    'localhost', '127.0.0.1', '::1', $LanIp, $HostName, "$HostName.local")

$caRoot = (& mkcert -CAROOT).Trim()
$caForTesters = Join-Path $Root 'certs\hdms-staging-rootCA.crt'
Copy-Item (Join-Path $caRoot 'rootCA.pem') $caForTesters -Force

# --- 5. Firewall -----------------------------------------------------------------------
$ruleName = "HDMS staging (TCP $Port)"
if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
    Write-Step "Opening inbound TCP $Port in Windows Firewall (Private, Domain)"
    New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Protocol TCP -LocalPort $Port `
        -Action Allow -Profile Private, Domain | Out-Null
}

# --- 6. Stack --------------------------------------------------------------------------
Write-Step 'Building and starting the staging stack (first build takes several minutes)'
Invoke-Native 'docker' ($Compose + @('up', '-d', '--build'))
# Caddy only reads the certificate at startup; pick up the one just issued.
Invoke-Native 'docker' ($Compose + @('restart', 'caddy'))

Write-Step "Waiting for https://${LanIp}:$Port/v1/healthz"
$healthy = $false
for ($i = 0; $i -lt 60 -and -not $healthy; $i++) {
    try {
        Invoke-RestMethod -Uri "https://${LanIp}:$Port/v1/healthz" -TimeoutSec 5 | Out-Null
        $healthy = $true
    } catch {
        Start-Sleep -Seconds 5
    }
}
if (-not $healthy) {
    throw "Staging did not become healthy within 5 minutes. Inspect: docker $($Compose -join ' ') logs api caddy"
}

if ($Seed) {
    Write-Step 'Seeding pilot-scale synthetic data'
    Invoke-Native 'docker' ($Compose + @('exec', '-T', 'api', 'hdms-cli', 'seed', '--scale'))
}

if ($AdminEmail) {
    Write-Step "Creating admin account $AdminEmail"
    Invoke-Native 'docker' ($Compose + @('exec', 'api', 'hdms-cli', 'admin', 'bootstrap', '--email', $AdminEmail, '--name', $AdminName))
}

# --- 7. Summary ------------------------------------------------------------------------
Write-Host ''
Write-Host 'HDMS staging is up.' -ForegroundColor Green
Write-Host ''
Write-Host 'Tester URLs (use the IP if a device cannot resolve the name):'
Write-Host "  Kiosk  https://${LanIp}:$Port/        https://${HostName}.local:$Port/"
Write-Host "  Admin  https://${LanIp}:$Port/admin/  https://${HostName}.local:$Port/admin/"
Write-Host "  Staff  https://${LanIp}:$Port/staff/  https://${HostName}.local:$Port/staff/"
Write-Host ''
Write-Host "Give testers this CA certificate and have them trust it once per device:"
Write-Host "  $caForTesters"
Write-Host '  iPad/iPhone: open it, install the profile, then enable it in'
Write-Host '               Settings > General > About > Certificate Trust Settings'
Write-Host '  Windows:     double-click > Install Certificate > Trusted Root Certification Authorities'
Write-Host '  Android:     Settings > Security > Install a certificate > CA certificate'
Write-Host "  Never share rootCA-key.pem from $caRoot."
Write-Host ''
Write-Host 'Pair a kiosk:  docker ' -NoNewline
Write-Host ($Compose + @('exec', 'api', 'hdms-cli', 'kiosk', 'register', '--name', '"Counter 1"') -join ' ')
Write-Host '               then the same command with: kiosk pairing-code <kiosk id>'
Write-Host 'Stop staging:  docker ' -NoNewline
Write-Host ($Compose + @('down') -join ' ')
