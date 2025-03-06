param (
    [Parameter(Mandatory=$false)]
    [string]$certSha1,
    
    [Parameter(Mandatory=$false)]
    [string]$timestampServer = "http://timestamp.digicert.com"
)

function Show-Help {
    Write-Host "Usage: sign_binaries_in_dist.ps1 -certSha1 <CERT_SHA1> [-timestampServer <TIMESTAMP_SERVER>]"
    Write-Host ""
    Write-Host "This script signs all binary files located under the '<project root>\dist\' directory recursively."
    Write-Host "Which should be done before creating the Portmaster installer."
    Write-Host ""
    Write-Host "Arguments:"
    Write-Host "  -certSha1        The SHA1 hash of the certificate to use for signing (code signing certificate)."
    Write-Host "  -timestampServer The timestamp server URL to use (optional). Default is http://timestamp.digicert.com."
    Write-Host ""
    Write-Host "Example:"
    Write-Host "  .\sign_binaries_in_dist.ps1 -certSha1 ABCDEF1234567890ABCDEF1234567890ABCDEF12"
}

# Show help if no certificate SHA1 provided or help flag used
if (-not $certSha1 -or ($args -contains "-h") -or ($args -contains "-help") -or ($args -contains "/h")) {
    Show-Help
    exit 0
}

# Find signtool.exe - simplified approach
function Find-SignTool {
    # First try the PATH
    $signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($signtool) { return $signtool }

    Write-Host "[+] signtool.exe not found in PATH. Searching in common locations..."

    # Common locations for signtool
    $commonLocations = @(
        # Windows SDK paths
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe",
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x86\signtool.exe",
        
        # Visual Studio paths via vswhere
        (& "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" -latest -products * -requires Microsoft.Component.MSBuild -find "**/signtool.exe" -ErrorAction SilentlyContinue)
    )

    foreach ($location in $commonLocations) {
        $tools = Get-ChildItem -Path $location -ErrorAction SilentlyContinue | 
                 Sort-Object -Property FullName -Descending
        if ($tools -and $tools.Count -gt 0) {
            return $tools[0]  # Return the first match
        }
    }

    return $null
}

function Get-SignatureInfo {
    param(
        [string]$filePath
    )    
    # Get the raw output from signtool
    $rawOutput = & $signtool verify /pa /v $filePath 2>&1    
    
    # Filter output to exclude everything after the timestamp line
    $filteredOutput = @()
    foreach ($line in $rawOutput) {
        if ($line -match "The signature is timestamped:") {
            break
        }
        $filteredOutput += $line
    }    
    # Extract last subject in the signing chain - it's typically the last "Issued to:" entry
    $lastSubject = ($filteredOutput | Select-String -Pattern "Issued to: (.*)$" | Select-Object -Last 1 | ForEach-Object { $_.Matches.Groups[1].Value })    
    # Create signature info object
    $signInfo = @{
        "IsSigned" = $LASTEXITCODE -eq 0
        "Subject" = ($filteredOutput | Select-String -Pattern "Issued to: (.*)$" | ForEach-Object { $_.Matches.Groups[1].Value }) -join ", "
        "Issuer" = ($filteredOutput | Select-String -Pattern "Issued by: (.*)$" | ForEach-Object { $_.Matches.Groups[1].Value }) -join ", "
        "ExpirationDate" = ($filteredOutput | Select-String -Pattern "Expires: (.*)$" | ForEach-Object { $_.Matches.Groups[1].Value }) -join ", "
        "SubjectLast" = $lastSubject
        "SignedBySameCert" = $false
    }

    # Check if signed by our certificate
    $null = & $signtool verify /pa /sha1 $certSha1 $filePath 2>&1
    $signInfo.SignedBySameCert = $LASTEXITCODE -eq 0
    
    return $signInfo
}

# Find dist directory relative to script location
$distDir = Join-Path $PSScriptRoot "../../dist"
if (-not (Test-Path -Path $distDir)) {
    Write-Host "The directory '$distDir' does not exist." -ForegroundColor Red
    exit 1
}
$distDir = Resolve-Path (Join-Path $PSScriptRoot "../../dist") # normalize path

# Find signtool.exe
$signtool = Find-SignTool
if (-not $signtool) {
    Write-Host "signtool.exe not found in any standard location." -ForegroundColor Red
    Write-Host "Please install one of the following:" -ForegroundColor Yellow
    Write-Host "- Windows SDK" -ForegroundColor Yellow
    Write-Host "- Visual Studio with the 'Desktop development with C++' workload" -ForegroundColor Yellow
    Write-Host "- Visual Studio Build Tools with the 'Desktop development with C++' workload" -ForegroundColor Yellow
    exit 1
}

Write-Host "[i] Using signtool: $($signtool)"

# Sign all binary files in the dist directory
try {
    # Define extensions for files that should be signed
    $binaryExtensions = @('.exe', '.dll', '.sys', '.msi')
    
    # Get all files with binary extensions
    $files = Get-ChildItem -Path $distDir -Recurse -File | Where-Object { 
        $extension = [System.IO.Path]::GetExtension($_.Name).ToLower()
        $binaryExtensions -contains $extension
    }
    
    $totalFiles = $files.Count
    $signedFiles = 0
    $alreadySignedFiles = 0
    $wrongCertFiles = 0
    $filesToSign = @()
    
    Write-Host "[+] Found $totalFiles binary files to process" -ForegroundColor Green    
    foreach ($file in $files) {
        $relativeFileName = $file.FullName.Replace("$distDir\", "")
        # Get signature information
        $signInfo = Get-SignatureInfo -filePath $file.FullName
                
        if ($signInfo.IsSigned) {
            if ($signInfo.SignedBySameCert) {
                Write-Host -NoNewline "  [signed OK ]" -ForegroundColor Green
                Write-Host -NoNewline " $($relativeFileName)" -ForegroundColor Blue
                Write-Host "`t: signed by our certificate"
                $alreadySignedFiles++
            } else {
                Write-Host -NoNewline "  [different ]" -ForegroundColor Yellow
                Write-Host -NoNewline " $($relativeFileName)" -ForegroundColor Blue
                Write-Host "`t: signed by different certificate [$($signInfo.SubjectLast)]"
                $wrongCertFiles++                
            }
        } else {
            Write-Host -NoNewline "  [NOT signed]" -ForegroundColor Red
            Write-Host -NoNewline " $($relativeFileName)" -ForegroundColor Blue
            Write-Host "`t: not signed"
            $filesToSign += $file.FullName
        }
    }
    
    # Batch sign files
    if ($filesToSign.Count -gt 0) {
        Write-Host "`n[+] Signing $($filesToSign.Count) files in batch..." -ForegroundColor Green
        
        & $signtool sign /tr $timestampServer /td sha256 /fd sha256 /sha1 $certSha1 /v $filesToSign       
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Failed to sign files!" -ForegroundColor Red
            exit 1
        }

        $signedFiles = $filesToSign.Count
    } else {
        Write-Host "`n[+] No files need signing." -ForegroundColor Green
    }

    Write-Host "`n[+] Summary:" -ForegroundColor Green
    Write-Host "    - Total binary files found: $totalFiles"
    Write-Host "    - Files already signed with our certificate: $alreadySignedFiles"
    Write-Host "    - Files signed with different certificate: $wrongCertFiles"
    Write-Host "    - Files newly signed: $signedFiles"
} catch {
    Write-Host "An error occurred: $_" -ForegroundColor Red
    exit 1
}