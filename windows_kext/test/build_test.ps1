# Build and Sign Test Driver Script
# Must be run from Developer PowerShell for Visual Studio

$ErrorActionPreference = "Stop"

# Get script directory and set paths
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$rootDir = Split-Path -Parent $scriptDir
$driverDir = Join-Path $rootDir "driver"
$certPath = Join-Path $rootDir "test\_testcert\DriverTestCert.cer"
$outDir = Join-Path $scriptDir "_out"

# Create output directory if it doesn't exist
if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Path $outDir -Force | Out-Null
}

Write-Host "=================================================" -ForegroundColor Cyan
Write-Host "  Building and Signing Test Driver" -ForegroundColor Cyan
Write-Host "=================================================" -ForegroundColor Cyan
Write-Host ""

# Verify we are in the correct directory
if (-not (Test-Path $driverDir)) {
    Write-Host "ERROR: Driver directory not found at: $driverDir" -ForegroundColor Red
    Write-Host "Please run this script from the windows_kext root directory or ensure paths are correct." -ForegroundColor Red
    exit 1
}

# Verify certificate exists
if (-not (Test-Path $certPath)) {
    Write-Host "ERROR: Certificate not found at: $certPath" -ForegroundColor Red
    Write-Host "Please create the test certificate first." -ForegroundColor Red
    exit 1
}

#
# Step 1: Build Driver in Release Mode
#
Write-Host "[1/3] Building driver in release mode..." -ForegroundColor Yellow
Push-Location $driverDir
try {
    cargo build --release --target x86_64-pc-windows-msvc
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: Cargo build failed with exit code $LASTEXITCODE" -ForegroundColor Red
        exit $LASTEXITCODE
    }
    
    Write-Host "   Driver built successfully" -ForegroundColor Green
} finally {
    Pop-Location
}

#
# Step 2: Link the Driver
#
Write-Host "[2/3] Linking driver..." -ForegroundColor Yellow
Push-Location $outDir
try {
    # Copy the .lib file to output directory
    $libSource = Join-Path $driverDir "target\x86_64-pc-windows-msvc\release\driver.lib"
    $libDest = Join-Path $outDir "driver.lib"
    
    if (-not (Test-Path $libSource)) {
        Write-Host "ERROR: Built driver.lib not found at: $libSource" -ForegroundColor Red
        exit 1
    }
    
    Copy-Item $libSource $libDest -Force
    Write-Host "   Copied driver.lib" -ForegroundColor Green
    
    # Run linker script (from output directory so files are created here)
    $linkScript = Join-Path $rootDir "link-dev.ps1"
    if (-not (Test-Path $linkScript)) {
        Write-Host "ERROR: link-dev.ps1 not found at: $linkScript" -ForegroundColor Red
        exit 1
    }
    
    & $linkScript
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: Linking failed with exit code $LASTEXITCODE" -ForegroundColor Red
        exit $LASTEXITCODE
    }
    
    # Rename driver.sys to test name
    $sysFile = Join-Path $outDir "driver.sys"
    if (-not (Test-Path $sysFile)) {
        Write-Host "ERROR: driver.sys was not created" -ForegroundColor Red
        exit 1
    }
    
    $testSysFile = Join-Path $outDir "PortmasterKext_test.sys"
    Move-Item $sysFile $testSysFile -Force
    
    Write-Host "   Driver linked successfully (PortmasterKext_test.sys)" -ForegroundColor Green
} finally {
    Pop-Location
}

#
# Step 3: Sign the Driver
#
Write-Host "[3/3] Signing driver..." -ForegroundColor Yellow
Push-Location $outDir
try {
    $sysFile = Join-Path $outDir "PortmasterKext_test.sys"
    
    # Sign the driver
    SignTool sign /v /fd SHA256 /s PrivateCertStore /n DriverTestCert $sysFile
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: Signing failed with exit code $LASTEXITCODE" -ForegroundColor Red
        Write-Host "Make sure the certificate is installed in PrivateCertStore" -ForegroundColor Yellow
        exit $LASTEXITCODE
    }
    
    Write-Host "   Driver signed successfully" -ForegroundColor Green
    
    # Verify signature
    Write-Host ""
    Write-Host "Verifying signature..." -ForegroundColor Yellow
    SignTool verify /v /pa $sysFile
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "WARNING: Signature verification failed" -ForegroundColor Yellow
    } else {
        Write-Host "   Signature verified" -ForegroundColor Green
    }
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "=================================================" -ForegroundColor Cyan
Write-Host "  Build Complete!" -ForegroundColor Green
Write-Host "=================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Output directory: $outDir" -ForegroundColor White
Write-Host "Driver file: PortmasterKext_test.sys" -ForegroundColor White
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Run playground as Administrator" -ForegroundColor White
Write-Host "  2. Use start command to load the driver" -ForegroundColor White
Write-Host ""
