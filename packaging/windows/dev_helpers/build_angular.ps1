# This script builds the Angular project for the Portmaster application and packages it into a zip file.
# The script assumes that all necessary dependencies are installed and available.
# Output file: dist/portmaster.zip

[CmdletBinding()]
param (
    [Parameter(Mandatory=$false)]
    [Alias("d")]
    [switch]$Development,
    
    [Parameter(Mandatory=$false)]
    [Alias("i")]
    [switch]$Interactive
)

# Store original directory and find project root
$originalDir = Get-Location
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = (Get-Item $scriptDir).Parent.Parent.Parent.FullName

try {
    # Create output directory
    $outputDir = Join-Path $scriptDir "dist"
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null

    # Navigate to Angular project
    Set-Location (Join-Path $projectRoot "desktop\angular")

    # npm install - always run in non-interactive mode, ask in interactive mode
    if (!$Interactive -or (Read-Host "Run 'npm install'? (Y/N, default: Y)") -notmatch '^[Nn]$') {
        npm install
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    # build libs - always run in non-interactive mode, ask in interactive mode
    if (!$Interactive -or (Read-Host "Build shared libraries? (Y/N, default: Y)") -notmatch '^[Nn]$') {
        if ($Development) {
            Write-Host "Building shared libraries in development mode" -ForegroundColor Yellow
            npm run build-libs:dev
        } else {
            Write-Host "Building shared libraries in production mode" -ForegroundColor Yellow
            npm run build-libs
        }
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    # Build Angular project
    if ($Development) {
        Write-Host "Building Angular project in development mode" -ForegroundColor Yellow
        ng build --configuration development --base-href /ui/modules/portmaster/ portmaster
    } else {
        Write-Host "Building Angular project in production mode" -ForegroundColor Yellow
        ng build --configuration production --base-href /ui/modules/portmaster/ portmaster
    }
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    # Create zip archive
    Write-Host "Creating zip archive" -ForegroundColor Yellow
    Set-Location dist
    $destinationZip = Join-Path $outputDir "portmaster.zip"
    if ($PSVersionTable.PSVersion.Major -ge 5) {
        # Option 1: Use .NET Framework directly (faster than Compress-Archive)
        Write-Host "Using System.IO.Compression for faster archiving" -ForegroundColor Yellow
        if (Test-Path $destinationZip) { Remove-Item $destinationZip -Force }    # Remove existing zip if it exists
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $compressionLevel = [System.IO.Compression.CompressionLevel]::Optimal 
        [System.IO.Compression.ZipFile]::CreateFromDirectory((Get-Location), $destinationZip, $compressionLevel, $false)
    } 
    else {
        # Fall back to Compress-Archive
        Compress-Archive -Path * -DestinationPath $destinationZip -Force
    }
    
    Write-Host "Build completed successfully: $(Join-Path $outputDir "portmaster.zip")" -ForegroundColor Green
}
finally {
    # Return to original directory - this will execute even if Ctrl+C is pressed
    Set-Location $originalDir
}