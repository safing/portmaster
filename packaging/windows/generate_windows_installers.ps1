#------------------------------------------------------------------------------
# Portmaster Windows Installer Generator
#------------------------------------------------------------------------------
# This script creates Windows installers (MSI and NSIS) for Portmaster application
# by combining pre-compiled binaries and packaging them with Tauri.
#
# ## Workflow for creating Portmaster Windows installers:
#
# 1. Compile Core Binaries (Linux environment)
#    ```
#    earthly +release-prep
#    ```
#    This compiles and places files into the 'dist' folder with the required structure.
#    Note: Latest KEXT binaries and Intel data will be downloaded from https://updates.safing.io
#
# 2. Compile Windows-Specific Binaries (Windows environment)
#    Some files cannot be compiled by Earthly and require Windows.
#    - Compile 'portmaster-core.dll' from the /windows_core_dll folder
#    - Copy the compiled DLL to <project-root>/dist/download/windows_amd64
#
# 3. Sign All Binaries (Windows environment)
#    ```
#    .\sign_binaries_in_dist.ps1 -certSha1 <SHA1_of_the_certificate>
#    ```
#    This signs all binary files in the dist directory
#
# 4. Create Installers (Windows environment)
#    Note! You can run it from docker container (see example bellow).
#    ```
#    .\generate_windows_installers.ps1
#    ```
#    Installers will be placed in <project-root>/dist/windows_amd64
#
# 5. Sign Installers (Windows environment)
#    ```
#    .\sign_binaries_in_dist.ps1 -certSha1 <SHA1_of_the_certificate>
#    ```
#    This signs the newly created installer files
#
#------------------------------------------------------------------------------
# Running inside Docker container
#       Tested with docker image 'abrarov/msvc-2022:latest'
#       sha256:f49435d194108cd56f173ad5bc6a27c70eed98b7e8cd54488f5acd85efbd51c9
# 
# Note! Ensure you switched Docker Desktop to use Windows containers.
# Start powershell and cd to the root of the project.
# Then run:
#   $path = Convert-Path .  # Get the absolute path of the current directory
#   docker run -it --rm -v "${path}:C:/app" -w "C:/app" abrarov/msvc-2022 powershell -NoProfile -File C:/app/packaging/windows/generate_windows_installers.ps1
#------------------------------------------------------------------------------
#
# Optional arguments:
# -i, --interactive: Can prompt for user input (e.g. when a file is not found in the primary folder but found in the alternate folder)
#------------------------------------------------------------------------------
param (
    [Alias('i')]
    [switch]$interactive
)

# Save the current directory
$originalDirectory = Get-Location

# <<<<<<<<<<<<<<<<<<<<<<< Functions <<<<<<<<<<<<<<<<<<<<<<<

# Function to copy a file, with fallback to an alternative location and detailed logging
# Parameters:
#   $SourceDir         - Primary directory to search for the file
#   $File              - Name of the file to copy
#   $DestinationDir    - Directory where the file will be copied to
#   $AlternateSourceDir - Fallback directory if file is not found in $SourceDir
# 
# Behavior:
# - Checks if the file exists in the primary source directory
# - If not found and an alternate directory is provided, checks there
# - In interactive mode, asks for confirmation before using the alternate source
# - Logs details about the copied file (path, size, timestamp, version)
# - Returns error and exits if file cannot be found or copied
function Find-And-Copy-File {
    param (
        [string]$SourceDir,        
        [string]$File,
        [string]$DestinationDir,
        [string]$AlternateSourceDir
    )
    $destinationPath = "$DestinationDir/$File"
    $fullSourcePath  = if ($SourceDir) { "$SourceDir/$File" } else { "" }    
    
    if ($AlternateSourceDir -and (-not $fullSourcePath -or -not (Test-Path -Path $fullSourcePath))) {
        # File doesn't exist, check in alternate folder
        $fallbackSourcePath = "$AlternateSourceDir/$File"                
        if (Test-Path -Path $fallbackSourcePath) {
            if ($interactive -and $fullSourcePath) { # Do not prompt if the sourceDir is empty or "interactive" mode is not set
                $response = Read-Host "    [?] The file '$File' found only in fallback '$AlternateSourceDir' folder.`n    Do you want to use it? (y/n)"
                if ($response -ne 'y' -and $response -ne 'Y') {
                    Write-Error "Cancelled. Required file not found: $fullSourcePath"
                    exit 1
                } 
            }           
            $fullSourcePath = $fallbackSourcePath            
        } else {
            Write-Error "Required file '$File' not found in: '$SourceDir', '$AlternateSourceDir'"
            exit 1
        }
    }

    try {
        # Print details about the file
        $fileInfo = Get-Item -Path $fullSourcePath        
        $output = "{0,-22}: {1,-28} -> {2,-38} [{3,-20} {4,18}{5}]" -f 
               $File,
               $(Split-Path -Path $fullSourcePath -Parent),
               $(Split-Path -Path $destinationPath -Parent),               
               "$($fileInfo.LastAccessTime.ToString("yyyy-MM-dd HH:mm:ss"));",
               "$($fileInfo.Length) bytes",
               $(if ($fileInfo.VersionInfo.FileVersion) { "; v$($fileInfo.VersionInfo.FileVersion)" } else { "" })
        Write-Output "$output"

        # Create destination directory if not exists
        if (-not (Test-Path -Path $DestinationDir)) {
            New-Item -ItemType Directory -Path $DestinationDir -ErrorAction Stop > $null
        }
        # Copy the file
        Copy-Item -Force -Path "${fullSourcePath}" -Destination "${destinationPath}" -ErrorAction Stop
    } catch {
        Write-Error "Failed to copy file from '$fullSourcePath' to '$destinationPath'.`nError: $_"
        exit 1
    }
}
# >>>>>>>>>>>>>>>>>>>>>>> End Functions >>>>>>>>>>>>>>>>>>>>>>>>

