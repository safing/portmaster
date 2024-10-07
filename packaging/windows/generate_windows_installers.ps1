# Save the current directory
$originalDirectory = Get-Location

$destinationDir = "desktop/tauri/src-tauri"
$binaryDir = "$destinationDir/binary"
$intelDir = "$destinationDir/intel"

# Make sure distination folder exists.
if (-not (Test-Path -Path $binaryDir)) {
    New-Item -ItemType Directory -Path $binaryDir > $null
}

# Copy binary files
Copy-Item -Force -Path "dist/binary/bin-index.json" -Destination "$binaryDir/bin-index.json"
Copy-Item -Force -Path "dist/binary/windows_amd64/portmaster-core.exe" -Destination "$binaryDir/portmaster-core.exe"
Copy-Item -Force -Path "dist/binary/windows_amd64/portmaster-kext.sys" -Destination "$binaryDir/portmaster-kext.sys"
Copy-Item -Force -Path "dist/binary/all/portmaster.zip" -Destination "$binaryDir/portmaster.zip"
Copy-Item -Force -Path "dist/binary/all/assets.zip" -Destination "$binaryDir/assets.zip"
Copy-Item -Force -Path "dist/binary/windows_amd64/portmaster.exe" -Destination "$destinationDir/target/release/portmaster.exe"

# Make sure distination folder exists.
if (-not (Test-Path -Path $intelDir)) {
    New-Item -ItemType Directory -Path $intelDir > $null
}
# Copy intel data
Copy-Item -Force -Path "dist/intel_decompressed/*" -Destination "$intelDir/"

Set-Location $destinationDir

# Download tauri-cli
Invoke-WebRequest -Uri https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v2.0.1/cargo-tauri-x86_64-pc-windows-msvc.zip -OutFile tauri-cli.zip
Expand-Archive -Force tauri-cli.zip

./tauri-cli/cargo-tauri.exe bundle

$installerDist = "..\..\..\dist\windows_amd64\"
# Make sure distination folder exists.
if (-not (Test-Path -Path $installerDist)) {
    New-Item -ItemType Directory -Path $installerDist > $null
}

Copy-Item -Path ".\target\release\bundle\msi\*" -Destination $installerDist
Copy-Item -Path ".\target\release\bundle\nsis\*" -Destination $installerDist

# Restore the original directory
Set-Location $originalDirectory