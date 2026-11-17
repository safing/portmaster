<#
.SYNOPSIS
    Portmaster Patcher / Installer for Windows
    Replaces or upgrades Portmaster core with Rust core (pm-core)
    and modern unlocked UI with Xray proxy integration.

.DESCRIPTION
    Usage in PowerShell (Run as Administrator):
        irm https://raw.githubusercontent.com/VLOD-ZDOV/portmaster-rust/main/scripts/patch-portmaster.ps1 | iex
        .\scripts\patch-portmaster.ps1 [-Core rs|go] [-Tag latest|<tag>] [-Restore]
#>

[CmdletBinding()]
param (
    [ValidateSet("rs", "go")]
    [string]$Core = "rs",

    [string]$Tag = "latest",

    [switch]$Restore
)

$ErrorActionPreference = "Stop"

# Ensure Administrator
$currentPrincipal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Administrator privileges are required. Please run PowerShell as Administrator."
    exit 1
}

$Repo = "VLOD-ZDOV/portmaster-rust"
$InstallDir = "C:\Program Files\Portmaster"
$DataDir = "C:\ProgramData\safing\portmaster"
$BackupDir = Join-Path $DataDir "core-backups"

if (-not (Test-Path $InstallDir)) {
    $altInstall = "C:\Program Files (x86)\Portmaster"
    if (Test-Path $altInstall) {
        $InstallDir = $altInstall
    }
}

if (-not (Test-Path $DataDir)) {
    New-Item -ItemType Directory -Path $DataDir -Force | Out-Null
}
if (-not (Test-Path $BackupDir)) {
    New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null
}

if ($Restore) {
    Write-Host "[*] Restoring Portmaster from backup..." -ForegroundColor Cyan
    $backups = Get-ChildItem -Path $BackupDir -Directory | Sort-Object CreationTime -Descending
    if ($backups.Count -eq 0) {
        Write-Error "No backups found in $BackupDir"
        exit 1
    }
    $latestBackup = $backups[0].FullName
    Write-Host "[*] Restoring from $latestBackup..." -ForegroundColor Cyan

    Stop-Service -Name "Portmaster" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    if (Test-Path (Join-Path $latestBackup "portmaster-core.exe")) {
        Copy-Item -Path (Join-Path $latestBackup "portmaster-core.exe") -Destination (Join-Path $InstallDir "portmaster-core.exe") -Force
    }
    Start-Service -Name "Portmaster" -ErrorAction SilentlyContinue
    Write-Host "[+] Restore completed successfully." -ForegroundColor Green
    exit 0
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Portmaster Windows Patcher (Core: $Core, Tag: $Tag)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# Resolve release URLs
Write-Host "[*] Resolving release from GitHub ($Repo)..." -ForegroundColor Gray
$BaseUrl = ""
if ($Tag -eq "latest") {
    $apiUrl = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        $release = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing
        $resolvedTag = $release.tag_name
        $BaseUrl = "https://github.com/$Repo/releases/download/$resolvedTag"
    } catch {
        $BaseUrl = "https://github.com/$Repo/releases/latest/download"
    }
} else {
    $BaseUrl = "https://github.com/$Repo/releases/download/$Tag"
}

$binName = "portmaster-core-$Core.exe"
$binUrl = "$BaseUrl/$binName"
$uiUrl = "$BaseUrl/portmaster.zip"

$tempDir = Join-Path $env:TEMP ("pm-patch-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    # Download binary
    $tempBin = Join-Path $tempDir $binName
    Write-Host "[*] Downloading $binName from $binUrl..." -ForegroundColor Gray
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls13
    Invoke-WebRequest -Uri $binUrl -OutFile $tempBin -UseBasicParsing

    # Download UI bundle
    $tempZip = Join-Path $tempDir "portmaster.zip"
    $hasUi = $false
    try {
        Write-Host "[*] Downloading UI bundle (portmaster.zip)..." -ForegroundColor Gray
        Invoke-WebRequest -Uri $uiUrl -OutFile $tempZip -UseBasicParsing
        $hasUi = $true
    } catch {
        Write-Host "[*] UI bundle not found or download failed, continuing without UI update." -ForegroundColor Yellow
    }

    # Stop Portmaster Service
    $serviceRunning = $false
    $pmService = Get-Service -Name "Portmaster" -ErrorAction SilentlyContinue
    if ($pmService -and $pmService.Status -eq "Running") {
        Write-Host "[*] Stopping Portmaster service..." -ForegroundColor Gray
        Stop-Service -Name "Portmaster" -Force
        Start-Sleep -Seconds 2
        $serviceRunning = $true
    }

    # Backup existing binary
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $currentBackup = Join-Path $BackupDir $stamp
    New-Item -ItemType Directory -Path $currentBackup -Force | Out-Null

    $existingBin = Join-Path $InstallDir "portmaster-core.exe"
    if (Test-Path $existingBin) {
        Copy-Item -Path $existingBin -Destination (Join-Path $currentBackup "portmaster-core.exe") -Force
        Write-Host "[*] Backed up original binary to: $currentBackup" -ForegroundColor Gray
    }

    # Install new binary
    Copy-Item -Path $tempBin -Destination (Join-Path $InstallDir $binName) -Force
    Copy-Item -Path $tempBin -Destination (Join-Path $InstallDir "portmaster-core.exe") -Force

    # Also update in updates dir if present
    $updatesBin = Join-Path $DataDir "updates\windows_amd64\core\portmaster-core_windows_amd64.exe"
    if (Test-Path (Split-Path $updatesBin)) {
        Copy-Item -Path $tempBin -Destination $updatesBin -Force
    }

    # Update UI if downloaded
    if ($hasUi -and (Test-Path $tempZip)) {
        Write-Host "[*] Updating Web UI..." -ForegroundColor Gray
        $uiDest = Join-Path $DataDir "ui\modules\portmaster"
        New-Item -ItemType Directory -Path $uiDest -Force | Out-Null
        Expand-Archive -Path $tempZip -DestinationPath $uiDest -Force

        $updatesUi = Join-Path $DataDir "updates\windows_amd64\ui\modules\portmaster"
        if (Test-Path (Split-Path $updatesUi)) {
            New-Item -ItemType Directory -Path $updatesUi -Force | Out-Null
            Expand-Archive -Path $tempZip -DestinationPath $updatesUi -Force
        }
    }

    # Restart Portmaster Service
    if ($serviceRunning -or $pmService) {
        Write-Host "[*] Starting Portmaster service..." -ForegroundColor Gray
        Start-Service -Name "Portmaster" -ErrorAction SilentlyContinue
    }

    Write-Host ""
    Write-Host "==========================================================" -ForegroundColor Green
    Write-Host " [✓] Portmaster Windows patched successfully!" -ForegroundColor Green
    Write-Host "     Core: $binName" -ForegroundColor Green
    Write-Host "     Backup saved in: $currentBackup" -ForegroundColor Green
    Write-Host "==========================================================" -ForegroundColor Green
} finally {
    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
