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
#    - Copy the compiled DLL to <project-root>/dist/downloaded/windows_amd64
#
# 3. Sign All Binaries (Windows environment)
#    ```
#    .\packaging\windows\sign_binaries_in_dist.ps1 -certSha1 <SHA1_of_the_certificate>
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
#    .\packaging\windows\sign_binaries_in_dist.ps1 -certSha1 <SHA1_of_the_certificate>
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
# -i: (interactive) Can prompt for user input (e.g. when a file is not found in the primary folder but found in the alternate folder)
# -v: (version)     Explicitly set the version to use for the installer file name
# -e: (erase)       Just erase work directories
#------------------------------------------------------------------------------
param (
    [Alias('i')]
    [switch]$interactive,

    [Alias('v')]
    [string]$version,

    [Alias('e')]
    [switch]$erase
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
        [string[]]$AlternateSourceDirs  # Changed from single string to array
    )
    $destinationPath = "$DestinationDir/$File"
    $fullSourcePath  = if ($SourceDir) { "$SourceDir/$File" } else { "" }    
    
    if ($AlternateSourceDirs -and (-not $fullSourcePath -or -not (Test-Path -Path $fullSourcePath))) {
        # File doesn't exist, check in alternate folders
        $foundInAlternate = $false
        
        foreach ($altDir in $AlternateSourceDirs) {
            $fallbackSourcePath = "$altDir/$File"    
            if (Test-Path -Path $fallbackSourcePath) {
                if ($interactive -and $fullSourcePath) { # Do not prompt if the sourceDir is empty or "interactive" mode is not set
                    $response = Read-Host "    [?] The file '$File' found in fallback '$altDir' folder.`n    Do you want to use it? (y/n)"
                    if ($response -ne 'y' -and $response -ne 'Y') {
                        continue  # Try next alternate directory
                    } 
                }           
                $fullSourcePath = $fallbackSourcePath
                $foundInAlternate = $true
                break  # Found a usable file, stop searching
            }
        }
        
        if (-not $foundInAlternate) {
            $altDirsString = $AlternateSourceDirs -join "', '"
            Write-Error "Required file '$File' not found in: '$SourceDir', '$altDirsString'"
            exit 1
        }
    }

    try {
        # Print details about the file
        $fileInfo = Get-Item -Path $fullSourcePath        
        $output = "{0,-22}: {1,-29} -> {2,-38} [{3,-20} {4,18}{5}]" -f 
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

# Function to set and restore Cargo.toml version
function Set-CargoVersion { 
    param ([string]$Version)
    if (-not (Test-Path "Cargo.toml.bak")) {        
        Copy-Item "Cargo.toml" "Cargo.toml.bak" -Force
    }
    # Update the version in Cargo.toml.
    # This will allow the Tauri CLI to set the correct filename for the installer.
    # NOTE: This works only when the version is not explicitly defined in tauri.conf.json5.
    (Get-Content "Cargo.toml" -Raw) -replace '(\[package\][^\[]*?)version\s*=\s*"[^"]+"', ('$1version = "' + $Version + '"') | Set-Content "Cargo.toml"
}
function Restore-CargoVersion {
    if (Test-Path "Cargo.toml.bak") {
        Copy-Item "Cargo.toml.bak" "Cargo.toml" -Force
        Remove-Item "Cargo.toml.bak" -Force
    }
}

function Get-GitTagVersion {
    # Check if running in Docker and configure Git accordingly
    if ($env:ComputerName -like "*container*" -or $env:USERNAME -eq "ContainerAdministrator") {
        $currentDir = (Get-Location).Path        
        git config --global --add safe.directory $currentDir
    }

    # Try to get exact tag pointing to current commit
    $version = $(git tag --points-at 2>$null)    
    # If no tag points to current commit, use most recent tag
    if ([string]::IsNullOrEmpty($version)) {
        $devVersion = $(git describe --tags --first-parent --abbrev=0 2>$null)
        if (-not [string]::IsNullOrEmpty($devVersion)) {
            $version = "${devVersion}"
        }
    }
    $version = $version -replace '^v', ''
    return $version
}
# >>>>>>>>>>>>>>>>>>>>>>> End Functions >>>>>>>>>>>>>>>>>>>>>>>>

# Set-Location relative to the script location "../.." (root of the project). So that the script can be run from any location.
Set-Location -Path (Join-Path -Path $PSScriptRoot -ChildPath "../..")
try {
    # CONSTANTS
    $destinationDir = "desktop/tauri/src-tauri"
    $binaryDir = "$destinationDir/binary"           #portmaster\desktop\tauri\src-tauri\binary
    $intelDir  = "$destinationDir/intel"            #portmaster\desktop\tauri\src-tauri\intel
    $targetBase= "$destinationDir/target"           #portmaster\desktop\tauri\src-tauri\target
    $targetDir = "$targetBase/release"              #portmaster\desktop\tauri\src-tauri\target\release

    # Erasing work directories
    Write-Output "[+] Erasing work directories: '$binaryDir', '$intelDir', '$targetBase'"
    Remove-Item -Recurse -Force -Path $binaryDir -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force -Path $intelDir -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force -Path $targetBase -ErrorAction SilentlyContinue
    if ($erase) {
        Write-Output "[ ] Done"
        exit 0
    }

    # Copying BINARY FILES
    Write-Output "`n[+] Copying binary files:"
    $filesToCopy = @(
        @{Folder="dist/binary/windows_amd64";   File="portmaster-kext.sys";     Destination=$binaryDir; AlternateSourceDirs=@("dist/downloaded/windows_amd64", "dist")},
        @{Folder="dist/binary/windows_amd64";   File="portmaster-core.dll";     Destination=$binaryDir; AlternateSourceDirs=@("dist/downloaded/windows_amd64", "dist")},
        @{Folder="dist/binary/windows_amd64";   File="portmaster-core.exe";     Destination=$binaryDir; AlternateSourceDirs=@("dist")},
        @{Folder="dist/binary/windows_amd64";   File="WebView2Loader.dll";      Destination=$binaryDir; AlternateSourceDirs=@("dist")},
        @{Folder="dist/binary/all";             File="portmaster.zip";          Destination=$binaryDir; AlternateSourceDirs=@("dist")},
        @{Folder="dist/binary/all";             File="assets.zip";              Destination=$binaryDir; AlternateSourceDirs=@("dist")},
        @{Folder="dist/binary/windows_amd64";   File="portmaster.exe";          Destination=$targetDir; AlternateSourceDirs=@("dist")}
    )
    foreach ($file in $filesToCopy) {    
        Find-And-Copy-File -SourceDir $file.Folder -File $file.File -DestinationDir $file.Destination -AlternateSourceDirs $file.AlternateSourceDirs
    }

    # Copying INTEL FILES
    Write-Output "`n[+] Copying intel files"
    if (-not (Test-Path -Path $intelDir)) {
        New-Item -ItemType Directory -Path $intelDir -ErrorAction Stop > $null
    }
    Copy-Item -Force -Path "dist/intel/*" -Destination "$intelDir/" -ErrorAction Stop
} catch {
    Set-Location $originalDirectory
    Write-Error "[!] Failed! Error: $_"
    exit 1
}

$VERSION_GIT_TAG    = Get-GitTagVersion

# Check versions of UI and Core binaries
$VERSION_UI         = (Get-Item "$targetDir/portmaster.exe").VersionInfo.FileVersion
$VERSION_CORE       = (& "$binaryDir/portmaster-core.exe" version | Select-String -Pattern "Portmaster\s+(\d+\.\d+\.\d+)" | ForEach-Object { $_.Matches.Groups[1].Value })
$VERSION_KEXT       = (Get-Item "$binaryDir/portmaster-kext.sys").VersionInfo.FileVersion
Write-Output "`n[i] VERSIONS INFO:"
Write-Output "    VERSION_GIT_TAG : $VERSION_GIT_TAG"
Write-Output "    VERSION_CORE    : $VERSION_CORE"
Write-Output "    VERSION_UI      : $VERSION_UI"
Write-Output "    VERSION_KEXT    : $VERSION_KEXT"
if ($VERSION_UI -ne $VERSION_CORE -or $VERSION_CORE -ne $VERSION_GIT_TAG) {
    Write-Warning "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    Write-Warning "Version mismatch between UI($VERSION_UI), Core($VERSION_CORE) and GitTag($VERSION_GIT_TAG)!"
    Write-Warning "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    if ($interactive) {
        $response = Read-Host "[?] Continue anyway? (y/n)"
        if ($response -ne 'y' -and $response -ne 'Y') {
            Write-Error "Cancelled. Version mismatch between UI and Core binaries."
            exit 1
        }     
    } 
}
# Determine which version to use for building
if ($version) {
    Write-Output "`n[i] Using explicitly provided version ($version) for installer file name`n"
    $VERSION_TO_USE  = $version
} else {
    Write-Output "`n[i] Using Core version version ($VERSION_CORE) for installer file name`n"
    $VERSION_TO_USE  = $VERSION_CORE    
}

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

    Write-Output "[i] Tools versions info:"
    Write-Output "    Tauri CLI: $((& $cargoTauriCommand -V | Out-String).Trim().Replace("`r`n", " "))"
    Write-Output "    Rust     : $((rustc -V | Out-String).Trim().Replace("`r`n", " ")); $((cargo -V | Out-String).Trim().Replace("`r`n", " "))"
    Write-Output ""

    # Building Tauri app bundle
    try {
        Write-Output "[+] Building Tauri app bundle with version $VERSION_TO_USE"
        Set-CargoVersion -Version $VERSION_TO_USE
        & $cargoTauriCommand bundle
        if ($LASTEXITCODE -ne 0) {
            throw "Tauri bundle command failed with exit code $LASTEXITCODE"         
        }
    }
    catch {
        Write-Error "[!] Bundle failed: $_"
        exit 1
    }
    finally {
       Restore-CargoVersion
    }

    Write-Output "[+] Copying generated bundles"
    $installerDist = "..\..\..\dist\windows_amd64\"
    if (-not (Test-Path -Path $installerDist)) {
        New-Item -ItemType Directory -Path $installerDist -ErrorAction Stop > $null
    }
    #Copy-Item -Path ".\target\release\bundle\msi\*"  -Destination $installerDist -ErrorAction Stop
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