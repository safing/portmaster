# Soak test of portmaster-core on Windows. Runs inside the test VM as
# administrator (see test-windows.sh soak):
#
#   .\soak.ps1 -Core C:\vmtest\portmaster-core.exe -Minutes 60
#
# Fetches the signed release binaries (including the kernel extension), runs
# the given portmaster-core under constant traffic and samples its resource
# usage every minute, to find leaks that short tests do not show. Prints one
# line per sample and a summary of the growth between the samples after
# warm-up and the last ones. Exits non-zero if the service dies or a resource
# keeps growing well beyond the warm-up level.
param(
    [Parameter(Mandatory = $true)][string]$Core,
    [int]$Minutes = 60
)

$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

$Work = 'C:\vmtest\pm-soak'
$Bin = Join-Path $Work 'bin'
$Data = Join-Path $Work 'data'
$Log = Join-Path $Work 'core.log'
$Csv = Join-Path $Work 'samples.csv'
$Api = 'http://127.0.0.1:817/api/v1'
$Curl = 'C:\Windows\System32\curl.exe'

Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $Bin, $Data | Out-Null

& $Core update --bin-dir $Bin --data-dir $Data *> (Join-Path $Work 'update.log')
# Allow incoming SSH, the test is controlled over SSH.
Set-Content -Path (Join-Path $Data 'config.json') -Encoding ASCII -Value '{"filter":{"serviceEndpoints":["+ * TCP/22"]}}'

$proc = Start-Process -FilePath $Core -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput $Log -RedirectStandardError "$Log.err" `
    -ArgumentList @('--bin-dir', $Bin, '--data-dir', $Data, '--log-stdout', '--log', 'warning', '--allowed-clients', $Curl)
$null = $proc.Handle
$ready = $false
for ($i = 0; $i -lt 150 -and -not $proc.HasExited; $i++) {
    & $Curl -sf --max-time 5 "$Api/ready" 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Seconds 2
}
if (-not $ready) {
    Write-Output 'SOAK FAILED: portmaster-core did not start'
    Get-Content $Log, "$Log.err" -Tail 20 -ErrorAction SilentlyContinue
    exit 1
}
Write-Output "portmaster-core running (pid $($proc.Id)), soaking for $Minutes minutes"

# Traffic: HTTPS to a few sites and DNS lookups of always new names, so that
# connections, DNS caches and the connection history keep changing.
$traffic = Start-Job -ScriptBlock {
    param($Curl)
    $sites = 'example.com', 'example.org', 'wikipedia.org', 'debian.org', 'kernel.org'
    $i = 0
    while ($true) {
        $i++
        $procs = foreach ($s in $sites) {
            Start-Process -FilePath $Curl -PassThru -WindowStyle Hidden -ArgumentList @('-s', '-o', 'NUL', '--max-time', '10', "https://$s/?soak=$i")
        }
        1..10 | ForEach-Object { Resolve-DnsName -Name "soak-$i-$_.example.com" -DnsOnly -QuickTimeout -ErrorAction SilentlyContinue | Out-Null }
        $procs | ForEach-Object { $_.WaitForExit(15000) | Out-Null }
        Start-Sleep -Seconds 1
    }
} -ArgumentList $Curl

'minute,ws_kb,goroutines,handles,data_kb' | Set-Content -Path $Csv
$failed = $false
for ($m = 0; $m -le $Minutes; $m++) {
    if ($proc.HasExited) {
        Write-Output "SOAK FAILED: portmaster-core died after $m minutes (exit $($proc.ExitCode))"
        Get-Content $Log, "$Log.err" -Tail 30 -ErrorAction SilentlyContinue
        $failed = $true
        break
    }
    $p = Get-Process -Id $proc.Id
    $ws = [int]($p.WorkingSet64 / 1KB)
    $handles = $p.HandleCount
    $stack = & $Curl -sf --max-time 10 "$Api/debug/stack" 2>$null | Select-Object -First 1
    $goroutines = 0
    if ($stack -match 'goroutine profile: total (\d+)') { $goroutines = [int]$Matches[1] }
    $dataKb = [int]((Get-ChildItem -Path $Data -Recurse -File -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum / 1KB)
    "$m,$ws,$goroutines,$handles,$dataKb" | Add-Content -Path $Csv
    'minute {0,3}: ws {1,7} KB  goroutines {2,5}  handles {3,5}  data {4,7} KB' -f $m, $ws, $goroutines, $handles, $dataKb
    if ($m -lt $Minutes) { Start-Sleep -Seconds 60 }
}

Stop-Job $traffic -ErrorAction SilentlyContinue
Remove-Job $traffic -Force -ErrorAction SilentlyContinue
& $Curl -sf --max-time 10 -X POST "$Api/core/shutdown" 2>$null | Out-Null
if (-not $proc.WaitForExit(60000)) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
if ($failed) { exit 1 }

# Compare the average of minutes 5-9 (after warm-up) with the last 5 samples.
$rows = Import-Csv $Csv
if ($rows.Count -lt 15) { Write-Output 'SOAK INCONCLUSIVE: too few samples'; exit 0 }
$early = $rows[5..9]
$late = $rows[($rows.Count - 5)..($rows.Count - 1)]
foreach ($check in @(@('ws_kb', 1.5), @('goroutines', 1.3), @('handles', 1.3))) {
    $key, $limit = $check
    $a = ($early | Measure-Object -Property $key -Average).Average
    $b = ($late | Measure-Object -Property $key -Average).Average
    $ratio = if ($a) { $b / $a } else { 0 }
    $flag = if ($ratio -gt $limit) { 'GROWING' } else { 'ok' }
    if ($ratio -gt $limit) { $failed = $true }
    '{0,-11} after warm-up {1,9:N0}  at the end {2,9:N0}  ratio {3:N2}  {4}' -f $key, $a, $b, $ratio, $flag
}
if ($failed) { Write-Output 'SOAK TEST FAILED'; exit 1 }
Write-Output 'SOAK TEST PASSED'
exit 0
