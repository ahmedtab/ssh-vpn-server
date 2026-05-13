#Requires -RunAsAdministrator
<#
.SYNOPSIS
    SSH VPN POC - Windows 10/11
    Builds (if needed) and runs poc.exe, which creates a wintun TUN adapter and
    bridges packets through an SSH tun@openssh.com channel to the remote LAN.

.NOTES
    Prerequisites:
      1. Go 1.22+      https://go.dev/dl/
      2. wintun.dll    https://www.wintun.net/builds/wintun-0.14.1.zip
                       extract amd64\wintun.dll into .\poc\

    Must be run as Administrator (required for adapter creation and route changes).
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$PocDir    = Join-Path $PSScriptRoot "poc"
$PocExe    = Join-Path $PocDir "poc.exe"
$WintunDll = Join-Path $PocDir "wintun.dll"

function Write-Step([string]$msg) {
    Write-Host "[*] $msg" -ForegroundColor Cyan
}
function Write-Ok([string]$msg) {
    Write-Host "[+] $msg" -ForegroundColor Green
}
function Write-Fail([string]$msg) {
    Write-Host "[!] $msg" -ForegroundColor Red
}

# --- 1. Preflight checks ---

Write-Step "Checking prerequisites..."

if (-not (Test-Path $PocDir)) {
    Write-Fail "poc\ directory not found at $PocDir"
    exit 1
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Fail "Go not found in PATH. Install from https://go.dev/dl/ then restart this shell."
    exit 1
}
Write-Ok "Go $(go version)"

if (-not (Test-Path $WintunDll)) {
    Write-Fail "wintun.dll not found at $WintunDll"
    Write-Host ""
    Write-Host "  1. Download: https://www.wintun.net/builds/wintun-0.14.1.zip"
    Write-Host "  2. Open the zip and copy  amd64\wintun.dll  into  $PocDir"
    Write-Host ""
    exit 1
}
Write-Ok "wintun.dll found"

# --- 2. Build poc.exe (skipped if already built) ---

if (-not (Test-Path $PocExe)) {
    Write-Step "Building poc.exe (first run)..."
    Push-Location $PocDir
    try {
        Write-Step "  go build..."
        go build -o poc.exe .
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
        Pop-Location
    }
    Write-Ok "poc.exe built at $PocExe"
} else {
    Write-Ok "poc.exe already built - skipping build"
    Write-Host "    (delete poc\poc.exe and re-run to force a rebuild)"
}

# ── 3. Show config summary ─────────────────────────────────────────────────────

Write-Host ""
Write-Host "  Adapter      SSH-VPN-POC" -ForegroundColor White
Write-Host "  Local IP     10.10.11.1/30" -ForegroundColor White
Write-Host "  Remote IP    10.10.11.2  (server tunnel endpoint)" -ForegroundColor White
Write-Host "  LAN subnet   192.168.4.0/24  (split tunnel)" -ForegroundColor White
Write-Host "  SSH gateway  157.180.4.166:2255" -ForegroundColor White
Write-Host ""
Write-Host "  To test after connection:" -ForegroundColor DarkGray
Write-Host "    ping 10.10.11.2         # reach server TUN endpoint" -ForegroundColor DarkGray
Write-Host "    ping 192.168.4.1        # reach a host on the remote LAN" -ForegroundColor DarkGray
Write-Host ""

# --- 4. Run ---

Write-Step "Starting poc.exe (Ctrl+C to disconnect)..."
Write-Host ""

Set-Location $PocDir
& $PocExe

# --- 5. Post-run check ---

Write-Host ""
if (Get-NetAdapter -Name "SSH-VPN-POC" -ErrorAction SilentlyContinue) {
    Write-Host "[!] Adapter SSH-VPN-POC still exists (poc.exe may have crashed)." -ForegroundColor Yellow
    Write-Host "    To remove it manually run:" -ForegroundColor Yellow
    Write-Host '    Remove-NetAdapter -Name "SSH-VPN-POC" -Confirm:$false' -ForegroundColor Yellow
}
