# This script builds the Tauri application for Portmaster on Windows.
# It optionally builds the required Angular tauri-builtin project first.
# The script assumes that all necessary dependencies (Node.js, Rust, etc.) are installed.
# Output file: dist/portmaster.exe

# Store original directory and find project root
$originalDir = Get-Location
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = (Get-Item $scriptDir).Parent.Parent.Parent.FullName

# Create output directory
$outputDir = Join-Path $scriptDir "dist"
New-Item -ItemType Directory -Path $outputDir -Force | Out-Null

# Ask if user wants to build the Angular tauri-builtin project
if ((Read-Host "Build Angular tauri-builtin project? (Y/N, default: Y)") -notmatch '^[Nn]$') {
    # Navigate to Angular project
    Set-Location (Join-Path $projectRoot "desktop\angular")
    
    # Build tauri-builtin project
    ng build --configuration production --base-href / tauri-builtin
    if ($LASTEXITCODE -ne 0) { Set-Location $originalDir; exit $LASTEXITCODE }
}

# Navigate to Tauri project directory
Set-Location (Join-Path $projectRoot "desktop\tauri\src-tauri")

# Build Tauri project for Windows
cargo tauri build --no-bundle
if ($LASTEXITCODE -ne 0) { Set-Location $originalDir; exit $LASTEXITCODE }

# Copy the output files to the script's dist directory
$tauriOutput = Join-Path (Get-Location) "target\release"
Copy-Item -Path "$tauriOutput\portmaster.exe" -Destination $outputDir -Force

# Return to original directory
Set-Location $originalDir
Write-Host "Build completed successfully: $outputDir\portmaster.exe" -ForegroundColor Green