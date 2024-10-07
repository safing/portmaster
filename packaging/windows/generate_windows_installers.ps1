# Tested with docker image 'abrarov/msvc-2022:latest'
# sha256:f49435d194108cd56f173ad5bc6a27c70eed98b7e8cd54488f5acd85efbd51c9

# Run: 
# Start powershell and cd to the root of the project. Then run:
# $path = Convert-Path .  # Get the absolute path of the current directory
# docker run -it --rm -v "${path}:C:/app" -w "C:/app" abrarov/msvc-2022 powershell -NoProfile -File C:/app/packaging/windows/generate_windows_installer.ps1

# Save the current directory
$originalDirectory = Get-Location

$destinationDir = "desktop/tauri/src-tauri"
$binaryDir = "$destinationDir/binary"
$intelDir = "$destinationDir/intel"

# Make sure distination folder exists.
if (-not (Test-Path -Path $binaryDir)) {
    New-Item -ItemType Directory -Path $binaryDir > $null
}

Write-Output "Copying binary files"
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

Write-Output "Copying intel files"
Copy-Item -Force -Path "dist/intel_decompressed/*" -Destination "$intelDir/"

Set-Location $destinationDir

if (-not (Get-Command cargo -ErrorAction SilentlyContinue)) {
    Write-Output "Installing rust toolchain..."
    Invoke-WebRequest -Uri https://win.rustup.rs/x86_64 -OutFile rustup.exe
    ./rustup.exe install stable
    $env:PATH += ";C:\Users\ContainerAdministrator\.rustup\toolchains\stable-x86_64-pc-windows-msvc\bin\"
} else {
    Write-Output "'cargo' command is already available"
}

Write-Output "Downloading tauri-cli"
Invoke-WebRequest -Uri https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v2.0.1/cargo-tauri-x86_64-pc-windows-msvc.zip -OutFile tauri-cli.zip
Expand-Archive -Force tauri-cli.zip
./tauri-cli/cargo-tauri.exe bundle


Write-Output "Copying generated bundles"
$installerDist = "..\..\..\dist\windows_amd64\"
# Make sure distination folder exists.
if (-not (Test-Path -Path $installerDist)) {
    New-Item -ItemType Directory -Path $installerDist > $null
}

Copy-Item -Path ".\target\release\bundle\msi\*" -Destination $installerDist
Copy-Item -Path ".\target\release\bundle\nsis\*" -Destination $installerDist

# Restore the original directory
Set-Location $originalDirectory