# CONSTANTS
$destinationDir = "desktop/tauri/src-tauri"
$binaryDir = "$destinationDir/binary"           #portmaster\desktop\tauri\src-tauri\binary
$intelDir  = "$destinationDir/intel"            #portmaster\desktop\tauri\src-tauri\intel
$targetDir = "$destinationDir/target/release"   #portmaster\desktop\tauri\src-tauri\target\release

# Copying BINARY FILES
Write-Output "`n[+] Copying binary files:"
$filesToCopy = @(
    @{Folder="";                            File="portmaster-kext.sys";     Destination=$binaryDir; AlternateFolder="dist/download/windows_amd64"},
    @{Folder="dist/binary/windows_amd64";   File="portmaster-core.dll";     Destination=$binaryDir; AlternateFolder="dist/download/windows_amd64"},
    @{Folder="dist/binary/windows_amd64";   File="portmaster-core.exe";     Destination=$binaryDir},
    @{Folder="dist/binary/windows_amd64";   File="WebView2Loader.dll";      Destination=$binaryDir},
    @{Folder="dist/binary/all";             File="portmaster.zip";          Destination=$binaryDir},
    @{Folder="dist/binary/all";             File="assets.zip";              Destination=$binaryDir},
    @{Folder="dist/binary";                 File="index.json";              Destination=$binaryDir},
    @{Folder="dist/binary/windows_amd64";   File="portmaster.exe";          Destination=$targetDir}
)
foreach ($file in $filesToCopy) {    
    Find-And-Copy-File -SourceDir $file.Folder -File $file.File -DestinationDir $file.Destination -AlternateSourceDir $file.AlternateFolder
}

# Copying INTEL FILES
Write-Output "`n[+] Copying intel files"
if (-not (Test-Path -Path $intelDir)) {
    New-Item -ItemType Directory -Path $intelDir -ErrorAction Stop > $null
}
Copy-Item -Force -Path "dist/intel/*" -Destination "$intelDir/" -ErrorAction Stop

Set-Location $destinationDir
try {
    # Ensure Rust toolchain is installed
    if (-not (Get-Command cargo -ErrorAction SilentlyContinue)) {
        Write-Output "[+] Installing rust toolchain..."        
        Start-BitsTransfer -Source "https://win.rustup.rs/x86_64" -Destination "rustup.exe"
        ./rustup.exe install --no-self-update stable
        $env:PATH += ";C:\Users\ContainerAdministrator\.rustup\toolchains\stable-x86_64-pc-windows-msvc\bin\"
    } 

    # Ensure Tauri CLI is available
    $cargoTauriCommand = "cargo-tauri.exe"
    if (-not (Get-Command $cargoTauriCommand -ErrorAction SilentlyContinue)) {    
        if (-not (Test-Path "./tauri-cli/cargo-tauri.exe")) {
            Write-Output "[+] Tauri CLI not found. Downloading tauri-cli"
            Start-BitsTransfer -Source "https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v2.2.7/cargo-tauri-x86_64-pc-windows-msvc.zip" -Destination "tauri-cli.zip"
            Expand-Archive -Force tauri-cli.zip
        }
        if (-not (Test-Path "./tauri-cli/cargo-tauri.exe")) {
            Write-Error "Tauri CLI not found. Download failed."
            exit 1
        }
        $cargoTauriCommand = "./tauri-cli/cargo-tauri.exe"
    }

    Write-Output "[i] VERSIONS INFO:"
    Write-Output "    Tauri CLI: $((& $cargoTauriCommand -V | Out-String).Trim().Replace("`r`n", " "))"
    Write-Output "    Rust     : $((rustc -V | Out-String).Trim().Replace("`r`n", " ")); $((cargo -V | Out-String).Trim().Replace("`r`n", " "))"
    Write-Output ""

    # Building Tauri app bundle
    Write-Output "[+] Building Tauri app bundle"    
    & $cargoTauriCommand bundle
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Tauri bundle command failed with exit code $LASTEXITCODE"
        exit 1
    }

    Write-Output "[+] Copying generated bundles"
    $installerDist = "..\..\..\dist\windows_amd64\"
    if (-not (Test-Path -Path $installerDist)) {
        New-Item -ItemType Directory -Path $installerDist -ErrorAction Stop > $null
    }
    Copy-Item -Path ".\target\release\bundle\msi\*"  -Destination $installerDist -ErrorAction Stop
    Copy-Item -Path ".\target\release\bundle\nsis\*" -Destination $installerDist -ErrorAction Stop

    Write-Output "[i] Done."
    Write-Output "    Installer files are available in:  $(Resolve-Path $installerDist)"
} catch {
    Write-Error "[!] Failed! Error: $_"
    exit 1
}
finally {
    # Restore the original directory if not already done
    Set-Location $originalDirectory
}