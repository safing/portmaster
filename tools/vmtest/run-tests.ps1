# Runs all compiled tests of a bundle (see build-tests.sh) on Windows.
# Run it from the bundle root in an elevated PowerShell:
#
#   .\run-tests.ps1 [EXTRA TEST FLAGS...]
#
# Prints one line per package and a summary; exits non-zero on failures.
# Full output of failed packages is in results\<package>.log.
$ErrorActionPreference = 'Continue'
$root = $PSScriptRoot
$results = Join-Path $root 'results'
Remove-Item -Recurse -Force $results -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $results | Out-Null

$pass = 0; $fail = 0; $failed = @()
$bins = Get-ChildItem -Path $root -Recurse -Filter 'pkg.test.exe' | Sort-Object FullName
foreach ($bin in $bins) {
    $rel = $bin.DirectoryName.Substring($root.Length).TrimStart('\') -replace '\\', '/'
    $logf = Join-Path $results (($rel -replace '/', '_') + '.log')
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Push-Location $bin.DirectoryName
    $proc = Start-Process -FilePath $bin.FullName -NoNewWindow -PassThru `
        -RedirectStandardOutput $logf -RedirectStandardError "$logf.err" `
        -ArgumentList (@('-test.short', '-test.count=1', '-test.timeout=10m') + $args)
    # Without querying the handle, ExitCode stays empty after WaitForExit.
    $null = $proc.Handle
    if (-not $proc.WaitForExit(15 * 60 * 1000)) { $proc.Kill(); $rc = 124 } else { $rc = $proc.ExitCode }
    Pop-Location
    Get-Content "$logf.err" -ErrorAction SilentlyContinue | Add-Content $logf
    Remove-Item "$logf.err" -ErrorAction SilentlyContinue
    $dur = [int]$sw.Elapsed.TotalSeconds
    if ($rc -eq 0) {
        $pass++
        '{0,-6}{1,-60} {2,4}s' -f 'ok', $rel, $dur
        Remove-Item $logf
    } else {
        $fail++
        $failed += $rel
        '{0,-6}{1,-60} {2,4}s (exit {3})' -f 'FAIL', $rel, $dur, $rc
    }
}
''
"passed: $pass  failed: $fail"
foreach ($f in $failed) { "  failed: $f" }
if ($fail -gt 0) { exit 1 }
